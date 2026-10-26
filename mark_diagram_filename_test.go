package mark

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var riFilename = regexp.MustCompile(`ri:filename="([^"]*)"`)

// TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses: the ac:image
// template flattens a "/" in an attachment's filename, and a diagram's
// filename is its title. A diagram titled Auth/Login was uploaded as
// Auth/Login.svg and shown as Auth_Login.svg, which is no attachment at all:
// the page showed a broken image.
func TestDiagramTitledWithASlashIsUploadedUnderTheNameThePageUses(t *testing.T) {
	// A title is text, not a path: only its slashes change, and "./" in it
	// is kept.
	for title, want := range map[string]string{
		"Auth/Login": "Auth_Login.svg", "./x": "._x.svg", "Q1/./Q2": "Q1_._Q2.svg",
	} {
		t.Run(title, func(t *testing.T) {
			p := publishBeside(t, nil, "", "```d2 title "+title+"\na -> b\n```\n", Config{
				Features: []string{"d2"}, D2Output: "svg", D2Scale: 1,
			})
			require.NoError(t, p.err)

			stored := uploadedNames(p)
			require.Len(t, stored, 1)

			m := riFilename.FindStringSubmatch(p.body)
			require.NotNil(t, m, p.body)
			assert.Equal(t, want, m[1])
			assert.Equal(t, m[1], stored[0], "the page names the attachment that was uploaded")
		})
	}
}
