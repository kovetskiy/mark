package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/require"
)

// TestAttachmentRootFlag: --attachment-root reaches the run from the command
// line, the environment and the configuration file alike, and "document" then
// refuses a file beside another document that the default boundary allows.
func TestAttachmentRootFlag(t *testing.T) {
	repo := t.TempDir()
	for _, dir := range []string{"guide", "other-doc"} {
		require.NoError(t, os.Mkdir(filepath.Join(repo, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, "other-doc", "file.pdf"), []byte("pdf"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "guide", "index.md"),
		[]byte("<!-- Space: DOCS -->\n<!-- Title: Guide -->\n\n[the file](../other-doc/file.pdf)\n"), 0o600))
	t.Chdir(repo)

	for _, tc := range []struct {
		name   string
		env    map[string]string
		config string
		args   []string
		fails  bool
	}{
		{name: "without it"},
		{name: "flag", args: []string{"--attachment-root", "document"}, fails: true},
		{name: "environment", env: map[string]string{"MARK_ATTACHMENT_ROOT": "document"}, fails: true},
		{name: "configuration file", config: "attachment-root = \"document\"\n", fails: true},
		{name: "a directory holding both", args: []string{"--attachment-root", repo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noDefaultConfig(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			server := confluencetest.New(t)
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)

			config := writeConfig(t, t.TempDir(), "mark.toml", tc.config)
			args := []string{"-c", config, "--color", "never", "-u", "user", "-p", "token", "publish",
				"-b", server.URL, "--compile-only", "--attach-referenced", "--files", "guide/index.md"}

			_, _, err := runMark(t, append(args, tc.args...)...)
			if tc.fails {
				require.ErrorContains(t, err, "outside the project")
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestAttachmentRootFlagRefusal: a directory that is not there is refused
// naming the flag and the field it sets.
func TestAttachmentRootFlagRefusal(t *testing.T) {
	noDefaultConfig(t)

	_, _, err := runMark(t, "--color", "never", "publish", "--compile-only",
		"--attachment-root", filepath.Join(t.TempDir(), "missing"), "--files", "missing.md")
	require.ErrorContains(t, err, "Rendering.AttachmentRoot (--attachment-root)")
}
