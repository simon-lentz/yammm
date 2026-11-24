package lsp

import (
	"io"
	"slices"
	"sync"

	"github.com/creachadair/jrpc2/channel"
)

// stoppableChannel reads its channel on a goroutine of its own, so stop ends Recv
// even while that goroutine sits in a read of a blocking descriptor, which no call
// ends on Unix. jrpc2's Stop leaves its reader in that read, and the next message then panics
// on a closed channel inside jrpc2, so the server ends its read here instead.
type stoppableChannel struct {
	channel.Channel
	frames  chan frame
	stopped chan struct{}
	once    sync.Once
}

// frame is one result of the wrapped channel's Recv.
type frame struct {
	bits []byte
	err  error
}

func newStoppableChannel(ch channel.Channel) *stoppableChannel {
	c := &stoppableChannel{
		Channel: ch,
		frames:  make(chan frame),
		stopped: make(chan struct{}),
	}
	go c.read()
	return c
}

// read ends at the wrapped channel's first error, or at the first read that returns
// after stop.
func (c *stoppableChannel) read() {
	for {
		bits, err := c.Channel.Recv()
		// A channel's Recv may reuse its buffer, and this read runs ahead of the caller.
		select {
		case c.frames <- frame{slices.Clone(bits), err}:
			if err != nil {
				return
			}
		case <-c.stopped:
			return
		}
	}
}

// stop ends Recv with [io.EOF], which jrpc2 reads as a client's disconnect.
func (c *stoppableChannel) stop() {
	c.once.Do(func() { close(c.stopped) })
}

// Recv returns the next frame, or [io.EOF] once stop has run.
func (c *stoppableChannel) Recv() ([]byte, error) {
	select {
	case f := <-c.frames:
		return f.bits, f.err
	case <-c.stopped:
		return nil, io.EOF
	}
}
