package page

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
)

func TestImmediateParentID(t *testing.T) {
	t.Parallel()

	if got := ImmediateParentID(nil); got != "" {
		t.Fatalf("nil page: got %q", got)
	}
	if got := ImmediateParentID(&confluence.PageInfo{}); got != "" {
		t.Fatalf("no ancestors: got %q", got)
	}

	pg := &confluence.PageInfo{
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{
			{ID: "root", Title: "Root"},
			{ID: "folder-1", Title: "Folder"},
		},
	}
	if got := ImmediateParentID(pg); got != "folder-1" {
		t.Fatalf("got %q, want folder-1", got)
	}
}
