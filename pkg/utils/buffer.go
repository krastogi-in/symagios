package utils

const (
	defaultBufferSize = 100
)

// Buffer is a bounded work queue. Get blocks until an item is available.
type Buffer struct {
	ch chan interface{}
}

func NewBuffer(bufferSize int) *Buffer {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	return &Buffer{
		ch: make(chan interface{}, bufferSize),
	}
}

// Add enqueues item. Returns false if the buffer is full or item is nil.
func (b *Buffer) Add(item interface{}) bool {
	if item == nil {
		return false
	}
	select {
	case b.ch <- item:
		return true
	default:
		return false
	}
}

// Get blocks until an item arrives. Does not close the channel.
func (b *Buffer) Get() interface{} {
	return <-b.ch
}
