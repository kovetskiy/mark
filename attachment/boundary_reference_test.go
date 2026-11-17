package attachment

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// referenceTree is a directory laid out to trip a containment check: nested
// directories, symbolic links to files and directories inside and outside,
// chains and a loop of links, a dangling link, names with ".." in them,
// spaces and Unicode, and prefix siblings ("docs" beside "docs2" and
// "docs.bak", "work" beside "work2"). Its work directory is the working
// directory.
type referenceTree struct {
	top, work string

	roots []Boundary
	bases []string
	parts []string
}

func newReferenceTree(tb testing.TB) referenceTree {
	tb.Helper()

	top, err := filepath.EvalSymlinks(tb.TempDir())
	require.NoError(tb, err)

	work := filepath.Join(top, "work")

	files := []string{
		"work/docs/a.txt",
		"work/docs/sub/b.txt",
		"work/docs/sub/s2/f.txt",
		"work/docs/sp ace/ü.txt",
		"work/docs/..x/c.txt",
		"work/docs/a..b/d.txt",
		"work/docs2/x.txt",
		"work/docs.bak/y.txt",
		"work/shared/s.txt",
		"work/deep/er/e.txt",
		"work2/z.txt",
		"outside/o.txt",
		"outside/docs/a.txt",
		// Where a link climbing above an fs.FS of the work directory would
		// land if the climb stopped at its root.
		"work/outside/o.txt",
	}
	for _, name := range files {
		name = filepath.Join(top, filepath.FromSlash(name))
		require.NoError(tb, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(tb, os.WriteFile(name, []byte(name), 0o600))
	}

	links := [][2]string{
		{"work/docs/in-file", "../shared/s.txt"},
		{"work/docs/in-dir", "sub"},
		{"work/docs/out-file", filepath.Join(top, "outside", "o.txt")},
		{"work/docs/out-dir", "../../outside"},
		{"work/docs/out-abs-dir", filepath.Join(top, "outside")},
		{"work/docs/chain1", "chain2"},
		{"work/docs/chain2", "chain3"},
		{"work/docs/chain3", "../shared/s.txt"},
		{"work/docs/chainout1", "chainout2"},
		{"work/docs/chainout2", "out-file"},
		{"work/docs/loop1", "loop2"},
		{"work/docs/loop2", "loop1"},
		{"work/docs/dangling", "nothere"},
		{"work/docs/dangling-out", "../../outside/nothere"},
		// Absolute, and missing on disk, but there in an fs.FS of the work
		// directory if the link were read from the fs.FS's root.
		{"work/docs/abs-shared", "/shared/s.txt"},
		{"work/docs/updir", ".."},
		{"work/docs/sub/top", "../.."},
		{"work/docs/sub/s2/back", filepath.Join(top, "work")},
		{"work/docs/sub/s2/shallow", "../../../shared"},
		{"work/rootlink", "docs"},
		{"work/outlink", "../outside"},
		{"work/work2link", "../work2"},
	}
	for _, link := range links {
		if err := os.Symlink(link[1], filepath.Join(top, filepath.FromSlash(link[0]))); err != nil {
			tb.Skipf("no symbolic links here: %v", err)
		}
	}

	tb.Chdir(work)

	return referenceTree{
		top:  top,
		work: work,
		roots: []Boundary{
			"", BoundaryDocument,
			"docs", "docs/", "./docs", "docs//sub", "docs/sub/..", "docs2", "docs.bak",
			"rootlink", "../work/docs", "outlink/..", "docs/in-dir",
			Boundary(work), Boundary(top), "/", Boundary(filepath.Join(top, "outside")), "outlink", "work2link",
		},
		bases: []string{
			"docs", "docs/sub", ".", filepath.Join(work, "docs"), "rootlink", "docs/in-dir", "docs/out-dir",
			"docs/sub/s2", "docs2", "./docs/", "docs/../docs",
		},
		parts: []string{
			"..", ".", "", "docs", "docs2", "docs.bak", "sub", "s2", "back", "top", "shallow",
			"in-dir", "out-dir", "out-abs-dir", "updir", "rootlink", "outlink", "work2link",
			"a.txt", "b.txt", "f.txt", "o.txt", "x.txt", "s.txt", "z.txt", "in-file", "out-file",
			"chain1", "chainout1", "loop1", "dangling", "dangling-out", "abs-shared", "sp ace", "ü.txt", "..x", "a..b",
			"c.txt", "work", "work2", "outside", "nothere", "shared",
		},
	}
}

// names are the names checked: a fixed list of the cases each part was made
// for, and n generated from the parts with rng.
func (tr referenceTree) names(rng *rand.Rand, n int) []string {
	names := []string{
		"a.txt", "sub/b.txt", "../shared/s.txt", "../docs2/x.txt", "../docs.bak/y.txt",
		"in-file", "in-dir/b.txt", "out-file", "out-dir/o.txt", "out-abs-dir/o.txt",
		"chain1", "chainout1", "loop1", "dangling", "dangling-out", "abs-shared", "updir/shared/s.txt",
		"sub/top/shared/s.txt", "sub/s2/back/docs/a.txt", "sub/s2/back/../outside/o.txt",
		"sub/s2/back/../../etc", "sub/s2/shallow/s.txt", "sp ace/ü.txt", "..x/c.txt", "a..b/d.txt",
		"./a.txt", "sub//b.txt", "sub/", "sub/./b.txt", "../../outside/o.txt", "../../work2/z.txt",
		"../work2link/z.txt", ".", "..", "", "nothere", "../nothere/../docs/a.txt",
		filepath.Join(tr.work, "docs", "a.txt"), filepath.Join(tr.top, "outside", "o.txt"),
		tr.work + "/docs/sub/s2/back/../../outside/o.txt", tr.work + "/docs/sub/s2/back/../work/docs/a.txt",
		tr.work + "//docs/./a.txt", tr.work + "2/z.txt", "/",
	}

	for range n {
		parts := make([]string, 1+rng.IntN(5))
		for i := range parts {
			parts[i] = tr.parts[rng.IntN(len(tr.parts))]
		}

		name := strings.Join(parts, "/")

		switch rng.IntN(10) {
		case 0:
			name = tr.work + "/" + name
		case 1:
			name = tr.top + "/" + name
		case 2:
			name += "/"
		}

		names = append(names, name)
	}

	return names
}

// absolute is name made absolute against the working directory without
// cleaning it, so that a ".." after a link is still there to be followed.
func (tr referenceTree) absolute(name string) string {
	if filepath.IsAbs(name) {
		return name
	}

	return tr.work + "/" + name
}

// errLoop is a chain of links too long to follow.
var errLoop = errors.New("too many links")

// physical is name, an absolute path, as the operating system walks it:
// each link followed where it stands, and ".." taken from where the walk is
// rather than from the name as written. It fails where a part is missing or
// a chain of links does not end.
func physical(name string) (string, error) {
	rest := strings.Split(name, "/")
	at := "/"
	links := 0

	for len(rest) > 0 {
		part := rest[0]
		rest = rest[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			at = filepath.Dir(at)

			continue
		}

		next := filepath.Join(at, part)

		info, err := os.Lstat(next)
		if err != nil {
			return "", err
		}

		if info.Mode()&fs.ModeSymlink == 0 {
			at = next

			continue
		}

		if links++; links > 255 {
			return "", errLoop
		}

		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}

		if filepath.IsAbs(target) {
			at = "/"
		}

		rest = append(strings.Split(target, "/"), rest...)
	}

	return at, nil
}

