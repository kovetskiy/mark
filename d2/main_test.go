package d2

import (
	"os"
	"testing"
)

// TestMain shuts the browser down after the package's tests.
//
// Chrome is held in a package-level singleton that is created lazily on first
// render and cancelled only by Cleanup. Production does that through
// util/cli.go's `defer mark.Cleanup()`, but nothing did it here, so every test
// run orphaned a browser: the test binary exited, the allocator context was
// never cancelled, and Chrome carried on with its /tmp/chromedp-runner* profile
// directory. They accumulate one per run, and since they are each a browser
// with its own zygote, GPU and renderer children, a few dozen runs are enough
// to exhaust memory on a developer machine and make the diagram tests fail for
// reasons that have nothing to do with the code under test.
func TestMain(m *testing.M) {
	code := m.Run()
	Cleanup()
	os.Exit(code)
}
