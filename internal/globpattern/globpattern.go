// Package globpattern reads and writes the doublestar patterns mark keeps in
// the page manifest, spelled with forward slashes as manifest.Key spells them.
package globpattern

import "strings"

// HasMeta reports whether a pattern holds an unescaped *, ?, [ or {, and so
// may match more than the one path it spells. A backslash escapes the
// character after it: on Windows, where it is a separator, the pattern has
// been through filepath.ToSlash first, which leaves no backslash to read.
func HasMeta(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '*', '?', '[', '{':
			return true
		}
	}

	return false
}

// Escape makes a path a pattern matching only itself.
func Escape(path string) string {
	var escaped strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`\*?[]{},`, r) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(r)
	}

	return escaped.String()
}