// deepest is name, an absolute path, walked by physical as far as it can be,
// with the rest of it appended as written: a file that is not there is still
// somewhere, and a misspelt name is left for the open to report.
func deepest(name string) string {
	parts := strings.Split(name, "/")

	for i := len(parts); i >= 0; i-- {
		walked, err := physical("/" + strings.Join(parts[:i], "/"))
		if err == nil {
			return filepath.Join(append([]string{walked}, parts[i:]...)...)
		}
	}

	return filepath.Clean(name)
}

// under reports whether file is root or below it, both absolute and clean.
func under(root, file string) bool {
	rel, err := filepath.Rel(root, file)

	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, "../")
}

// verdict is what the reference makes of a name: allowed, or refused by the
// default boundary or by the attachment root.
type verdict string

const (
	allowed          verdict = "allowed"
	refusedByDefault verdict = "refused by the default boundary"
	refusedByRoot    verdict = "refused by the attachment root"
)

// onDisk is the reference for a Root on the local disk: the path that will
// be read is walked as the operating system walks it, and it has to be under
// the document's directory or the working directory, walked the same way,
// and then under the root.
func (tr referenceTree) onDisk(b Boundary, base, read string) verdict {
	file := deepest(tr.absolute(read))
	baseDir := deepest(tr.absolute(base))

	if !under(baseDir, file) && !under(deepest(tr.work), file) {
		return refusedByDefault
	}

	switch b {
	case "":
		return allowed
	case BoundaryDocument:
		if under(baseDir, file) {
			return allowed
		}
	default:
		if under(deepest(tr.absolute(string(b))), file) {
			return allowed
		}
	}

	return refusedByRoot
}

