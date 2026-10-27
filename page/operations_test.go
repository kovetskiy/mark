package page_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveUnderParent(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	b := server.AddPage("DOCS", "B", "page", root.ID)
	child := server.AddPage("DOCS", "B child", "page", b.ID)

	var out bytes.Buffer
	require.NoError(t, page.Move(context.Background(), api, page.Ref{ID: b.ID}, page.MoveOptions{Parent: &page.Ref{ID: a.ID}}, &out))

	assert.Equal(t, a.ID, server.Page(b.ID).ParentID)
	assert.Equal(t, b.ID, server.Page(child.ID).ParentID, "the page's children go with it")
	assert.Contains(t, out.String(), `moved "B"`)
}

func TestMoveByTitle(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	b := server.AddPage("DOCS", "B", "page", root.ID)

	err := page.Move(context.Background(), api, page.Ref{Space: "DOCS", Title: "B"},
		page.MoveOptions{Parent: &page.Ref{Space: "DOCS", Title: "A"}}, &bytes.Buffer{})
	require.NoError(t, err)

	assert.Equal(t, a.ID, server.Page(b.ID).ParentID)
}

func TestMoveRefusesItsOwnSubtree(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	grandchild := server.AddPage("DOCS", "A grandchild", "page",
		server.AddPage("DOCS", "A child", "page", a.ID).ID)

	for _, parent := range []string{a.ID, grandchild.ID} {
		err := page.Move(context.Background(), api, page.Ref{ID: a.ID}, page.MoveOptions{Parent: &page.Ref{ID: parent}}, &bytes.Buffer{})
		require.ErrorContains(t, err, "beneath itself")
	}
	assert.Equal(t, root.ID, server.Page(a.ID).ParentID)
}

func TestMoveRefusesAnotherSpace(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.AddSpace("OTHER")
	a := server.AddPage("DOCS", "A", "page", server.AddPage("DOCS", "Root", "page", "").ID)
	elsewhere := server.AddPage("OTHER", "Elsewhere", "page", "")

	err := page.Move(context.Background(), api, page.Ref{ID: a.ID}, page.MoveOptions{Parent: &page.Ref{ID: elsewhere.ID}}, &bytes.Buffer{})
	require.ErrorContains(t, err, "only within their space")
}

func TestMoveBesideSibling(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	x := server.AddPage("DOCS", "X", "page", root.ID)
	y := server.AddPage("DOCS", "Y", "page", root.ID)
	z := server.AddPage("DOCS", "Z", "page", root.ID)

	require.NoError(t, page.Move(context.Background(), api, page.Ref{ID: z.ID}, page.MoveOptions{Before: &page.Ref{ID: x.ID}}, &bytes.Buffer{}))
	assert.Equal(t, []string{z.ID, x.ID, y.ID}, server.ChildOrder(root.ID))

	require.NoError(t, page.Move(context.Background(), api, page.Ref{ID: z.ID}, page.MoveOptions{After: &page.Ref{ID: y.ID}}, &bytes.Buffer{}))
	assert.Equal(t, []string{x.ID, y.ID, z.ID}, server.ChildOrder(root.ID))
}

func TestMoveBesideChecksTheParent(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	x := server.AddPage("DOCS", "X", "page", root.ID)
	y := server.AddPage("DOCS", "Y", "page", root.ID)
	other := server.AddPage("DOCS", "Other", "page", root.ID)

	err := page.Move(context.Background(), api, page.Ref{ID: y.ID},
		page.MoveOptions{Parent: &page.Ref{ID: other.ID}, Before: &page.Ref{ID: x.ID}}, &bytes.Buffer{})
	require.ErrorContains(t, err, "is not a child of")
}

func TestMoveDryRunAndNoOp(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	b := server.AddPage("DOCS", "B", "page", root.ID)
	server.ResetRequests()

	var out bytes.Buffer
	require.NoError(t, page.Move(context.Background(), api, page.Ref{ID: b.ID},
		page.MoveOptions{Parent: &page.Ref{ID: a.ID}, DryRun: true}, &out))
	assert.Contains(t, out.String(), "would move")

	out.Reset()
	require.NoError(t, page.Move(context.Background(), api, page.Ref{ID: b.ID}, page.MoveOptions{Parent: &page.Ref{ID: root.ID}}, &out))
	assert.Contains(t, out.String(), "already under")

	assert.Equal(t, root.ID, server.Page(b.ID).ParentID)
	assert.Zero(t, server.CountRequests("PUT", "/rest/api/content"), "nothing was moved")
}

