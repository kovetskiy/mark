package mermaid

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silentBrowser makes engine launch a program that runs and never answers, in
// place of Chrome.
func silentBrowser(t *testing.T, engine *Engine) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in browser is a shell script")
	}

	path := filepath.Join(t.TempDir(), "silent-browser")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 600\n"), 0o700)) //nolint:gosec // G306: it has to be executable

	engine.extraOptions = []chromedp.ExecAllocatorOption{chromedp.ExecPath(path)}
}

// shortStartup shortens the startup bound for one test.
func shortStartup(t *testing.T, timeout time.Duration) {
	t.Helper()

	original := startupTimeout
	startupTimeout = timeout

	t.Cleanup(func() { startupTimeout = original })
}

// renderResult is what a Render running on its own goroutine came back with.
func renderResult(t *testing.T, engine *Engine) <-chan error {
	t.Helper()

	got := make(chan error, 1)

	go func() {
		_, err := engine.Render(t.Context(), "d", []byte("graph TD;\n A-->B;"), Options{Output: imageformat.SVG})
		got <- err
	}()

	return got
}

// await is what a render came back with, failing the test when it has not come
// back within limit.
func await(t *testing.T, got <-chan error, limit time.Duration) error {
	t.Helper()

	select {
	case err := <-got:
		return err
	case <-time.After(limit):
		t.Fatal("the render was still waiting for the browser to start")

		return nil
	}
}

// TestStartupIsBounded: a browser that launches and never answers fails the
// diagram that asked for it once the startup bound has passed, as it does in
// chrome.Browser, and leaves no engine behind.
func TestStartupIsBounded(t *testing.T) {
	engine, _ := useEngine(t, EngineChrome)
	silentBrowser(t, engine)
	shortStartup(t, 500*time.Millisecond)

	err := await(t, renderResult(t, engine), 20*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not finish starting within 500ms")
	assert.NotErrorIs(t, err, errClosed)

	_, started := engine.renderers().Peek()
	assert.False(t, started)
}

// TestRenderAfterAStartupThatTimedOut: the next diagram launches a browser of
// its own, and that one outlives the startup bound it was started under.
func TestRenderAfterAStartupThatTimedOut(t *testing.T) {
	engine, _ := useEngine(t, EngineChrome)
	silentBrowser(t, engine)
	shortStartup(t, 500*time.Millisecond)

	require.Error(t, await(t, renderResult(t, engine), 20*time.Second))

	engine.extraOptions = nil
	startupTimeout = time.Minute

	require.NoError(t, await(t, renderResult(t, engine), 2*time.Minute))

	// No start could finish within this, so drawing again shows the browser
	// already started is the one in use.
	startupTimeout = 0

	assert.NoError(t, await(t, renderResult(t, engine), 2*time.Minute))
}

// TestCloseDuringASilentStartReturnsAtOnce: Close ends a launch that is still
// inside its startup bound rather than waiting the bound out.
func TestCloseDuringASilentStartReturnsAtOnce(t *testing.T) {
	engine, _ := useEngine(t, EngineChrome)
	silentBrowser(t, engine)

	got := renderResult(t, engine)

	// Long enough for the stand-in to be running.
	time.Sleep(200 * time.Millisecond)

	began := time.Now()
	engine.Close()
	assert.Less(t, time.Since(began), 5*time.Second)

	select {
	case err := <-got:
		require.ErrorIs(t, err, errClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("the render was not released by Close")
	}
}
