// Package header renders the --page-header template placed at the top of
// every published page.
package header

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/manifest"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
)

// Header is a loaded page header template. A nil *Header renders nothing.
type Header struct {
	path     string
	markdown bool
	tmpl     *template.Template
	std      *stdlib.Lib

	// compiled holds a Markdown header's compiled result by the text its
	// template executed to and the configuration it was compiled with. A
	// header that names nothing about the document -- or names only what many
	// documents share -- compiles once rather than once per page, which for a
	// diagram means once rather than once per page in Chrome.
	compiled map[string]compiledHeader
	// compiles counts calls to CompileMarkdown, for the tests.
	compiles int
}

type compiledHeader struct {
	html        string
	attachments []attachment.Attachment
}

// Data is what the header template is executed with.
type Data struct {
	// Path is the document's path relative to the working directory, with
	// forward slashes.
	Path string
	// EscapedPath is Path with each segment escaped for use in a URL.
	EscapedPath string
	Title       string
	Space       string
}

// Load reads the header template at path, returning nil when path is empty.
// A file ending in .md is Markdown; anything else is storage format.
//
// The template is executed here, so that a broken template or malformed
// markup fails the run before any page is published. cfg is the run's
// compile configuration, which a Markdown header is compiled with.
func Load(path string, std *stdlib.Lib, cfg types.MarkConfig) (*Header, error) {
	if path == "" {
		return nil, nil
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read page header %q: %w", path, err)
	}

	// Cloned so that the header does not join the set documents render with.
	base, err := std.Templates.Clone()
	if err != nil {
		return nil, fmt.Errorf("unable to prepare page header %q: %w", path, err)
	}

	compileSet, err := std.Templates.Clone()
	if err != nil {
		return nil, fmt.Errorf("unable to prepare page header %q: %w", path, err)
	}

	tmpl, err := base.New("page-header").Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("unable to parse page header %q: %w", path, err)
	}

	header := &Header{
		path:     path,
		markdown: strings.EqualFold(filepath.Ext(path), ".md"),
		tmpl:     tmpl,
		// Includes and macros register templates on the set they compile
		// with; a set of its own keeps them from reaching the documents.
		std:      &stdlib.Lib{Templates: compileSet},
		compiled: map[string]compiledHeader{},
	}

	// Several samples, not one: a template that indexes into .Path or
	// branches on .Title fails only for some documents, and that has to be
	// found before the first page is published rather than part-way through.
	samples := []Data{
		{Path: "example.md", EscapedPath: "example.md"},
		{
			Path: "docs/my notes.md", EscapedPath: "docs/my%20notes.md",
			Title: "Example & <title>", Space: "SPACE",
		},
		{Path: xmlSpecialPath, EscapedPath: escapePath(xmlSpecialPath)},
	}

	for _, sample := range samples {
		rendered, err := header.execute(sample)
		if err != nil {
			return nil, err
		}

		if header.markdown {
			rendered, _, err = header.compile(rendered, cfg)
			if err != nil {
				return nil, err
			}
		}

		if err := markmd.CheckWellFormed(rendered); err != nil {
			return nil, fmt.Errorf("page header %q: %w", path, err)
		}
	}

	return header, nil
}

// Render returns the header for one document as storage format, and any
// attachments a Markdown header references. cfg is the document's compile
// configuration; links in the header are left as written.
func (h *Header) Render(file, title, space string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	if h == nil {
		return "", nil, nil
	}

	path := relativePath(file)

	rendered, err := h.execute(Data{
		Path:        path,
		EscapedPath: escapePath(path),
		Title:       title,
		Space:       space,
	})
	if err != nil {
		return "", nil, err
	}

	if !h.markdown {
		return rendered, nil, nil
	}

	return h.compile(rendered, cfg)
}

// compile compiles the executed Markdown header, or returns what an earlier
// call compiled from the same text and configuration. The attachments are a
// fresh slice each time, so that a caller appending to or editing them does
// not change what the next page is given.
func (h *Header) compile(rendered string, cfg types.MarkConfig) (string, []attachment.Attachment, error) {
	cfg = compileConfig(cfg)

	// The configuration is part of the key because Render is handed each
	// document's. compileConfig has cleared its functions, so this prints only
	// values.
	key := fmt.Sprintf("%#v\x00%s", cfg, rendered)

	cached, ok := h.compiled[key]
	if !ok {
		h.compiles++

		html, attachments, err := markmd.CompileMarkdown([]byte(rendered), h.std, h.path, cfg)
		if err != nil {
			return "", nil, fmt.Errorf("unable to compile page header %q: %w", h.path, err)
		}

		cached = compiledHeader{html: html, attachments: attachments}
		h.compiled[key] = cached
	}

	return cached.html, slices.Clone(cached.attachments), nil
}

// compileConfig adapts a document's configuration to the header, which is not
// the document: its first H1 is not the one to drop, and its relative links do
// not name the document's neighbours.
func compileConfig(cfg types.MarkConfig) types.MarkConfig {
	cfg.DropFirstH1 = false
	cfg.ResolveLink = nil
	cfg.ResolveAttachment = nil

	return cfg
}

func (h *Header) execute(data Data) (string, error) {
	var buffer bytes.Buffer
	if err := h.tmpl.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("unable to execute page header %q: %w", h.path, err)
	}

	return buffer.String(), nil
}

// relativePath names file relative to the working directory. A file outside
// it is named by its base name alone: the page is public to whoever reads the
// space, and the machine's directory layout is not for them.
func relativePath(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return filepath.Base(file)
	}

	// Key leaves a path outside the working directory absolute.
	rel := manifest.Key(abs)
	if filepath.IsAbs(rel) {
		return filepath.Base(file)
	}

	return rel
}

// xmlSpecialPath is a sample name a template must escape to stay well-formed.
const xmlSpecialPath = "docs/a & <b>.md"

func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}

	return strings.Join(segments, "/")
}
