package mermaid

import (
	"os"
	"testing"
)

// engine is the chrome engine the package's tests draw with, started by the
// first of them.
var engine = func() *Engine {
	e, err := NewEngine(EngineChrome)
	if err != nil {
		panic(err)
	}

	return e
}()

// TestMain shuts the engine down after the package's tests; see the note on
// d2's TestMain for why this is needed.
func TestMain(m *testing.M) {
	code := m.Run()
	engine.Close()
	os.Exit(code)
}
