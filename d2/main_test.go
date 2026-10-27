package d2

import (
	"os"
	"testing"

	"github.com/kovetskiy/mark/v17/chrome"
)

// browser is the one the package's tests render in, started by the first.
var browser = chrome.NewBrowser()

// TestMain shuts the browser down after the package's tests.
//
// A browser left running when the test binary exits carries on with its
// /tmp/chromedp-runner* profile directory, one per run, each with its own
// zygote, GPU and renderer children. A few dozen runs are enough to exhaust
// memory on a developer machine and make the diagram tests fail for reasons
// that have nothing to do with the code under test.
func TestMain(m *testing.M) {
	code := m.Run()
	browser.Close()
	os.Exit(code)
}
