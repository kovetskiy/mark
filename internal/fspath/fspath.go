// Package fspath compares paths on disk the way the files they name compare,
// whatever symlinks they were reached through.
package fspath

import "path/filepath"

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