// errOutward is a walk through an fs.FS that leaves it.
var errOutward = errors.New("leaves the fs.FS")

// inFS is name, a valid path in the fs.FS on dir, walked as the fs.FS's links
// are documented to be followed: only to a relative target, and never above
// its root.
func inFS(dir, name string) (string, error) {
	rest := strings.Split(name, "/")
	at := "."
	links := 0

	for len(rest) > 0 {
		part := rest[0]
		rest = rest[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			if at == "." {
				return "", errOutward
			}

			at = path.Dir(at)

			continue
		}

		next := path.Join(at, part)

		info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(next)))
		if err != nil {
			return "", err
		}

		if info.Mode()&fs.ModeSymlink == 0 {
			at = next

			continue
		}

		if links++; links > maxLinks {
			return "", errLoop
		}

		target, err := os.Readlink(filepath.Join(dir, filepath.FromSlash(next)))
		if err != nil {
			return "", err
		}

		if target == "" || filepath.IsAbs(target) {
			return "", errOutward
		}

		rest = append(strings.Split(target, "/"), rest...)
	}

	return at, nil
}

// deepestInFS is deepest for inFS.
func deepestInFS(dir, name string) string {
	parts := strings.Split(name, "/")

	for i := len(parts); i > 0; i-- {
		walked, err := inFS(dir, strings.Join(parts[:i], "/"))
		if err == nil {
			return path.Join(append([]string{walked}, parts[i:]...)...)
		}
	}

	return name
}

// underInFS is under for slash paths inside an fs.FS.
func underInFS(root, file string) bool {
	return root == "." || file == root || strings.HasPrefix(file, root+"/")
}

// fsVerdict is the reference for a Root reading from the fs.FS on dir, or
// from an fs.FS with no links when dir is empty: a name has to be a path
// inside it, its links have to stay inside it, and under BoundaryDocument it
// has to be under the document's directory.
func fsVerdict(dir string, b Boundary, base, joined string) verdict {
	if isAbs(joined) {
		return refusedByDefault
	}

	name := path.Clean(filepath.ToSlash(joined))
	if !fs.ValidPath(name) {
		return refusedByDefault
	}

	baseName := path.Clean(filepath.ToSlash(base))

	if dir == "" {
		if b == BoundaryDocument && !underInFS(baseName, name) {
			return refusedByRoot
		}

		return allowed
	}

	if _, err := inFS(dir, name); errors.Is(err, errOutward) {
		return refusedByDefault
	}

	if b == BoundaryDocument && !underInFS(deepestInFS(dir, baseName), deepestInFS(dir, name)) {
		return refusedByRoot
	}

	return allowed
}

// mirrorFS is the tree's work directory as an fs.FS with its regular files
// and none of its links.
func (tr referenceTree) mirrorFS(tb testing.TB) fstest.MapFS {
	tb.Helper()

	mirror := fstest.MapFS{}
	err := filepath.WalkDir(tr.work, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}

		rel, err := filepath.Rel(tr.work, name)
		if err != nil {
			return err
		}

		mirror[filepath.ToSlash(rel)] = &fstest.MapFile{Data: []byte(rel)}

		return nil
	})
	require.NoError(tb, err)

	return mirror
}

// referenceCase is one check of a name by the real code against the
// reference.
type referenceCase struct {
	where    string
	root     Root
	boundary Boundary
	base     string
	name     string
	readable bool
}

// compare runs c through the real check and the reference, and returns a
// description of where they disagree, or "" where they agree.
func (tr referenceTree) compare(c referenceCase) string {
	var err error
	if c.readable {
		err = c.root.CheckReadable(c.base, c.name)
	} else {
		err = c.root.checkAttachment(c.base, c.name)
	}

	if err != nil && !errors.Is(err, ErrOutsideProject) {
		return fmt.Sprintf("an error that is not ErrOutsideProject: %v", err)
	}

	var want verdict

	switch {
	case c.root.FS() == nil:
		read := filepath.Join(c.base, c.name)
		if c.readable && filepath.IsAbs(c.name) {
			read = c.name
		}

		want = tr.onDisk(c.boundary, c.base, read)
	default:
		joined := filepath.Join(c.base, c.name)
		if c.readable && isAbs(c.name) {
			joined = c.name
		}

		dir := ""
		if c.where == "os.DirFS" {
			dir = tr.work
		}

		want = fsVerdict(dir, c.boundary, c.base, joined)
	}

	switch {
	case want == allowed && err != nil:
		return fmt.Sprintf("refused, the reference allows: %v", err)
	case want != allowed && err == nil:
		return "allowed, the reference says " + string(want)
	case err != nil && c.root.FS() == nil && c.boundary != "":
		// Only on disk does an attachment root sit beside a second limit
		// that the message has to tell apart.
		byDefault := strings.Contains(err.Error(), "the default boundary, which")
		if byDefault != (want == refusedByDefault) {
			return fmt.Sprintf("the reference says %s, the message: %v", want, err)
		}
	}

	return ""
}

