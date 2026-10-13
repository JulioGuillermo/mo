package tools

import (
	"fmt"
	"sync"
)

// headTailBuffer is a writer that keeps the first and the last bytes written,
// up to limit in total, and counts what it drops in between.
type headTailBuffer struct {
	mu      sync.Mutex
	limit   int
	head    []byte
	tail    []byte
	dropped int
}

func newHeadTailBuffer(limit int) *headTailBuffer {
	return &headTailBuffer{limit: limit}
}

func (b *headTailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(data)
	headRoom := b.limit/2 - len(b.head)
	if headRoom > 0 {
		taken := min(headRoom, len(data))
		b.head = append(b.head, data[:taken]...)
		data = data[taken:]
	}
	b.tail = append(b.tail, data...)
	if excess := len(b.tail) - b.limit/2; excess > 0 {
		b.tail = b.tail[excess:]
		b.dropped += excess
	}
	return written, nil
}

func (b *headTailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropped == 0 {
		return string(b.head) + string(b.tail)
	}
	return fmt.Sprintf("%s\n[... %d bytes cut ...]\n%s", b.head, b.dropped, b.tail)
}
