package lsp

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2/channel"
)

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// The framing reuses its buffer, and the read runs ahead of the caller, so the first
// frame must hold its bytes once the second is read.
func TestStoppableChannel_AFrameOutlivesTheNextRead(t *testing.T) {
	t.Parallel()
	input := "Content-Length: 5\r\n\r\nfirst" + "Content-Length: 6\r\n\r\nsecond"
	c := newStoppableChannel(channel.LSP(bytes.NewReader([]byte(input)), nopWriteCloser{io.Discard}))
	defer c.Close()

	first, err := c.Recv()
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "first" || string(second) != "second" {
		t.Errorf("frames = %q, %q, want first, second", first, second)
	}
}

func TestStoppableChannel_StopEndsRecvWhileTheReadIsBlocked(t *testing.T) {
	t.Parallel()
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	c := newStoppableChannel(channel.LSP(r, nopWriteCloser{io.Discard}))

	c.stop()
	if _, err := c.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("Recv after stop = %v, want io.EOF", err)
	}
}

func TestStoppableChannel_AChannelErrorReachesRecv(t *testing.T) {
	t.Parallel()
	c := newStoppableChannel(channel.LSP(bytes.NewReader(nil), nopWriteCloser{io.Discard}))
	defer c.Close()
	if _, err := c.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("Recv = %v, want the channel's io.EOF", err)
	}
}

// scriptedChannel answers each Recv from answer, with the number of the call.
type scriptedChannel struct {
	channel.Channel
	recvs  atomic.Int32
	answer func(call int32) ([]byte, error)
}

func (c *scriptedChannel) Recv() ([]byte, error) {
	return c.answer(c.recvs.Add(1))
}

// A read past an error would sit on an unread frame for the life of the process.
func TestStoppableChannel_TheReadEndsAtTheChannelsError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		wrapped := &scriptedChannel{answer: func(int32) ([]byte, error) { return nil, errors.New("closed") }}
		c := newStoppableChannel(wrapped)
		defer c.stop()

		if _, err := c.Recv(); err == nil {
			t.Error("Recv = nil, want the channel's error")
		}
		synctest.Wait()
		if got := wrapped.recvs.Load(); got != 1 {
			t.Errorf("the channel took %d reads, want 1", got)
		}
	})
}

// A frame read before stop finds no caller. The read drops it and ends, and the bubble
// refuses to end while the read still waits to hand it over.
func TestStoppableChannel_StopDropsAFrameAlreadyRead(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		wrapped := &scriptedChannel{answer: func(int32) ([]byte, error) { return []byte("dropped"), nil }}
		c := newStoppableChannel(wrapped)
		synctest.Wait()

		c.stop()
		synctest.Wait()
		if got := wrapped.recvs.Load(); got != 1 {
			t.Errorf("the channel took %d reads, want 1", got)
		}
	})
}