func TestRename(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "Old", "page", root.ID)
	server.EditPage(a.ID, "<p>kept</p>")
	before := server.Page(a.ID).Version

	var out bytes.Buffer
	require.NoError(t, page.Rename(context.Background(), api, page.Ref{Space: "DOCS", Title: "Old"}, " New ", false, &out))

	renamed := server.Page(a.ID)
	assert.Equal(t, "New", renamed.Title)
	assert.Equal(t, "<p>kept</p>", renamed.Body)
	assert.Equal(t, root.ID, renamed.ParentID)
	assert.Equal(t, before+1, renamed.Version)
	assert.Equal(t, `renamed from "Old"`, renamed.Message)
	assert.Contains(t, out.String(), `to "New"`)
}

func TestRenameRefusesATakenTitle(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	server.AddPage("DOCS", "B", "page", root.ID)

	err := page.Rename(context.Background(), api, page.Ref{ID: a.ID}, "B", false, &bytes.Buffer{})
	require.ErrorContains(t, err, "already has a page titled")
	assert.Equal(t, "A", server.Page(a.ID).Title)

	require.ErrorContains(t, page.Rename(context.Background(), api, page.Ref{ID: a.ID}, "  ", false, &bytes.Buffer{}), "needs a title")
}

func TestRenameDryRun(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	a := server.AddPage("DOCS", "A", "page", server.AddPage("DOCS", "Root", "page", "").ID)

	var out bytes.Buffer
	require.NoError(t, page.Rename(context.Background(), api, page.Ref{ID: a.ID}, "B", true, &out))
	assert.Contains(t, out.String(), "would rename")
	assert.Equal(t, "A", server.Page(a.ID).Title)
}

func TestDelete(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	leaf := server.AddPage("DOCS", "Leaf", "page", root.ID)

	require.NoError(t, page.Delete(context.Background(), api, page.Ref{ID: leaf.ID}, false, false, &bytes.Buffer{}))
	assert.True(t, server.Page(leaf.ID).Trashed)
}

func TestDeleteRecursive(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	top := server.AddPage("DOCS", "Top", "page", root.ID)
	child := server.AddPage("DOCS", "Child", "page", top.ID)
	grandchild := server.AddPage("DOCS", "Grandchild", "page", child.ID)
	sibling := server.AddPage("DOCS", "Sibling", "page", top.ID)

	err := page.Delete(context.Background(), api, page.Ref{ID: top.ID}, false, false, &bytes.Buffer{})
	require.ErrorContains(t, err, "pass --recursive")
	assert.False(t, server.Page(top.ID).Trashed)

	var out bytes.Buffer
	require.NoError(t, page.Delete(context.Background(), api, page.Ref{ID: top.ID}, true, true, &out))
	assert.Equal(t, `would trash "Grandchild" (`+grandchild.ID+`)
would trash "Child" (`+child.ID+`)
would trash "Sibling" (`+sibling.ID+`)
would trash "Top" (`+top.ID+`)
`, out.String(), "deepest first")
	assert.False(t, server.Page(top.ID).Trashed, "a dry run trashes nothing")

	require.NoError(t, page.Delete(context.Background(), api, page.Ref{ID: top.ID}, true, false, &bytes.Buffer{}))
	for _, id := range []string{top.ID, child.ID, grandchild.ID, sibling.ID} {
		assert.True(t, server.Page(id).Trashed, id)
	}
	assert.False(t, server.Page(root.ID).Trashed)
}