// cases is every combination of the tree's roots, bases and names, through
// each Root: on disk, an os.DirFS of the work directory, and a MapFS of its
// regular files.
func (tr referenceTree) cases(tb testing.TB, names []string) []referenceCase {
	tb.Helper()

	dirFS := FSRoot(os.DirFS(tr.work))
	mapFS := FSRoot(tr.mirrorFS(tb))

	var cases []referenceCase

	for _, b := range tr.roots {
		for _, base := range tr.bases {
			for _, name := range names {
				for _, readable := range []bool{false, true} {
					cases = append(cases, referenceCase{"disk", Root{}.WithBoundary(b), b, base, name, readable})

					// A directory root holds nothing of an fs.FS; a test of
					// its own says so.
					if b.NamesDirectory() || filepath.IsAbs(base) {
						continue
					}

					cases = append(cases,
						referenceCase{"os.DirFS", dirFS.WithBoundary(b), b, base, name, readable},
						referenceCase{"fstest.MapFS", mapFS.WithBoundary(b), b, base, name, readable},
					)
				}
			}
		}
	}

	return cases
}

// TestBoundaryAgreesWithAReference holds the containment check to a simpler
// reading of it over generated names: every combination of the tree's roots,
// bases and names, on disk and in an fs.FS, through checkAttachment and
// CheckReadable.
//
// The reference walks a path as the operating system does, by its own code
// rather than filepath.EvalSymlinks, and compares with filepath.Rel. Two
// things it shares with the check by design: a name joined to the
// document's directory is cleaned first, since that cleaned name is the one
// opened; and only the deepest part of a path that resolves is followed, so a
// dangling link or a loop counts as the link's own name, which the open then
// fails on with nothing read. In an fs.FS it follows the documented rule for
// links: a relative target only, never above the root.
func TestBoundaryAgreesWithAReference(t *testing.T) {
	tr := newReferenceTree(t)

	cases := tr.cases(t, tr.names(rand.New(rand.NewPCG(1, 2)), 60))

	failures := 0
	for _, c := range cases {
		if got := tr.compare(c); got != "" {
			failures++
			if failures <= 20 {
				t.Errorf("%s, root %q, base %q, name %q, readable %v: %s",
					c.where, c.boundary, c.base, c.name, c.readable, got)
			}
		}
	}

	if failures > 20 {
		t.Errorf("and %d more", failures-20)
	}

	t.Logf("%d checks", len(cases))
}

// FuzzBoundaryAgreesWithAReference is TestBoundaryAgreesWithAReference for
// names the fuzzer makes.
func FuzzBoundaryAgreesWithAReference(f *testing.F) {
	tr := newReferenceTree(f)

	for _, name := range tr.names(rand.New(rand.NewPCG(3, 4)), 50) {
		f.Add(uint8(0), uint8(0), name, false, uint8(0))
	}

	dirFS := FSRoot(os.DirFS(tr.work))

	f.Fuzz(func(t *testing.T, rootIndex, baseIndex uint8, name string, readable bool, where uint8) {
		if strings.ContainsRune(name, 0) {
			t.Skip("a NUL is no path")
		}

		b := tr.roots[int(rootIndex)%len(tr.roots)]
		base := tr.bases[int(baseIndex)%len(tr.bases)]

		c := referenceCase{"disk", Root{}.WithBoundary(b), b, base, name, readable}
		if where%2 == 1 && !b.NamesDirectory() && !filepath.IsAbs(base) {
			c = referenceCase{"os.DirFS", dirFS.WithBoundary(b), b, base, name, readable}
		}

		if got := tr.compare(c); got != "" {
			t.Errorf("%s, root %q, base %q, name %q, readable %v: %s", c.where, b, base, name, readable, got)
		}
	})
}
