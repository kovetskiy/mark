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

func TestPageUnderParents(t *testing.T) {
	t.Parallel()

	anchor := []string{"NinjaOne"}
	pg := &confluence.PageInfo{
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{{ID: "1", Title: "MSP"}, {ID: "2", Title: "NinjaOne"}},
	}
	if !pageUnderParents(pg, anchor) {
		t.Fatal("expected page under NinjaOne anchor")
	}
	if pageUnderParents(&confluence.PageInfo{Ancestors: []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{{ID: "1", Title: "MSP"}}}, anchor) {
		t.Fatal("expected page outside NinjaOne anchor")
	}
}

// Confluence compares titles without regard to case, so an ancestor that
// differs from a declared parent only in case is that parent.
func TestPageUnderParentsIgnoresCase(t *testing.T) {
	t.Parallel()

	pg := &confluence.PageInfo{
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{{ID: "1", Title: "MSP"}, {ID: "2", Title: "NinjaOne"}},
	}
	if !pageUnderParents(pg, []string{"ninjaone"}) {
		t.Fatal("expected page under ninjaone anchor")
	}
	if !UnderDeclaredParents(pg, []string{"msp", "NINJAONE"}) {
		t.Fatal("expected page under msp > NINJAONE")
	}
	if UnderDeclaredParents(pg, []string{"msp", "Other"}) {
		t.Fatal("expected page outside msp > Other")
	}
}

// A header that differs from the existing parent's title only in case names
// the page a real run leaves alone.
func TestWouldMoveIgnoresCaseOfDeclaredParentTitle(t *testing.T) {
	t.Parallel()

	pg := &confluence.PageInfo{
		ID: "2",
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{{ID: "1", Title: "My Parent"}},
	}

	if WouldMove(t.Context(), nil, pg, &confluence.PageInfo{ID: "1", Title: "My Parent"}, []string{"my parent"}, false) {
		t.Fatal("a parent differing only in case is not a move")
	}

	elsewhere := &confluence.PageInfo{
		ID: "2",
		Ancestors: []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{{ID: "8", Title: "Other"}},
	}
	if !WouldMove(t.Context(), nil, elsewhere, &confluence.PageInfo{ID: "1", Title: "My Parent"}, []string{"my parent"}, false) {
		t.Fatal("a page under another parent is still a move")
	}
}
