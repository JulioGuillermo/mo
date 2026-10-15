package tools

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const cutMarker = "[... %d bytes cut ...]"

// How much of a command's output is kept: its first and its last bytes.
var (
	jobLogHead int64 = 25 << 20
	jobLogTail int64 = 25 << 20
)

// jobLog writes a command's output to disk as it comes: the first head
// bytes in the log itself, what follows in a tail file that keeps only its
// last tail bytes. Once the command ends the tail joins the log, after a
// line saying how much was dropped between them.
type jobLog struct {
	mu       sync.Mutex
	path     string
	head     *os.File
	tail     *os.File
	headCap  int64
	tailCap  int64
	headSize int64
	tailSize int64
	dropped  int64
	closed   bool
	writeErr error
}

func newJobLog(path string) (*jobLog, error) {
	head, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &jobLog{path: path, head: head, headCap: jobLogHead, tailCap: jobLogTail}, nil
}

func (l *jobLog) tailPath() string {
	return l.path + ".tail"
}

// Write never fails the command: a disk error is kept and the output from
// then on dropped.
func (l *jobLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	written := len(data)
	if l.closed || l.writeErr != nil {
		return written, nil
	}
	if room := l.headCap - l.headSize; room > 0 {
		taken := min(room, int64(len(data)))
		if _, err := l.head.Write(data[:taken]); err != nil {
			l.writeErr = err
			return written, nil
		}
		l.headSize += taken
		data = data[taken:]
	}
	if len(data) == 0 {
		return written, nil
	}
	if l.tail == nil {
		tail, err := os.OpenFile(l.tailPath(), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
		if err != nil {
			l.writeErr = err
			return written, nil
		}
		l.tail = tail
	}
	if _, err := l.tail.Write(data); err != nil {
		l.writeErr = err
		return written, nil
	}
	l.tailSize += int64(len(data))
	if l.tailSize > 2*l.tailCap {
		l.writeErr = l.trimTail()
	}
	return written, nil
}

// trimTail keeps the last tailCap bytes of the tail file, from the start of
// a line when there is one near.
func (l *jobLog) trimTail() error {
	kept, err := l.lastTail()
	if err != nil {
		return err
	}
	if err := l.tail.Truncate(0); err != nil {
		return err
	}
	if _, err := l.tail.WriteAt(kept, 0); err != nil {
		return err
	}
	if _, err := l.tail.Seek(int64(len(kept)), io.SeekStart); err != nil {
		return err
	}
	l.dropped += l.tailSize - int64(len(kept))
	l.tailSize = int64(len(kept))
	return nil
}

func (l *jobLog) lastTail() ([]byte, error) {
	size := min(l.tailSize, l.tailCap)
	kept := make([]byte, size)
	if _, err := l.tail.ReadAt(kept, l.tailSize-size); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if size < l.tailSize {
		if newline := bytes.IndexByte(kept, '\n'); newline >= 0 && newline < len(kept)/10 {
			kept = kept[newline+1:]
		}
	}
	return kept, nil
}

// Close joins the tail to the log once the command ended.
func (l *jobLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.tail != nil {
		kept, err := l.lastTail()
		if err == nil {
			dropped := l.dropped + l.tailSize - int64(len(kept))
			if dropped > 0 {
				_, err = fmt.Fprintf(l.head, "\n"+cutMarker+"\n", dropped)
			}
			if err == nil {
				_, err = l.head.Write(kept)
			}
		}
		l.tail.Close()
		os.Remove(l.tailPath())
		if err != nil && l.writeErr == nil {
			l.writeErr = err
		}
	}
	if err := l.head.Close(); err != nil && l.writeErr == nil {
		l.writeErr = err
	}
	return l.writeErr
}

// Size is how many bytes the command wrote so far, dropped ones included.
func (l *jobLog) Size() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.headSize + l.tailSize + l.dropped
}

// Text is the output kept so far, also while the command runs.
func (l *jobLog) Text() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return readJobLog(l.path, l.closed, l.dropped)
}

// readJobLog reads a log, with its tail file and what was cut between them
// while the command still runs.
func readJobLog(path string, closed bool, dropped int64) (string, error) {
	head, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if closed {
		return string(head), nil
	}
	tail, err := os.ReadFile(path + ".tail")
	if errors.Is(err, os.ErrNotExist) {
		return string(head), nil
	}
	if err != nil {
		return "", err
	}
	if dropped > 0 {
		return fmt.Sprintf("%s\n"+cutMarker+"\n%s", head, dropped, tail), nil
	}
	return string(head) + string(tail), nil
}

// outputLog is where a command's output goes: a file, memory, or nowhere
// but the count of what it wrote.
type outputLog interface {
	io.Writer
	Close() error
	Size() int64
	Text() (string, error)
}

// memoryLog keeps a command's output in memory, its first and last bytes
// as a file would; it is gone when mo exits.
type memoryLog struct {
	mu     sync.Mutex
	kept   *HeadTailBuffer
	size   int64
	closed bool
}

func newMemoryLog() *memoryLog {
	return &memoryLog{kept: NewHeadTailBuffer(int(jobLogHead + jobLogTail))}
}

func (l *memoryLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	l.size += int64(len(data))
	l.mu.Unlock()
	return l.kept.Write(data)
}

func (l *memoryLog) Close() error { return nil }

func (l *memoryLog) Size() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.size
}

func (l *memoryLog) Text() (string, error) { return l.kept.String(), nil }

// countingLog keeps nothing of a command's output but how much there was.
type countingLog struct {
	mu   sync.Mutex
	size int64
}

func (l *countingLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.size += int64(len(data))
	return len(data), nil
}

func (l *countingLog) Close() error { return nil }

func (l *countingLog) Size() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.size
}

func (l *countingLog) Text() (string, error) {
	return "", errors.New("the output was not kept: tools.outputs is none, so only its start and end came back when it ran")
}
