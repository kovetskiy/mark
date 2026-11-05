package lazy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestClosed = errors.New("closed")

// resource is what the fake starter makes.
type resource struct {
	id      int64
	stopped atomic.Bool
}

// fake counts what it starts and stops. Its start can be held until release
// is closed, standing in for a browser that takes its time, and it gives up
// only when its context ends, as a real start does.
type fake struct {
	starts  atomic.Int64
	stops   atomic.Int64
	release chan struct{}
	failing atomic.Bool
	began   chan struct{}
}

func newFake() *fake {
	return &fake{began: make(chan struct{}, 1)}
}

func (f *fake) start(ctx context.Context) (*resource, error) {
	id := f.starts.Add(1)

	select {
	case f.began <- struct{}{}:
	default:
	}

	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if f.failing.Load() {
		return nil, errors.New("start failed")
	}

	return &resource{id: id}, nil
}

func (f *fake) stop(r *resource) {
	if r.stopped.CompareAndSwap(false, true) {
		f.stops.Add(1)
	}
}

func (f *fake) lazy() *Lazy[*resource] {
	return New(f.start, f.stop, errTestClosed)
}

func TestNothingStartsUntilAsked(t *testing.T) {
	f := newFake()
	l := f.lazy()
	l.Close()

	assert.Zero(t, f.starts.Load())
	assert.Zero(t, f.stops.Load())
}

func TestGetSharesOneResource(t *testing.T) {
	f := newFake()
	l := f.lazy()
	t.Cleanup(l.Close)

	first, err := l.Get(t.Context())
	require.NoError(t, err)

	second, err := l.Get(t.Context())
	require.NoError(t, err)

	assert.Same(t, first, second)
	assert.Equal(t, int64(1), f.starts.Load())
}

func TestCloseTwice(t *testing.T) {
	f := newFake()
	l := f.lazy()

	_, err := l.Get(t.Context())
	require.NoError(t, err)

	l.Close()
	l.Close()

	assert.Equal(t, int64(1), f.stops.Load())
	assert.True(t, l.Closed())
}

func TestGetAfterClose(t *testing.T) {
	f := newFake()
	l := f.lazy()
	l.Close()

	_, err := l.Get(t.Context())
	require.ErrorIs(t, err, errTestClosed)
	assert.Zero(t, f.starts.Load())
}

func TestDiscardStartsAnother(t *testing.T) {
	f := newFake()
	l := f.lazy()
	t.Cleanup(l.Close)

	first, err := l.Get(t.Context())
	require.NoError(t, err)

	l.Discard(first)
	assert.True(t, first.stopped.Load())

	_, has := l.Peek()
	assert.False(t, has)

	second, err := l.Get(t.Context())
	require.NoError(t, err)
	assert.NotSame(t, first, second)

	// A second caller that failed on the first resource leaves the
	// replacement alone.
	l.Discard(first)
	assert.False(t, second.stopped.Load())

	current, has := l.Peek()
	assert.True(t, has)
	assert.Same(t, second, current)
}

func TestFailedStartIsShared(t *testing.T) {
	f := newFake()
	f.release = make(chan struct{})
	f.failing.Store(true)
	l := f.lazy()
	t.Cleanup(l.Close)

	const callers = 4

	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := l.Get(t.Context())
			errs <- err
		}()
	}
	<-f.began

	// Gives every caller time to find the start in flight.
	time.Sleep(20 * time.Millisecond)
	close(f.release)

	for range callers {
		require.Error(t, <-errs)
	}

	assert.Equal(t, int64(1), f.starts.Load())

	f.failing.Store(false)

	_, err := l.Get(t.Context())
	require.NoError(t, err, "a failed start is tried again")
}

// TestCallerGivesUpOnAStart: a caller's context bounds its own wait, and the
// start goes on for the next caller.
func TestCallerGivesUpOnAStart(t *testing.T) {
	f := newFake()
	f.release = make(chan struct{})
	l := f.lazy()
	t.Cleanup(l.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err := l.Get(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	close(f.release)

	_, err = l.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), f.starts.Load())
}

func TestEndedCallerStartsNothing(t *testing.T) {
	f := newFake()
	l := f.lazy()
	t.Cleanup(l.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := l.Get(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, f.starts.Load())
}

// TestConcurrentUse races Get, Discard and Close; run under -race. Whatever
// was started has been stopped once Close returns.
func TestConcurrentUse(t *testing.T) {
	f := newFake()
	l := f.lazy()

	var wg sync.WaitGroup

	for range 16 {
		wg.Go(func() {
			for range 50 {
				r, err := l.Get(context.Background())
				if err != nil {
					if !errors.Is(err, errTestClosed) {
						t.Errorf("unexpected error: %v", err)
					}

					return
				}

				l.Discard(r)
			}
		})
	}

	time.Sleep(5 * time.Millisecond)
	l.Close()
	wg.Wait()

	assert.Equal(t, f.starts.Load(), f.stops.Load(), "every resource started was stopped")
}
