// Package lazy holds a resource that is started by the first caller that needs
// it, shared by every caller after that, replaced when a caller finds it
// broken, and shut down for good by Close.
//
// The headless browser and the mermaid engine are both run this way. What
// they share is here; what counts as broken is each one's own decision.
package lazy

import (
	"context"
	"fmt"
	"sync"
)

// Lazy is a resource of type T started on first use. It is safe for concurrent
// use, and its zero value is not: make one with New.
type Lazy[T comparable] struct {
	start  func(ctx context.Context) (T, error)
	stop   func(T)
	closed error

	// life is what every start runs under. Close cancels it, so that a start
	// in flight gives up at once instead of running to its own timeout.
	life   context.Context
	cancel context.CancelFunc

	// pending counts the starts and Discard's stops in flight. Close waits
	// for it, so that nothing they made or are stopping outlives Close.
	pending sync.WaitGroup

	mu       sync.Mutex
	value    T
	has      bool
	attempt  *attempt[T]
	isClosed bool
}

// attempt is one start, shared by every Get that arrives while it runs.
type attempt[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// New returns a Lazy that has not started. start makes the resource and must
// return promptly once its ctx is done, which happens when Close is called;
// the resource it returns may outlive that call and lives until stop. stop
// shuts a resource down and may be called more than once for the same one.
// closed is what Get answers once Close has been called.
func New[T comparable](start func(ctx context.Context) (T, error), stop func(T), closed error) *Lazy[T] {
	l := &Lazy[T]{start: start, stop: stop, closed: closed}
	l.life, l.cancel = context.WithCancel(context.Background()) //nolint:gosec // G118: Close calls l.cancel

	return l
}

// Get returns the resource, starting it if there is none. Callers that arrive
// while a start runs wait for that one rather than starting another, and all
// of them get its error if it fails; the next Get after a failure tries again.
//
// ctx bounds only this caller's wait. The start runs on its own, so a caller
// that gives up leaves the resource to the next one.
func (l *Lazy[T]) Get(ctx context.Context) (T, error) {
	var zero T

	if err := ctx.Err(); err != nil {
		return zero, context.Cause(ctx)
	}

	l.mu.Lock()

	if l.isClosed {
		l.mu.Unlock()
		return zero, l.closed
	}

	if l.has {
		value := l.value
		l.mu.Unlock()

		return value, nil
	}

	try := l.attempt
	if try == nil {
		try = &attempt[T]{done: make(chan struct{})}
		l.attempt = try
		l.pending.Add(1)

		go l.run(try)
	}

	l.mu.Unlock()

	select {
	case <-try.done:
		return try.value, try.err
	case <-ctx.Done():
		return zero, context.Cause(ctx)
	}
}

// run performs one start and publishes what it made, or stops it when Close
// came first.
func (l *Lazy[T]) run(try *attempt[T]) {
	defer l.pending.Done()
	defer close(try.done)

	value, err := l.start(l.life)

	l.mu.Lock()
	l.attempt = nil
	closed := l.isClosed

	if err == nil && !closed {
		l.value, l.has = value, true
	}
	l.mu.Unlock()

	switch {
	case closed && err == nil:
		l.stop(value)
		try.err = l.closed
	case closed:
		try.err = fmt.Errorf("%w: %w", l.closed, err)
	case err != nil:
		try.err = err
	default:
		try.value = value
	}
}

// Discard stops value and forgets it, so that the next Get starts another. A
// value that is no longer the current one was already stopped by whoever
// replaced it, so the replacement is left alone.
func (l *Lazy[T]) Discard(value T) {
	l.mu.Lock()

	current := l.has && l.value == value
	if current {
		var zero T
		l.value, l.has = zero, false
		l.pending.Add(1)
	}
	l.mu.Unlock()

	if current {
		l.stop(value)
		l.pending.Done()
	}
}

// Peek returns the current resource without starting one.
func (l *Lazy[T]) Peek() (T, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.value, l.has
}

// Closed reports whether Close has been called. A caller whose use of the
// resource failed asks it to tell Close interrupting it from a fault.
func (l *Lazy[T]) Closed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.isClosed
}

// Close stops the resource for good. A start in flight is cancelled rather
// than waited out, and whatever it made is stopped before Close returns, as is
// a value Discard is still stopping.
// Closing twice is harmless.
func (l *Lazy[T]) Close() {
	l.mu.Lock()

	if l.isClosed {
		l.mu.Unlock()
		return
	}

	l.isClosed = true
	l.cancel()

	value, has := l.value, l.has

	var zero T
	l.value, l.has = zero, false
	l.mu.Unlock()

	if has {
		l.stop(value)
	}

	l.pending.Wait()
}
