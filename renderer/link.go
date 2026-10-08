package renderer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	ctransformer "github.com/kovetskiy/mark/v16/transformer"
	"github.com/kovetskiy/mark/v16/vfs"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/renderer"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

type ConfluenceLinkRenderer struct {
	html.Config

	// options are the html options the constructor was given, which apply
	// on top of the ones the renderer is registered with.
	options []html.Option
	Stdlib  *stdlib.Lib

	// Attachments collects a file a link points at, when this run attaches
	// what its documents refer to.
	Attachments attachment.Attacher

	// Path is the document the link was written in, which is what a relative
	// destination is relative to.
	Path string

	// AttachReferenced turns the whole of that on. Off, a link to a file is
	// published as the path the document wrote.
	AttachReferenced bool
}

// NewConfluenceLinkRenderer creates a new instance of the ConfluenceLinkRenderer.
func NewConfluenceLinkRenderer(
	lib *stdlib.Lib,
	attachments attachment.Attacher,
	path string,
	attachReferenced bool,
	opts ...html.Option,
) html.Extension {
	r := &ConfluenceLinkRenderer{
		Stdlib:           lib,
		Attachments:      attachments,
		Path:             path,
		AttachReferenced: attachReferenced,
	}
	r.options = opts
	r.Config = withOptions(html.Config{}.Default(), opts)
	return r
}

// RendererOptions implements html.Extension.
func (r *ConfluenceLinkRenderer) RendererOptions(cfg *html.Config) []html.Option {
	r.Config = withOptions(*cfg, r.options)

	return []html.Option{html.WithNodeRenderers(map[ast.NodeKind]html.NodeRenderer{
		ast.KindLink: contextNodeRenderer(r.renderLink),
	})}
}

// SplitPageAnchor separates a page title from the anchor written after it.
//
// Exported for the link checker in page/, which has to look up the same title
// the renderer links to.
//
// A "#" alone is not enough to split on: "ac:C# Guide" is a page whose title
// contains one, and reading it as a page called "C" with an anchor called
// " Guide" would break a link that works today. So the part after the last "#"
// has to look like an anchor -- present, and with no whitespace in it, which is
// true of every id mark generates, since a heading's spaces become hyphens.
//
// The last "#" rather than the first, so a title containing one can still be
// given an anchor.
func SplitPageAnchor(destination string) (title, anchor string) {
	at := strings.LastIndex(destination, "#")
	if at <= 0 || at == len(destination)-1 {
		return destination, ""
	}

	candidate := destination[at+1:]
	if strings.ContainsFunc(candidate, unicode.IsSpace) {
		return destination, ""
	}

	return destination[:at], candidate
}

