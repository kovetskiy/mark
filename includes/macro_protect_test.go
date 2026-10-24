package includes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
