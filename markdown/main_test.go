package mark

import (
	"os"
	"testing"

	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/mermaid"
)

// testBrowser and testMermaid are what this package's tests draw diagrams and
// formulas with, each started by the first test that needs it. Exported as
// TestBrowser for the tests in mark_test.
var (
	testBrowser = chrome.NewBrowser()
	testMermaid = func() *mermaid.Engine {
		engine, err := mermaid.NewEngine(mermaid.EngineChrome)
		if err != nil {
			panic(err)
		}

		return engine
	}()

	TestBrowser = testBrowser
)

// TestMain shuts down the browsers this package's tests start, rather than
// leaving them orphaned. See the note on d2's TestMain.
func TestMain(m *testing.M) {
	code := m.Run()
	testBrowser.Close()
	testMermaid.Close()
	os.Exit(code)
}
