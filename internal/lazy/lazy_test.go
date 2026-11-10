package lazy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	doubled atomic.Int64
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
	} else {
		f.doubled.Add(1)
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

// TestCloseDoesNotWaitOutAStart: the start in flight is cancelled, not waited
// for, and Close returns at once.
func TestCloseDoesNotWaitOutAStart(t *testing.T) {
	f := newFake()
	f.release = make(chan struct{}) // never released
	l := f.lazy()

	got := make(chan error, 1)
	go func() {
		_, err := l.Get(context.Background())
		got <- err
	}()
	<-f.began

	began := time.Now()
	l.Close()
	assert.Less(t, time.Since(began), 500*time.Millisecond)

	select {
	case err := <-got:
		require.ErrorIs(t, err, errTestClosed)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("the waiting Get was not released by Close")
	}
}

// TestCloseStopsWhatALateStartMade: a start that finishes after Close despite
// being cancelled leaves nothing running once Close returns.
func TestCloseStopsWhatALateStartMade(t *testing.T) {
	stopped := make(chan *resource, 1)
	release := make(chan struct{})
	began := make(chan struct{})

	l := New(func(ctx context.Context) (*resource, error) {
		close(began)
		<-ctx.Done()
		<-release // ignores the cancellation for a moment

		return &resource{}, nil
	}, func(r *resource) { stopped <- r }, errTestClosed)

	got := make(chan error, 1)
	go func() {
		_, err := l.Get(context.Background())
		got <- err
	}()
	<-began

	closed := make(chan struct{})
	go func() {
		l.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close returned before the start it cancelled had finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	<-closed

	select {
	case <-stopped:
	default:
		t.Fatal("what the start made was not stopped")
	}

	require.ErrorIs(t, <-got, errTestClosed)
}

func TestCloseTwice(t *testing.T) {
	f := newFake()
	l := f.lazy()

	_, err := l.Get(t.Context())
	require.NoError(t, err)

	l.Close()
	l.Close()

	assert.Equal(t, int64(1), f.stops.Load())
}

// TestDiscardAfterCloseSaysSo: a use that failed once Close was called is
// said to be Close's doing, and one that failed before it is returned as is.
func TestDiscardAfterCloseSaysSo(t *testing.T) {
	f := newFake()
	l := f.lazy()

	r, err := l.Get(t.Context())
	require.NoError(t, err)

	failure := errors.New("render failed")
	assert.Equal(t, failure, l.Discard(r, failure))
	require.NoError(t, l.Discard(r, nil))

	r, err = l.Get(t.Context())
	require.NoError(t, err)

	l.Close()

	err = l.Discard(r, context.Canceled)
	require.ErrorIs(t, err, errTestClosed)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(2), f.stops.Load())
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

	require.NoError(t, l.Discard(first, nil))
	assert.True(t, first.stopped.Load())

	_, has := l.Peek()
	assert.False(t, has)

	second, err := l.Get(t.Context())
	require.NoError(t, err)
	assert.NotSame(t, first, second)

	// A second caller that failed on the first resource leaves the
	// replacement alone.
	require.NoError(t, l.Discard(first, nil))
	assert.False(t, second.stopped.Load())

	current, has := l.Peek()
	assert.True(t, has)
	assert.Same(t, second, current)
}

func TestFailedStartIsShared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		// Returns once every caller is waiting on the one start.
		synctest.Wait()
		close(f.release)

		for range callers {
			require.Error(t, <-errs)
		}

		assert.Equal(t, int64(1), f.starts.Load())

		f.failing.Store(false)

		_, err := l.Get(t.Context())
		require.NoError(t, err, "a failed start is tried again")
	})
}

// TestCloseWaitsForDiscard: Close does not return while Discard is still
// stopping the value it took.
func TestCloseWaitsForDiscard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		stopped := make(chan struct{})

		l := New(func(context.Context) (*resource, error) {
			return &resource{}, nil
		}, func(*resource) {
			<-release
			close(stopped)
		}, errTestClosed)

		r, err := l.Get(t.Context())
		require.NoError(t, err)

		go func() { _ = l.Discard(r, nil) }()
		synctest.Wait() // Discard is inside stop

		closed := make(chan struct{})
		go func() {
			l.Close()
			close(closed)
		}()
		synctest.Wait()

		select {
		case <-closed:
			t.Fatal("Close returned while Discard was still stopping the value")
		default:
		}

		close(release)
		<-closed

		select {
		case <-stopped:
		default:
			t.Fatal("Close returned before the stop finished")
		}
	})
}

// TestSecondCloseWaitsForStop: a Close that arrives while another is still
// stopping the resource does not return before that stop has finished.
func TestSecondCloseWaitsForStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		stopped := make(chan struct{})

		l := New(func(context.Context) (*resource, error) {
			return &resource{}, nil
		}, func(*resource) {
			<-release
			close(stopped)
		}, errTestClosed)

		_, err := l.Get(t.Context())
		require.NoError(t, err)

		first := make(chan struct{})
		go func() {
			l.Close()
			close(first)
		}()
		synctest.Wait() // the first Close is inside stop

		second := make(chan struct{})
		go func() {
			l.Close()
			close(second)
		}()
		synctest.Wait()

		select {
		case <-second:
			t.Error("the second Close returned while the first was still stopping the resource")
		default:
		}

		close(release)
		<-first
		<-second

		select {
		case <-stopped:
		default:
			t.Fatal("Close returned before the stop finished")
		}
	})
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

				_ = l.Discard(r, nil)
			}
		})
	}

	time.Sleep(5 * time.Millisecond)
	l.Close()
	wg.Wait()

	assert.Equal(t, f.starts.Load(), f.stops.Load(), "every resource started was stopped")
	assert.Zero(t, f.doubled.Load(), "no resource was stopped twice")
}
