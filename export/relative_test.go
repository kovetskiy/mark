package export

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pixel is a 1x1 PNG.
var pixel = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa75\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

// republishBesideFiles exports storage and publishes the Markdown that comes
// out as docs/guide/page.md, with docs/guide/chart.png and docs/notes.txt on
// disk and --attach-referenced on. It returns the Markdown, the storage
// format in canonical form, and the names of the files publishing attached.
func republishBesideFiles(t *testing.T, storage string) (string, string, []string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "docs", "guide")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chart.png"), pixel, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "..", "notes.txt"), []byte("notes"), 0o644))

	md := convert(t, storage)

	lib, err := stdlib.New(context.Background(), nil)
	require.NoError(t, err)

	out, attached, err := markdown.CompileMarkdown(t.Context(), []byte(md), lib, filepath.Join(dir, "page.md"), markdown.Options{
		Features:         []string{"mention"},
		AttachReferenced: true,
	})
	require.NoError(t, err, "exported Markdown:\n%s", md)

	var names []string
	for _, a := range attached {
		names = append(names, a.Name)
	}

	return md, canonical(t, out), names
}

// TestConvertRelativeAddressIsNotAFile: an address on the page that names no
// host is relative to the page in Confluence. Published back, it is still
// that address, whatever file of the same name is beside the Markdown.
func TestConvertRelativeAddressIsNotAFile(t *testing.T) {
	for _, test := range []struct {
		name, storage, published string
	}{
		{name: "link", storage: `<p>see <a href="../notes.txt">notes</a> here</p>`},
		{name: "link beside the page", storage: `<p>see <a href="chart.png">the chart</a> here</p>`},
		{name: "link with a title", storage: `<p>see <a href="chart.png" title="Chart">the <em>chart</em></a> here</p>`},
		{name: "link alone", storage: `<p><a href="../notes.txt">notes</a></p>`},
		{
			name:      "link in a table",
			storage:   `<table><tbody><tr><th>a</th></tr><tr><td><a href="chart.png">chart</a></td></tr></tbody></table>`,
			published: `<table><thead><tr><th>a</th></tr></thead><tbody><tr><td><a href="chart.png">chart</a></td></tr></tbody></table>`,
		},
		{name: "image", storage: `<p>see <ac:image><ri:url ri:value="chart.png"/></ac:image> here</p>`},
		{name: "image alone", storage: `<p><ac:image ac:alt="Chart"><ri:url ri:value="chart.png"/></ac:image></p>`},
		{name: "image from the site root", storage: `<p>see <ac:image><ri:url ri:value="/chart.png"/></ac:image> here</p>`},
		{name: "sized image", storage: `<p>see <ac:image ac:width="20"><ri:url ri:value="chart.png"/></ac:image> here</p>`},
		{
			name:      "img element",
			storage:   `<p><img src="chart.png" width="20" height="10px" alt="Chart" title="A chart" class="wide"/></p>`,
			published: `<p><ac:image ac:alt="Chart" ac:title="A chart" ac:custom-width="true" ac:width="20" ac:height="10"><ri:url ri:value="chart.png"/></ac:image></p>`,
		},
		{
			name:      "img element in a div",
			storage:   `<div><p>see <img src="chart.png"/> here</p></div>`,
			published: `<div><p>see <ac:image><ri:url ri:value="chart.png"/></ac:image> here</p></div>`,
		},
		{
			// Publishing leaves a <picture> alone only as an HTML block.
			name:      "img element in a picture in a list",
			storage:   `<ul><li>see <picture><img src="chart.png"/></picture> here</li></ul>`,
			published: `<ul><li>see <picture><ac:image><ri:url ri:value="chart.png"/></ac:image></picture> here</li></ul>`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			md, published, attached := republishBesideFiles(t, test.storage)

			want := test.published
			if want == "" {
				want = test.storage
			}

			assert.Empty(t, attached, "exported Markdown:\n%s", md)
			assert.Equal(t, canonical(t, want), published, "exported Markdown:\n%s", md)
		})
	}
}

// TestConvertAddressElsewhereStaysMarkdown: an address publishing reads as
// written is a Markdown link or image as before.
func TestConvertAddressElsewhereStaysMarkdown(t *testing.T) {
	for storage, want := range map[string]string{
		`<p>see <a href="https://example.com/notes.txt">notes</a> here</p>`:                        "see [notes](https://example.com/notes.txt) here\n",
		`<p>see <a href="https://example.com/">https://example.com/</a> here</p>`:                  "see <https://example.com/> here\n",
		`<p>see <a href="mailto:team@example.com">the team</a> here</p>`:                           "see [the team](mailto:team@example.com) here\n",
		`<p>see <a href="/wiki/notes.txt">notes</a> here</p>`:                                      "see [notes](/wiki/notes.txt) here\n",
		`<p>see <ac:image><ri:url ri:value="https://example.com/chart.png"/></ac:image> here</p>`:  "see ![](https://example.com/chart.png) here\n",
		`<p><ac:image ac:width="20"><ri:url ri:value="https://example.com/c.png"/></ac:image></p>`: "<img src=\"https://example.com/c.png\" width=\"20\" />\n",
		`<p><img src="https://example.com/chart.png"/></p>`:                                        "<p><img src=\"https://example.com/chart.png\"/></p>\n",
		`<p><picture><img src="chart.png"/></picture></p>`:                                         "<p><picture><img src=\"chart.png\"/></picture></p>\n",
	} {
		assert.Equal(t, want, convert(t, storage), storage)
	}
}

// TestConvertRelativeAddressOfAnExportedPage: a link the export maps to the
// file of another exported page is that file's path.
func TestConvertRelativeAddressOfAnExportedPage(t *testing.T) {
	doc, err := Convert(`<p>see <a href="other">the other page</a> here</p>`, Options{
		PageURLPath: func(href string) (string, string, bool) { return "other.md", "Other", href == "other" },
	})
	require.NoError(t, err)

	assert.Equal(t, "see [the other page](other.md) here\n", doc.Markdown)
}

// TestConvertRelativeAddressToRead: Markdown that is read is not published
// back, and keeps its links and images as Markdown.
func TestConvertRelativeAddressToRead(t *testing.T) {
	doc, err := Convert(`<p>see <a href="../notes.txt">notes</a> and <ac:image><ri:url ri:value="chart.png"/></ac:image></p>`,
		Options{Reading: true})
	require.NoError(t, err)

	assert.Equal(t, "see [notes](../notes.txt) and ![](chart.png)\n", doc.Markdown)
}

// TestConvertRelativeImgInTheSidebar: the sidebar travels in a header, which
// publishing writes to the page as it is, looking for no file.
func TestConvertRelativeImgInTheSidebar(t *testing.T) {
	doc, err := Convert(`<ac:layout><ac:layout-section ac:type="two_right_sidebar">`+
		`<ac:layout-cell><p>body</p></ac:layout-cell>`+
		`<ac:layout-cell><p><img src="chart.png" style="float: left"/></p></ac:layout-cell>`+
		`</ac:layout-section></ac:layout>`, Options{})
	require.NoError(t, err)

	assert.Equal(t, `<p><img src="chart.png" style="float: left"/></p>`, doc.Sidebar)
}
