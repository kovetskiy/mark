package renderer_test

import (
	"bytes"
	"testing"

	cparser "github.com/kovetskiy/mark/v16/parser"
	crenderer "github.com/kovetskiy/mark/v16/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func TestConfluenceDateRenderer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "date macro simple",
			input:    "Release on @date(2026-07-27).",
			expected: "<p>Release on <time datetime=\"2026-07-27\" />.</p>\n",
		},
		{
			name:     "date macro quoted",
			input:    "Target date is @date(\"2026-12-31\").",
			expected: "<p>Target date is <time datetime=\"2026-12-31\" />.</p>\n",
		},
		{
			name:     "html time tag with datetime attribute",
			input:    "Event date: <time datetime=\"2026-07-27\">July 27, 2026</time>",
			expected: "<p>Event date: <time datetime=\"2026-07-27\" /></p>\n",
		},
		{
			name:     "html time tag inner text",
			input:    "Deadline: <time>2026-08-15</time>",
			expected: "<p>Deadline: <time datetime=\"2026-08-15\" /></p>\n",
		},
		{
			name:     "html time self-closing tag",
			input:    "Check-in: <time datetime=\"2026-09-01\" />",
			expected: "<p>Check-in: <time datetime=\"2026-09-01\" /></p>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gm := goldmark.New(
				goldmark.WithParserOptions(
					parser.WithInlineParsers(
						util.Prioritized(cparser.NewDateParser(), 99),
					),
				),
				goldmark.WithRendererOptions(
					renderer.WithNodeRenderers(
						util.Prioritized(crenderer.NewConfluenceDateRenderer(), 100),
					),
				),
			)

			var buf bytes.Buffer
			err := gm.Convert([]byte(tt.input), &buf)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, buf.String())
		})
	}
}
