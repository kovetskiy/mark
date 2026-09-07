package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

// TestMermaidEngineFlagValidation covers the value a run is told to draw with.
// A name mark does not know would otherwise reach the renderer and fall back to
// chrome, publishing diagrams drawn by something other than what was asked for.
func TestMermaidEngineFlagValidation(t *testing.T) {
	run := func(args ...string) error {
		cmd := &cli.Command{
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "mermaid-engine", Value: "chrome"},
			},
			Before: CheckFlags,
			Action: func(context.Context, *cli.Command) error { return nil },
		}

		return cmd.Run(context.Background(), append([]string{"cmd"}, args...))
	}

	t.Run("chrome is accepted", func(t *testing.T) {
		assert.NoError(t, run("--mermaid-engine", "chrome"))
	})

	t.Run("merman is accepted", func(t *testing.T) {
		assert.NoError(t, run("--mermaid-engine", "merman"))
	})

	t.Run("anything else is rejected", func(t *testing.T) {
		assert.Error(t, run("--mermaid-engine", "graphviz"))
	})

	t.Run("emptied on purpose is rejected", func(t *testing.T) {
		assert.Error(t, run("--mermaid-engine", ""))
	})
}
