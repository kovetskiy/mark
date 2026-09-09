package renderer

import (
	"errors"
	"fmt"
	stdhtml "html"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/vfs"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceLinkRenderer struct {
	html.Config
	Stdlib *stdlib.Lib

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

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceLinkRenderer(
	lib *stdlib.Lib,
	attachments attachment.Attacher,
	path string,
	attachReferenced bool,
	opts ...html.Option,
) renderer.NodeRenderer {
	return &ConfluenceLinkRenderer{
		Config:           html.NewConfig(),
		Stdlib:           lib,
		Attachments:      attachments,
		Path:             path,
		AttachReferenced: attachReferenced,
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceLinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
}

// splitPageAnchor separates a page title from the anchor written after it.
//
// A "#" alone is not enough to split on: "ac:C# Guide" is a page whose title
// contains one, and reading it as a page called "C" with an anchor called
// " Guide" would break a link that works today. So the part after the last "#"
// has to look like an anchor -- present, and with no whitespace in it, which is
// true of every id mark generates, since a heading's spaces become hyphens.
//
// The last "#" rather than the first, so a title containing one can still be
// given an anchor.
func splitPageAnchor(destination string) (title, anchor string) {
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
func (r *ConfluenceLinkRenderer) renderLink(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)

	// A link to an anchor on this page. The HTML idiom -- href="#X" against an
	// id="X" on the heading -- renders and does nothing when clicked, because
	// Confluence keeps no id on a heading and generates its own from the
	// element's text. ac:link with ac:anchor and no ri:page is the storage
	// format's own way of saying "somewhere on this page", and is what the
	// footnote renderers have always emitted.
	if anchor, found := strings.CutPrefix(string(n.Destination), "#"); found && anchor != "" && r.Stdlib != nil {
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

	if len(n.Destination) >= 3 && string(n.Destination[0:3]) == "ac:" {
		if entering {
			// A "#" in the destination names an anchor on the page rather than
			// part of its title: "ac:Other Page#Setup" is the section, not a
			// page called "Other Page#Setup". Storage format says so with the
			// same ac:anchor a same-page link uses, alongside the ri:page that
			// says which page.
			title, anchor := splitPageAnchor(string(n.Destination[min(3, len(n.Destination)):]))

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
			if len(string(n.Destination)) < 4 {
				//nolint:staticcheck
				_, err := writer.WriteString(xmlAttrEscape(string(node.Text(source))))
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
			//nolint:staticcheck
			_, err = writer.WriteString(cdataEscape(string(node.Text(source))))
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
	if r.AttachReferenced && r.attachable(string(n.Destination)) {
		if entering {
			if err := r.attachReferencedFile(writer, source, node, n); err != nil {
				return ast.WalkStop, err
			}
		}

		return ast.WalkSkipChildren, nil
	}

	if entering {
		_, _ = writer.WriteString("<a href=\"")
		if r.Unsafe || !html.IsDangerousURL(n.Destination) {
			_, _ = writer.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
		}
		_ = writer.WriteByte('"')
		if n.Title != nil {
			_, _ = writer.WriteString(` title="`)
			r.Writer.Write(writer, n.Title)
			_ = writer.WriteByte('"')
		}
		if n.Attributes() != nil {
			html.RenderAttributes(writer, n, html.LinkAttributeFilter)
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
func (r *ConfluenceLinkRenderer) attachable(destination string) bool {
	if r.Attachments == nil || r.Stdlib == nil || r.Path == "" {
		return false
	}

	if !isLocalFileReference(destination) {
		return false
	}

	info, err := os.Stat(filepath.Join(filepath.Dir(r.Path), destination))

	return err == nil && !info.IsDir()
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
	attached, err := attachment.ResolveLocalAttachment(
		vfs.LocalOS, filepath.Dir(r.Path), string(link.Destination),
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

	//nolint:staticcheck
	text := string(node.Text(source))

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
	if destination == "" {
		return false
	}

	if strings.HasPrefix(destination, "#") || isRooted(destination) {
		return false
	}

	if strings.Contains(destination, "://") || strings.HasPrefix(destination, "mailto:") {
		return false
	}

	switch strings.ToLower(filepath.Ext(destination)) {
	case ".md", ".markdown", "":
		return false
	}

	return true
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
// attribute value.
func xmlAttrEscape(s string) string {
	return stdhtml.EscapeString(s)
}

// cdataEscape splits any "]]>" in s across two CDATA sections, which is the only
// way to represent that sequence inside one. Mirrors the stdlib "cdata" template
// function used by the ac:code and ac:plantuml macros.
func cdataEscape(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]><![CDATA[]]]]><![CDATA[>")
}