func TestDeleteRefusesTheHomepageAndFolders(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	server.SetHomepage("DOCS", root.ID)
	holder := server.AddPage("DOCS", "Holder", "page", root.ID)
	server.AddFolder("DOCS", "Folder", holder.ID, "page")

	require.ErrorContains(t, page.Delete(context.Background(), api, page.Ref{ID: root.ID}, true, false, &bytes.Buffer{}), "homepage")
	require.ErrorContains(t, page.Delete(context.Background(), api, page.Ref{ID: holder.ID}, true, false, &bytes.Buffer{}), "folder")
	require.ErrorContains(t, page.Archive(context.Background(), api, page.Ref{ID: root.ID}, false, &bytes.Buffer{}), "homepage")
	assert.False(t, server.Page(root.ID).Trashed)
	assert.False(t, server.Page(holder.ID).Trashed)
}

func TestArchive(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	parent := server.AddPage("DOCS", "Parent", "page", root.ID)
	leaf := server.AddPage("DOCS", "Leaf", "page", parent.ID)

	require.ErrorContains(t, page.Archive(context.Background(), api, page.Ref{ID: parent.ID}, false, &bytes.Buffer{}), "child page")

	var out bytes.Buffer
	require.NoError(t, page.Archive(context.Background(), api, page.Ref{ID: leaf.ID}, true, &out))
	assert.Contains(t, out.String(), "would archive")
	assert.False(t, server.Page(leaf.ID).Archived)

	require.NoError(t, page.Archive(context.Background(), api, page.Ref{ID: leaf.ID}, false, &bytes.Buffer{}))
	assert.True(t, server.Page(leaf.ID).Archived)
}

func TestWatch(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	p := server.AddPage("DOCS", "Page", "page", "")

	var out bytes.Buffer
	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: p.ID}, nil, true, false, true, &out))
	assert.Equal(t, `would watch "Page" (`+p.ID+`) for you`+"\n", out.String())
	assert.Empty(t, server.Watchers(p.ID))

	out.Reset()
	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: p.ID}, nil, true, false, false, &out))
	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: p.ID}, nil, true, false, false, &out))
	assert.Equal(t, `watched "Page" (`+p.ID+`) for you`+"\n"+
		`you are already watching "Page" (`+p.ID+`)`+"\n", out.String())
	assert.Equal(t, []string{"acct-current"}, server.Watchers(p.ID))

	out.Reset()
	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: p.ID}, nil, false, false, false, &out))
	assert.Equal(t, `unwatched "Page" (`+p.ID+`) for you`+"\n", out.String())
	assert.Empty(t, server.Watchers(p.ID))
}

func TestWatchRecursive(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	top := server.AddPage("DOCS", "Top", "page", root.ID)
	child := server.AddPage("DOCS", "Child", "page", top.ID)
	grandchild := server.AddPage("DOCS", "Grandchild", "page", child.ID)
	require.NoError(t, api.WatchPage(context.Background(), child.ID, ""))

	var out bytes.Buffer
	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: top.ID}, nil, true, true, false, &out))
	assert.Equal(t, `watched "Top" (`+top.ID+`) for you
you are already watching "Child" (`+child.ID+`)
watched "Grandchild" (`+grandchild.ID+`) for you
`, out.String(), "from the top down")
	for _, id := range []string{top.ID, child.ID, grandchild.ID} {
		assert.Equal(t, []string{"acct-current"}, server.Watchers(id), id)
	}
	assert.Empty(t, server.Watchers(root.ID))

	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: top.ID}, nil, false, true, false, &bytes.Buffer{}))
	for _, id := range []string{top.ID, child.ID, grandchild.ID} {
		assert.Empty(t, server.Watchers(id), id)
	}
}

func TestWatchRecursiveRefusesAFolderBeforeWatchingAnything(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	top := server.AddPage("DOCS", "Top", "page", "")
	child := server.AddPage("DOCS", "Child", "page", top.ID)
	server.AddFolder("DOCS", "Folder", child.ID, "page")

	err := page.Watch(context.Background(), api, page.Ref{ID: top.ID}, nil, true, true, false, &bytes.Buffer{})
	require.ErrorContains(t, err, "does not reach into folders")
	assert.Empty(t, server.Watchers(top.ID), "nothing is watched when the tree cannot be")

	require.NoError(t, page.Watch(context.Background(), api, page.Ref{ID: top.ID}, nil, true, false, false, &bytes.Buffer{}),
		"without --recursive the folder is no concern")
}
