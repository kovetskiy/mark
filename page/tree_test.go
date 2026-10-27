package page_test

import (
	"bytes"
	"testing"

	"github.com/kovetskiy/mark/v17/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTree(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)
	a1 := server.AddPage("DOCS", "A1", "page", a.ID)
	b := server.AddPage("DOCS", "B", "page", root.ID)
	folder := server.AddFolder("DOCS", "Folder", root.ID, "page")
	inFolder := server.AddPage("DOCS", "In folder", "page", folder.ID)
	gone := server.AddPage("DOCS", "Gone", "page", b.ID)
	server.DeletePage(gone.ID)

	var out bytes.Buffer
	require.NoError(t, page.Tree(api, page.Ref{ID: root.ID}, 0, &out))
	assert.Equal(t, `Root (`+root.ID+`)
├── A (`+a.ID+`)
│   └── A1 (`+a1.ID+`)
├── B (`+b.ID+`)
└── Folder [folder] (`+folder.ID+`)
    └── In folder (`+inFolder.ID+`)
`, out.String())

	out.Reset()
	require.NoError(t, page.Tree(api, page.Ref{ID: root.ID}, 2, &out))
	assert.Equal(t, `Root (`+root.ID+`)
├── A (`+a.ID+`)
├── B (`+b.ID+`)
└── Folder [folder] (`+folder.ID+`)
`, out.String(), "two levels, counting the page itself")

	out.Reset()
	require.NoError(t, page.Tree(api, page.Ref{ID: folder.ID}, 0, &out))
	assert.Equal(t, "Folder [folder] ("+folder.ID+")\n└── In folder ("+inFolder.ID+")\n", out.String(),
		"a folder is named by its id")
}

func TestTreeDataCenter(t *testing.T) {
	api, server := newAPI(t)
	server.SetDataCenter()
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	a := server.AddPage("DOCS", "A", "page", root.ID)

	var out bytes.Buffer
	require.NoError(t, page.Tree(api, page.Ref{Space: "DOCS", Title: "Root"}, 0, &out))
	assert.Equal(t, "Root ("+root.ID+")\n└── A ("+a.ID+")\n", out.String())
}

func TestCopy(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	source := server.AddPage("DOCS", "Guide", "page", root.ID)
	server.EditPage(source.ID, "<p>body</p>")
	server.AddLabel(source.ID, "kept")
	server.AddAttachmentData(source.ID, "diagram.png", []byte("png"))
	server.SetRestrictions(source.ID, "update", []string{"acct-current"}, nil)
	child := server.AddPage("DOCS", "Guide child", "page", source.ID)
	target := server.AddPage("DOCS", "Target", "page", root.ID)

	var out bytes.Buffer
	require.NoError(t, page.Copy(api, page.Ref{ID: source.ID},
		page.CopyOptions{Parent: page.Ref{ID: target.ID}, Title: "Guide (copy)"}, &out))

	copies := server.ChildOrder(target.ID)
	require.Len(t, copies, 1, "without --recursive only the page is copied")
	made := server.Page(copies[0])
	assert.Equal(t, "Guide (copy)", made.Title)
	assert.Equal(t, "<p>body</p>", made.Body)
	assert.Equal(t, []string{"kept"}, made.Labels)
	assert.Len(t, server.Attachments(made.ID), 1)
	assert.Empty(t, made.UpdateRestrictedTo, "restrictions stay behind")
	assert.Contains(t, out.String(), `copied "Guide"`)
	assert.Equal(t, source.ID, server.Page(child.ID).ParentID, "the source is untouched")
}

func TestCopyRecursive(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	server.AddSpace("OTHER")
	root := server.AddPage("DOCS", "Root", "page", "")
	source := server.AddPage("DOCS", "Guide", "page", root.ID)
	first := server.AddPage("DOCS", "First", "page", source.ID)
	server.AddPage("DOCS", "Deep", "page", first.ID)
	server.AddPage("DOCS", "Second", "page", source.ID)
	elsewhere := server.AddPage("OTHER", "Elsewhere", "page", "")

	t.Run("a title the space has stops it before the first copy", func(t *testing.T) {
		err := page.Copy(api, page.Ref{ID: source.ID},
			page.CopyOptions{Parent: page.Ref{ID: root.ID}, Title: "New guide", Recursive: true}, &bytes.Buffer{})
		require.ErrorContains(t, err, `"First"`)
		require.ErrorContains(t, err, "--prefix")
		assert.Len(t, server.ChildOrder(root.ID), 1, "nothing was copied")
	})

	t.Run("a dry run shows the plan", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, page.Copy(api, page.Ref{ID: source.ID},
			page.CopyOptions{Parent: page.Ref{ID: root.ID}, Prefix: "Copy of ", Recursive: true, DryRun: true}, &out))
		assert.Equal(t, `would copy "Guide" (`+source.ID+`) as "Copy of Guide" under "Root" (`+root.ID+`)
would copy "First" (`+first.ID+`) as "Copy of First" under the copy of "Guide"
would copy "Deep" (`+server.ChildOrder(first.ID)[0]+`) as "Copy of Deep" under the copy of "First"
would copy "Second" (`+server.ChildOrder(source.ID)[1]+`) as "Copy of Second" under the copy of "Guide"
`, out.String())
		assert.Len(t, server.ChildOrder(root.ID), 1)
	})

	t.Run("into another space, under the copy of each parent", func(t *testing.T) {
		require.NoError(t, page.Copy(api, page.Ref{ID: source.ID},
			page.CopyOptions{Parent: page.Ref{ID: elsewhere.ID}, Recursive: true}, &bytes.Buffer{}))

		var out bytes.Buffer
		require.NoError(t, page.Tree(api, page.Ref{ID: elsewhere.ID}, 0, &out))
		lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
		require.Len(t, lines, 5)
		assert.Contains(t, string(lines[1]), "└── Guide (")
		assert.Contains(t, string(lines[2]), "    ├── First (")
		assert.Contains(t, string(lines[3]), "    │   └── Deep (")
		assert.Contains(t, string(lines[4]), "    └── Second (")
	})

	t.Run("not into itself", func(t *testing.T) {
		err := page.Copy(api, page.Ref{ID: source.ID},
			page.CopyOptions{Parent: page.Ref{ID: first.ID}, Prefix: "X ", Recursive: true}, &bytes.Buffer{})
		require.ErrorContains(t, err, "into itself")
	})
}

func TestCopyRefusesAFolderInTheTree(t *testing.T) {
	api, server := newAPI(t)
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	source := server.AddPage("DOCS", "Guide", "page", root.ID)
	server.AddFolder("DOCS", "Folder", source.ID, "page")

	err := page.Copy(api, page.Ref{ID: source.ID},
		page.CopyOptions{Parent: page.Ref{ID: root.ID}, Prefix: "Copy of ", Recursive: true}, &bytes.Buffer{})
	require.ErrorContains(t, err, "copies pages only")
	assert.Len(t, server.ChildOrder(root.ID), 1)
}

func TestCopyOnDataCenter(t *testing.T) {
	api, server := newAPI(t)
	server.SetDataCenter()
	server.AddSpace("DOCS")
	root := server.AddPage("DOCS", "Root", "page", "")
	source := server.AddPage("DOCS", "Guide", "page", root.ID)

	err := page.Copy(api, page.Ref{ID: source.ID},
		page.CopyOptions{Parent: page.Ref{ID: root.ID}, Title: "Guide 2"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "no page copy endpoint")
}
