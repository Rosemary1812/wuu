package providers

import "context"

// StreamEmitter delivers events from a provider stream producer to its output
// channel without ever blocking past request cancellation.
//
// This is part of the StreamClient producer contract: the channel returned by
// StreamChat has a bounded buffer and the consumer may stop reading at any
// point (user cancellation, replay-guard rejection, journal failure). A bare
// `ch <- ev` on a full buffer then blocks forever, which strands the producer
// goroutine and — because lease release rides on the producer's defers — leaks
// the provider-scope admission slot. Every producer send must therefore also
// select on the request context, which the reliable layer cancels whenever it
// abandons an attempt.
//
// Send reports false once the context is done. The producer should stop
// promptly: return, run its defers (release the lease, close the channel), and
// write nothing further.
type StreamEmitter struct {
	ctx context.Context
	ch  chan<- StreamEvent
}

// NewStreamEmitter binds a producer output channel to its request context.
func NewStreamEmitter(ctx context.Context, ch chan<- StreamEvent) *StreamEmitter {
	return &StreamEmitter{ctx: ctx, ch: ch}
}

// Send delivers one event, or reports false when the request context ended
// before the consumer accepted it.
func (e *StreamEmitter) Send(ev StreamEvent) bool {
	select {
	case e.ch <- ev:
		return true
	case <-e.ctx.Done():
		return false
	}
}

// Aborted reports whether the request context has ended. Producers can check
// it at loop boundaries to stop early without attempting another send.
func (e *StreamEmitter) Aborted() bool {
	return e.ctx.Err() != nil
}

// drainStream discards any events still buffered (or in flight) on an
// abandoned attempt channel so a producer mid-send can finish, run its defers,
// and release its lease. It returns immediately; the goroutine ends when the
// producer closes the channel. A producer that honors the emitter contract
// closes promptly after cancellation; one that never closes parks this
// goroutine but cannot leak admission capacity, which is released on the
// producer side.
func drainStream(ch <-chan StreamEvent) {
	if ch == nil {
		return
	}
	go func() {
		for range ch {
		}
	}()
}
