package syntax

import (
	"path/filepath"
	"strings"
)

// IsMarkdownFile reports whether path has one of the extensions Markdown is
// commonly saved under, whatever its case.
func IsMarkdownFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mkdn", ".mdwn":
		return true
	}

	return false
}
