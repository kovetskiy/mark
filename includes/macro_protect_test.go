package includes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectMacroDirectives(t *testing.T) {
	const macro = "<!-- Macro: X\n     Template: #t\n     t: \"{{ .Name }}\" -->"

	tests := []struct {
		name, in, left, right, want string
	}{
		{
			"default delims",
			"{{ .a }} " + macro + " {{ .b }}",
			"", "",
			"{{ .a }} " + strings.ReplaceAll(macro, "{{", `{{"{{"}}`) + " {{ .b }}",
		},
		{
			"custom delims",
			"[[ .a ]] <!-- Macro: X\n [[ .n ]] -->",
			"[[", "]]",
			"[[ .a ]] <!-- Macro: X\n [[\"[[\"]] .n ]] -->",
		},
		{"delims none", macro, "\x00", "\x01", macro},
		{"example in code", "```\n" + macro + "\n```\n", "", "", "```\n" + macro + "\n```\n"},
		{"other comments untouched", "<!-- note {{ .a }} -->", "", "", "<!-- note {{ .a }} -->"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(protectMacroDirectives([]byte(tt.in), tt.left, tt.right)))
		})
	}
}

func TestDirectiveEnd(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // the directive text expected up to and including "-->"
	}{
		{
			name: "plain directive ends at its own terminator",
			in:   "<!-- Macro: :a:\n Template: x\n-->\n\nlater -->\n",
			want: "<!-- Macro: :a:\n Template: x\n-->",
		},
		{
			name: "nested pattern consumes two terminators",
			in:   "<!-- Macro: <!-- d -->(x)\n Template: y\n-->\n\nbody\n",
			want: "<!-- Macro: <!-- d -->(x)\n Template: y\n-->",
		},
		{
			name: "unterminated comment reports -1",
			in:   "<!-- Macro: :a:\n Template: x\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := directiveEnd(tt.in)
			if tt.want == "" {
				assert.Equal(t, -1, got)
				return
			}
			require.NotEqual(t, -1, got, "expected a terminator")
			assert.Equal(t, tt.want, tt.in[:got+len("-->")])
			assert.True(t, strings.HasSuffix(tt.in[:got+3], "-->"))
		})
	}
}
