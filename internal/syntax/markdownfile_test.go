package syntax

import "testing"

func TestIsMarkdownFile(t *testing.T) {
	for path, want := range map[string]bool{
		"a.md": true, "a.MD": true, "dir/a.markdown": true, "a.mdown": true,
		"a.mkd": true, "a.mkdn": true, "a.mdwn": true,
		"a.sh": false, "a.txt": false, "a": false, "a.md.bak": false,
	} {
		if got := IsMarkdownFile(path); got != want {
			t.Errorf("IsMarkdownFile(%q) = %v, want %v", path, got, want)
		}
	}
}
