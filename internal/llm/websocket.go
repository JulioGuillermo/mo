package llm

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	wsText  = 0x1
	wsClose = 0x8
	wsPing  = 0x9
	wsPong  = 0xA
	// wsAcceptGUID is the fixed key RFC 6455 joins to the client's to
	// prove the server speaks WebSocket.
	wsAcceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// webSocket is the client side of a WebSocket connection, text messages
// only, as the realtime API uses.
type webSocket struct {
	conn    net.Conn
	reader  *bufio.Reader
	writing sync.Mutex
}

// dialWebSocket opens a WebSocket at an http or https address, sending
// headers with the upgrade.
func dialWebSocket(ctx context.Context, address string, headers map[string]string) (*webSocket, error) {
	target, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	host := target.Host
	if target.Port() == "" {
		port := "80"
		if target.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(target.Hostname(), port)
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "https" {
		secure := tls.Client(conn, &tls.Config{ServerName: target.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		conn = secure
	}
	socket := &webSocket{conn: conn, reader: bufio.NewReader(conn)}
	if err := socket.upgrade(target, headers); err != nil {
		conn.Close()
		return nil, err
	}
	return socket, nil
}

func (s *webSocket) upgrade(target *url.URL, headers map[string]string) error {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	key := base64.StdEncoding.EncodeToString(nonce)
	var request strings.Builder
	fmt.Fprintf(&request, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n", target.RequestURI(), target.Host)
	fmt.Fprintf(&request, "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", key)
	for name, value := range headers {
		fmt.Fprintf(&request, "%s: %s\r\n", name, value)
	}
	request.WriteString("\r\n")
	if _, err := io.WriteString(s.conn, request.String()); err != nil {
		return err
	}
	resp, err := http.ReadResponse(s.reader, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	sum := sha1.Sum([]byte(key + wsAcceptGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return errors.New("the server did not accept the WebSocket")
	}
	return nil
}

// Send writes one text message, masked as a client must.
func (s *webSocket) Send(data []byte) error {
	return s.writeFrame(wsText, data)
}

func (s *webSocket) writeFrame(opcode byte, data []byte) error {
	frame := []byte{0x80 | opcode}
	switch {
	case len(data) < 126:
		frame = append(frame, 0x80|byte(len(data)))
	case len(data) <= 0xFFFF:
		frame = append(frame, 0x80|126)
		frame = binary.BigEndian.AppendUint16(frame, uint16(len(data)))
	default:
		frame = append(frame, 0x80|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(data)))
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	frame = append(frame, mask...)
	for i, b := range data {
		frame = append(frame, b^mask[i%4])
	}
	s.writing.Lock()
	defer s.writing.Unlock()
	_, err := s.conn.Write(frame)
	return err
}

// Receive reads the next whole message, answering pings on the way.
func (s *webSocket) Receive() ([]byte, error) {
	var message []byte
	for {
		final, opcode, payload, err := s.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case wsPing:
			if err := s.writeFrame(wsPong, payload); err != nil {
				return nil, err
			}
			continue
		case wsPong:
			continue
		case wsClose:
			return nil, fmt.Errorf("the server closed the WebSocket: %s", closeReason(payload))
		}
		message = append(message, payload...)
		if final {
			return message, nil
		}
	}
}

func (s *webSocket) readFrame() (final bool, opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err = io.ReadFull(s.reader, header); err != nil {
		return
	}
	final, opcode = header[0]&0x80 != 0, header[0]&0x0F
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err = io.ReadFull(s.reader, extended); err != nil {
			return
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err = io.ReadFull(s.reader, extended); err != nil {
			return
		}
		length = binary.BigEndian.Uint64(extended)
	}
	if length > maxWebSocketMessage {
		return false, 0, nil, fmt.Errorf("a WebSocket frame of %d bytes is too large", length)
	}
	payload = make([]byte, length)
	_, err = io.ReadFull(s.reader, payload)
	return
}

const maxWebSocketMessage = 64 << 20

func closeReason(payload []byte) string {
	if len(payload) <= 2 {
		return "no reason given"
	}
	return string(payload[2:])
}

func (s *webSocket) Close() error {
	s.writeFrame(wsClose, nil)
	return s.conn.Close()
}
