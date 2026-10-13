package llm

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const (
	sseInitialBuffer = 64 << 10
	sseMaxLine       = 16 << 20
)

var errStopSSE = errors.New("stop reading the stream")

// readSSE calls onData with the payload of every "data:" line until the body
// ends or onData returns errStopSSE.
func readSSE(body io.Reader, onData func(data []byte) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, sseInitialBuffer), sseMaxLine)
	for scanner.Scan() {
		data, found := bytes.CutPrefix(scanner.Bytes(), []byte("data:"))
		if !found {
			continue
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			continue
		}
		err := onData(data)
		if err == errStopSSE {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return scanner.Err()
}
