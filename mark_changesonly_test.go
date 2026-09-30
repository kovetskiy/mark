package mark

import (
	"io"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChangesOnlySeesPagePropertyHeaders: --changes-only compared a hash of
// the body alone, so a document whose only change was its Emoji or
// Content-Appearance header was reported "already up to date" and the page
// kept the old one. Both are published with the body.
func TestChangesOnlySeesPagePropertyHeaders(t *testing.T) {
	const header = "<!-- Space: DOCS -->\n<!-- Parent: Parent -->\n<!-- Title: Doc -->\n"

	tests := []struct {
		name  string
		extra string
	}{
		{"emoji", "<!-- Emoji: 🚀 -->\n"},
		{"content appearance", "<!-- Content-Appearance: fixed -->\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := docsSpace(t)
			dir := t.TempDir()
			file := writeFile(t, dir, "doc.md", header+"\nbody\n")

			config := Config{
				BaseURL: server.URL, Username: "user", Password: "token",
				Files: file, Features: []string{"mention"}, Output: io.Discard,
				ChangesOnly: true,
			}
			api := confluence.NewAPI(server.URL, "user", "token", false)

			target, err := ProcessFile(file, api, config)
			require.NoError(t, err)
			published := server.Page(target.ID).Version

			// Unchanged: skipped, which is the point of the flag.
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			require.Equal(t, published, server.Page(target.ID).Version)

			writeFile(t, dir, "doc.md", header+tt.extra+"\nbody\n")
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			assert.Greater(t, server.Page(target.ID).Version, published,
				"a changed %s header must update the page", tt.name)

			// And having been published, it is up to date again.
			updated := server.Page(target.ID).Version
			_, err = ProcessFile(file, confluence.NewAPI(server.URL, "user", "token", false), config)
			require.NoError(t, err)
			assert.Equal(t, updated, server.Page(target.ID).Version)
		})
	}
}

// TestContentFingerprintOfAPlainPageIsTheBodyHash pins what keeps an upgrade
// quiet: a page with neither header -- or with the appearance every page gets
// by default -- is fingerprinted exactly as it was before the headers counted,
// so the fingerprints already stamped on it still match.
func TestContentFingerprintOfAPlainPageIsTheBodyHash(t *testing.T) {
	const body = "<p>body</p>"

	assert.Equal(t, sha1Hash(body), contentFingerprint(body, "", ""))
	assert.Equal(t, sha1Hash(body), contentFingerprint(body, "full-width", ""))
	assert.NotEqual(t, sha1Hash(body), contentFingerprint(body, "fixed", ""))
	assert.NotEqual(t, sha1Hash(body), contentFingerprint(body, "full-width", "🚀"))
	assert.NotEqual(t, contentFingerprint(body, "fixed", "🚀"), contentFingerprint(body, "default", "🚀"))
}