// renderLink renders links specifically for confluence
func (r *ConfluenceLinkRenderer) renderLink(writer util.BufWriter, source []byte, node ast.Node, entering bool, rc renderer.Context) (ast.WalkStatus, error) {
	n := node.(*ast.Link)

	// The destination as written. Each branch below reads it its own way.
	destination := n.Destination.Str(source)

	// A link to an anchor on this page. The HTML idiom -- href="#X" against an
	// id="X" on the heading -- renders and does nothing when clicked, because
	// Confluence keeps no id on a heading and generates its own from the
	// element's text. ac:link with ac:anchor and no ri:page is the storage
	// format's own way of saying "somewhere on this page", and is what the
	// footnote renderers have always emitted.
	if anchor, found := strings.CutPrefix(destination, "#"); found && anchor != "" && r.Stdlib != nil {
		if entering {
			err := r.Stdlib.Templates.ExecuteTemplate(writer, "ac:link:anchor", struct {
				Anchor string
			}{anchor})
			if err != nil {
				return ast.WalkStop, err
			}

			return ast.WalkContinue, nil
		}

		if _, err := writer.WriteString("</ac:link-body></ac:link>"); err != nil {
			return ast.WalkStop, err
		}

		return ast.WalkContinue, nil
	}

	if strings.HasPrefix(destination, "ac:") {
		if entering {
			// A "#" in the destination names an anchor on the page rather than
			// part of its title: "ac:Other Page#Setup" is the section, not a
			// page called "Other Page#Setup". Storage format says so with the
			// same ac:anchor a same-page link uses, alongside the ri:page that
			// says which page.
			title, anchor := SplitPageAnchor(destination[3:])

			opening := "<ac:link>"
			if anchor != "" {
				opening = `<ac:link ac:anchor="` + xmlAttrEscape(anchor) + `">`
			}

			_, err := writer.Write([]byte(opening + "<ri:page ri:content-title=\""))
			if err != nil {
				return ast.WalkStop, err
			}

			// The page title lands in an XML attribute, so it has to be escaped:
			// an unescaped "&" makes the body malformed and a quote closes the
			// attribute early, letting document content inject further attributes.
			if len(destination) < 4 {
				_, err := writer.WriteString(xmlAttrEscape(ctransformer.NodeText(node, source)))
				if err != nil {
					return ast.WalkStop, err
				}
			} else {
				_, err := writer.WriteString(xmlAttrEscape(title))
				if err != nil {
					return ast.WalkStop, err
				}

			}
			_, err = writer.Write([]byte("\"/><ac:plain-text-link-body><![CDATA["))
			if err != nil {
				return ast.WalkStop, err
			}

			// A "]]>" in the link text would terminate the CDATA section early;
			// splitting it across two sections is the only way to escape it.
			_, err = writer.WriteString(cdataEscape(ctransformer.NodeText(node, source)))
			if err != nil {
				return ast.WalkStop, err
			}

			_, err = writer.Write([]byte("]]></ac:plain-text-link-body></ac:link>"))
			if err != nil {
				return ast.WalkStop, err
			}
		}
		return ast.WalkSkipChildren, nil
	}
	// A file beside the document, which is published with it rather than left
	// as a path that means nothing once the page is on Confluence.
	//
	// Decided the same way on the way in and on the way out, as the ac: branch
	// above is: the renderer is called twice for one link, and a decision made
	// only on the way in leaves the closing </a> of a tag that was never opened.
	if r.AttachReferenced && r.attachable(n, source) {
		if entering {
			if err := r.attachReferencedFile(writer, source, node, n); err != nil {
				return ast.WalkStop, err
			}
		}

		return ast.WalkSkipChildren, nil
	}

	if entering {
		_, _ = writer.WriteString("<a href=\"")
		if r.Unsafe || !html.IsDangerousURL(destination) {
			// CommonMark escapes and references resolved, then made a URL,
			// which goldmark v2's URLEscape also makes safe in an attribute.
			_, _ = writer.Write(util.URLEscape(ctransformer.DecodeMarkdown(n.Destination.Bytes(source))))
		}
		_ = writer.WriteByte('"')
		if !n.Title.IsEmpty() {
			_, _ = writer.WriteString(` title="`)
			_, _ = ctransformer.DecodeMarkdownTo(html.ContextTextWriter(rc), n.Title.Bytes(source))
			_ = writer.WriteByte('"')
		}
		if n.Attributes() != nil {
			html.RenderAttributes(writer, source, n, html.LinkAttributeFilter, nil)
		}
		_ = writer.WriteByte('>')
	} else {
		_, _ = writer.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}

// attachable reports whether a destination names a file this run should publish
// alongside the document.
//
// Cheap on purpose, and asked on both halves of the render: it decides which
// element is written, so it has to give the same answer each time. Existence is
// part of the question -- a path to nothing is left as the document wrote it,
// which is what happens without the flag at all -- but the file is not read
// here, and whether it may be read is decided where it is.
func (r *ConfluenceLinkRenderer) attachable(link *ast.Link, source []byte) bool {
	_, ok := r.localFile(link, source)

	return ok
}

// localFile reports the file beside the document that a destination names.
//
// Tried as written first and then the way an image destination is read --
// CommonMark escapes resolved, then as the URL a destination is -- so that
// "a%23b.png" finds a#b.png and "my\_file.pdf" finds my_file.pdf, while a file
// really called my%20file.png still finds itself.
func (r *ConfluenceLinkRenderer) localFile(link *ast.Link, source []byte) (string, bool) {
	destination := link.Destination.Str(source)

	if r.Attachments == nil || r.Stdlib == nil || r.Path == "" {
		return "", false
	}

	if !isLocalFileReference(destination) {
		return "", false
	}

	names := []string{destination}
	for _, name := range ctransformer.LocalImagePaths(ctransformer.LinkDestination(link, source)) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	for _, name := range names {
		info, err := os.Stat(filepath.Join(filepath.Dir(r.Path), name))
		if err == nil && !info.IsDir() {
			return name, true
		}
	}

	return "", false
}

// attachReferencedFile uploads what a link points at, and writes a link to the
// attachment in place of the path.
func (r *ConfluenceLinkRenderer) attachReferencedFile(
	writer util.BufWriter,
	source []byte,
	node ast.Node,
	link *ast.Link,
) error {
	// Resolved as written rather than as a pattern: a link names one file, and
	// the one it names is the one the reader was promised.
	name, ok := r.localFile(link, source)
	if !ok {
		name = link.Destination.Str(source)
	}

	attached, err := attachment.ResolveLocalAttachment(
		vfs.LocalOS, filepath.Dir(r.Path), name,
	)

	// Refused rather than published as a link to somewhere it should not have
	// reached, and said so where the image renderer says the same thing.
	if errors.Is(err, attachment.ErrOutsideProject) {
		line, col := GetLineCol(source, node.Pos())

		return fmt.Errorf("line %d, col %d: %w", line, col, err)
	}

	if err != nil {
		return err
	}

	r.Attachments.Attach(attached)

	text := ctransformer.NodeText(node, source)

	return r.Stdlib.Templates.ExecuteTemplate(writer, "ac:link:attachment", struct {
		Name string
		Text string
	}{attached.Filename, text})
}

// isLocalFileReference reports whether a destination names a file next to the
// document rather than something else entirely.
//
// A document is one of those things: a link to another .md is how one page
// refers to another, and it is resolved into a page link long before this. One
// that resolved into nothing is still not an attachment -- publishing a
// colleague's source as a download is not what was meant by linking to it.
func isLocalFileReference(destination string) bool {
	if !NamesBesideDocument(destination) {
		return false
	}

	switch strings.ToLower(filepath.Ext(destination)) {
	case ".md", ".markdown", "":
		return false
	}

	return true
}

// NamesBesideDocument reports whether a destination can name a file next to the
// document: not empty, not an anchor, not rooted, and not a URI.
//
// A protocol-relative "//host/path" is caught as rooted.
func NamesBesideDocument(destination string) bool {
	if destination == "" || strings.HasPrefix(destination, "#") || isRooted(destination) {
		return false
	}

	return !isURI(destination)
}

// opaqueSchemes are the schemes a link is written with and without the "//"
// after the colon: "mailto:", "data:", and "https:example.com" alike.
var opaqueSchemes = []string{
	"mailto", "tel", "sms", "data", "javascript", "urn", "news", "magnet",
	"xmpp", "about", "blob", "http", "https", "ftp", "file",
}

// isURI reports whether a destination is a URI rather than a file.
//
// Any "://" is one. Without it, only the schemes in opaqueSchemes count: a
// Linux or macOS filename may hold a colon, and "notes:v2.pdf" or
// "shot:1.png" is shaped like a scheme without being one, so taking every
// scheme-shaped prefix for a URI would leave such a file unattached.
func isURI(destination string) bool {
	if strings.Contains(destination, "://") {
		return true
	}

	scheme, _, found := strings.Cut(destination, ":")

	return found && slices.Contains(opaqueSchemes, strings.ToLower(scheme))
}

// isRooted reports whether a destination names a place from the root of a
// filesystem rather than one beside the document.
//
// Answered by shape rather than by filepath.IsAbs, which answers for the OS
// this happens to be running on. A document naming C:\docs\report.pdf names an
// absolute path whether it is published from Windows or from CI on Linux, and
// either way it is not a file beside the document. Left alone it keeps the link
// the author wrote, rather than being joined onto the document's directory and
// refused as "outside the project" -- an error about a path that was never
// going to be attached.
func isRooted(destination string) bool {
	// A leading backslash covers both the Windows root and the "\\server\share"
	// of a UNC path.
	if strings.HasPrefix(destination, "/") || strings.HasPrefix(destination, `\`) {
		return true
	}

	// A drive letter -- "C:/docs/report.pdf", "c:\docs\report.pdf".
	if len(destination) >= 2 && destination[1] == ':' {
		letter := destination[0]

		return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
	}

	return false
}

// xmlAttrEscape makes a document-derived string safe to interpolate into an XML
// attribute value. It is the stdlib "xmlesc" template function.
func xmlAttrEscape(s string) string {
	return stdlib.XMLEscape(s)
}

// cdataEscape splits any "]]>" in s across two CDATA sections, which is the only
// way to represent that sequence inside one. It is the stdlib "cdata" template
// function used by the ac:code and ac:plantuml macros.
func cdataEscape(s string) string {
	return stdlib.CDATA(s)
}
