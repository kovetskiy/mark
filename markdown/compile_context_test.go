package markdown

import (
	"context"
	"testing"
	"time"

	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/require"
)

// TestCompileMarkdownStopsWithItsContext: the caller's context reaches the
// diagram and formula renderers, so a compile whose context has ended fails
// with that context's reason, rather than drawing the diagram regardless.
// Nothing here starts a browser: every renderer checks the context first.
func TestCompileMarkdownStopsWithItsContext(t *testing.T) {
	documents := map[string]struct {
		source string
		config Options
	}{
		"mermaid": {
			"```mermaid\ngraph LR; A-->B\n```\n",
			Options{Mermaid: testMermaid, Features: []string{"mermaid"}, MermaidScale: 1.0},
		},
		"d2": {
			"```d2\na -> b\n```\n",
			Options{Browser: testBrowser, Features: []string{"d2"}, D2Scale: 1.0},
		},
		"math": {
			"$$\nx^2\n$$\n",
			Options{Browser: testBrowser, Features: []string{"math"}, MathScale: 1.0},
		},
	}

	ended := map[string]struct {
		ctx  func(t *testing.T) context.Context
		want error
	}{
		"cancelled": {
			func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			},
			context.Canceled,
		},
		"expired": {
			func(t *testing.T) context.Context {
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)

				return ctx
			},
			context.DeadlineExceeded,
		},
	}

	std, err := stdlib.New(t.Context(), nil)
	require.NoError(t, err)

	for name, doc := range documents {
		for how, end := range ended {
			t.Run(name+"/"+how, func(t *testing.T) {
				start := time.Now()
				_, _, err := CompileMarkdown(end.ctx(t), []byte(doc.source), std, "test.md", doc.config)
				require.ErrorIs(t, err, end.want)
				require.Less(t, time.Since(start), 10*time.Second, "a compile whose context has ended should not wait on a render")
			})
		}
	}
}
