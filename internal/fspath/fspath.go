// Package fspath compares paths on disk the way the files they name compare,
// whatever symlinks they were reached through.
package fspath

import (
	"path/filepath"
	"strings"
)

// ResolveDeepest resolves the longest leading part of a path that exists and
// re-appends whatever is left.
//
// Both sides of the comparison have to be in the same terms or a directory is
// found not to contain the file sitting in it: with mark run in
// "/mnt/data/work" and the document named through a link as
// "~/work/docs/x.md", "/home/me/work/docs/logo.png" is not relative to
// "/mnt/data/work", however plainly it sits beside its own document. Symlinked
// checkouts, bind-mounted CI workspaces and every path under macOS's /tmp
// arrive this way.
//
// The deepest part rather than the whole path, because the file itself need not
// exist: a name that is merely misspelled should be refused by the open, which
// says so, rather than by the boundary, which would say something else.
func ResolveDeepest(path string) string {
	path = filepath.Clean(path)

	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Join(resolved, rest)
		}

		parent := filepath.Dir(path)
		if parent == path {
			// The root itself did not resolve; nothing is left to walk up to.
			return filepath.Join(path, rest)
		}

		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// Within is path relative to root, and false when path is not under it. Both
// are made absolute, so either may be relative to the working directory, and
// compared as given: a caller comparing files rather than names resolves both
// with ResolveDeepest first, or uses Under.
func Within(root, path string) (string, bool) {
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return "", false
	}

	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return rel, true
}

// Under is Within, and failing that Within with symlinks resolved as deep as
// each path exists, so that a root reached through one (every path under
// macOS's /tmp) still contains what its target does. A path under root as
// named counts even when it resolves elsewhere, so this is not a boundary
// check: one of those resolves both sides alone.
func Under(root, path string) (string, bool) {
	if rel, ok := Within(root, path); ok {
		return rel, true
	}

	return Within(ResolveDeepest(root), ResolveDeepest(path))
}
