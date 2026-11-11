package d2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"io/fs"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/rs/zerolog/log"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/imgbundler"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

// ErrUnsafeDiagram is returned for a diagram that would do something when it is
// rendered or opened, rather than depict something.
var ErrUnsafeDiagram = errors.New("diagram is not safe to publish")

var renderTimeout = 120 * time.Second

// diagramPad is the margin d2 draws around a diagram. It is named because the
// size of the picture depends on it: the drawing is the bounding box of what
// was laid out, with this much added on every side.
const diagramPad = 5

// renderSVG compiles a diagram and draws it, which is where both outputs start:
// the PNG is a screenshot of this, and the SVG is this.
//
// The size comes back with it, because d2 knows it: the drawing is the bounding
// box of what it laid out plus the pad on every side. Reading it back out of
// the rendered XML would be parsing an answer that was already given -- and
// mermaid has to do exactly that, since its diagrams are drawn by a browser
// rather than by a library that will say.
//
// imports is what the diagram's imports are read through.
func renderSVG(ctx context.Context, d2Diagram []byte, imports *importFS) (out []byte, width, height int, err error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, 0, 0, err
	}
	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:     go2.Pointer(int64(diagramPad)),
		ThemeID: &d2themescatalog.GrapeSoda.ID,
	}
	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
		FS:             imports,
	}

	diagram, _, err := d2lib.Compile(ctx, string(d2Diagram), compileOpts, renderOpts)
	if err != nil {
		if imports.refused != nil {
			return nil, 0, 0, importError{err: err, refused: imports.refused}
		}

		return nil, 0, 0, err
	}

	out, err = d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, 0, 0, err
	}

	// Before anything is done with the drawing, because both things done with
	// it are dangerous. The PNG is taken by navigating a browser to this as a
	// document, so anything the drawing runs runs here, on the machine
	// publishing -- in a browser started with --no-sandbox. The SVG is uploaded
	// whole, so the same thing is served to whoever opens the page.
	if err := checkDrawingIsSafe(out); err != nil {
		return nil, 0, 0, err
	}

	topLeft, bottomRight := diagram.BoundingBox()

	return out,
		bottomRight.X - topLeft.X + 2*diagramPad,
		bottomRight.Y - topLeft.Y + 2*diagramPad,
		nil
}

// Options say how Render draws a diagram.
type Options struct {
	// Output is imageformat.PNG, the default when empty, or imageformat.SVG.
	Output imageformat.Format

	// Scale multiplies a PNG's pixels, or the size the page displays an SVG
	// at.
	Scale float64

	// Browser rasterises a PNG. An SVG needs none, and it may then be nil.
	Browser *chrome.Browser

	// InputPath is the document the diagram is in, which the diagram's imports
	// and the images an SVG references are relative to; empty or "-" for none.
	InputPath string

	// Root is where InputPath is and what the diagram names is read from. The
	// zero Root is the disk. With an fs.FS, InputPath is a path inside it,
	// imports and local images are read through Root and held to its
	// boundary, and the local images a PNG references are inlined too, since
	// the browser drawing it has no file system to read them from.
	Root attachment.Root

	// BundleRemote lets a diagram inline the remote images it references, which
	// are otherwise refused: an SVG carries them, and a PNG is drawn by a
	// browser that loads nothing from the network.
	BundleRemote bool
}

// Render draws a diagram as an attachment named after title, or after its
// checksum when title is empty.
func Render(ctx context.Context, title string, d2Diagram []byte, options Options) (attachment.Attachment, error) {
	switch options.Output {
	case imageformat.PNG, "":
		return renderPNGAttachment(ctx, options.Browser, title, d2Diagram, options.document(), options.Scale, options.BundleRemote)
	case imageformat.SVG:
		return renderSVGAttachment(ctx, title, d2Diagram, options.document(), options.Scale, options.BundleRemote)
	default:
		return attachment.Attachment{}, fmt.Errorf("unknown d2 output %q: expected png or svg", options.Output)
	}
}

// renderPNGAttachment draws a diagram as a PNG, rasterised in browser.
func renderPNGAttachment(ctx context.Context, browser *chrome.Browser, title string, d2Diagram []byte, doc document, scale float64, bundleRemote bool) (attachment.Attachment, error) {
	// Before the layout, which can take seconds, rather than after it.
	if browser == nil {
		return attachment.Attachment{}, chrome.ErrNoBrowser
	}

	// The layout's own budget: the browser times its capture itself, so the
	// capture is given the caller's context rather than what is left of this.
	layoutCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	layoutCtx = d2log.WithDefault(layoutCtx)
	defer cancel()

	imports := &importFS{doc: doc}

	out, _, _, err := renderSVG(layoutCtx, d2Diagram, imports)
	if err != nil {
		return attachment.Attachment{}, err
	}

	// The browser loads nothing from the network, so a remote image is
	// fetched here, by the same rule and the same bundler as for an SVG, or the
	// diagram is refused rather than drawn without it. Nor does it see the
	// document's fs.FS, so a local image there is inlined first; on disk one
	// is left as it always has been.
	var remote bool
	var images []byte
	if doc.inFS() {
		var local bool
		local, remote, err = references(out, doc, bundleRemote)
		if err == nil && local {
			out, images, err = bundleLocalIn(doc, out)
		}
	} else {
		remote, err = remoteReferences(out, bundleRemote)
	}
	if err != nil {
		return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
	}

	if remote {
		out, err = fetchRemoteImages(layoutCtx, out)
		if err != nil {
			return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
		}
	}

	log.Debug().Msgf("Rendering: %q", title)
	// d2 nests the diagram in an outer svg, so the inner one is what to
	// screenshot: the outer one carries the padding.
	pngBytes, width, height, err := browser.PNGFromSVG(ctx, out, `document.querySelector("svg > svg")`, scale)
	if err != nil {
		return attachment.Attachment{}, err
	}

	scaleAsBytes := make([]byte, 8)

	binary.LittleEndian.PutUint64(scaleAsBytes, math.Float64bits(scale))

	// What a diagram imports is part of what it draws, so a changed import
	// changes the checksum; a diagram that imports nothing keeps the one it
	// had. A fetched image can change without the source changing, so when
	// there is one the drawing that carries it is part of what identifies the
	// PNG too. So is every local image inlined from the document's fs.FS.
	identity := []io.Reader{bytes.NewReader(d2Diagram), bytes.NewReader(scaleAsBytes), bytes.NewReader(imports.read), bytes.NewReader(images)}
	if remote {
		identity = append(identity, bytes.NewReader(out))
	}

	checkSum, err := attachment.GetChecksum(io.MultiReader(identity...))

	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}
	if title == "" {
		title = checkSum
	}

	fileName := attachment.FlattenFilename(title) + ".png"

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  fileName,
		FileBytes: pngBytes,
		Checksum:  checkSum,
		Replace:   title,
		Width:     strconv.FormatInt(width, 10),
		Height:    strconv.FormatInt(height, 10),
	}, nil
}

// renderSVGAttachment publishes a diagram as the SVG it was drawn as, rather
// than a picture of it: one file that is sharp at any zoom and whose text
// stays text.
//
// scale multiplies the size the page displays it at. The file itself is the
// same drawing whatever that is, so unlike the PNG's the scale is not part of
// what identifies the attachment.
func renderSVGAttachment(ctx context.Context, title string, d2Diagram []byte, doc document, scale float64, bundleRemote bool) (attachment.Attachment, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	ctx = d2log.WithDefault(ctx)
	defer cancel()

	out, width, height, err := renderSVG(ctx, d2Diagram, &importFS{doc: doc})
	if err != nil {
		return attachment.Attachment{}, err
	}

	// A PNG carries what a diagram references by being a picture of it. An SVG
	// carries the reference itself, and Confluence serves the attachment from
	// its own host, where a path relative to the document resolves to nothing
	// and a remote image may be blocked. Both are inlined here, which is what
	// d2's own --bundle does.
	log.Debug().Msgf("Bundling what the diagram references: %q", title)

	// Every reference is judged before any of them is fetched, because the
	// bundler decides for itself what a name means: it reads an absolute path
	// as written, joins a relative one to wherever it was told the document is,
	// and fetches any URL with a client that follows redirects to anywhere at
	// all. None of that is reachable through its API, so the only place to say
	// no is here, before it is asked.
	local, remote, err := references(out, doc, bundleRemote)
	if err != nil {
		return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
	}

	// Files first: what a server answers is written into the drawing, and
	// only the references judged above may be read.
	switch {
	case local && doc.inFS():
		out, _, err = bundleLocalIn(doc, out)
		if err != nil {
			return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
		}
	case local:
		out, err = imgbundler.BundleLocal(ctx, bundleLogger{}, doc.path, out, false)
		if err != nil {
			return attachment.Attachment{}, err
		}
	}

	if remote {
		out, err = fetchRemoteImages(ctx, out)
		if err != nil {
			return attachment.Attachment{}, fmt.Errorf("diagram %q: %w", title, err)
		}
	}

	// Taken over the drawing rather than over the source it was drawn from,
	// which is what the PNG side and mermaid do. Bundling is why: what the
	// diagram references is now inside the file, so an image that changed at
	// the other end of a URL changes the attachment without changing a line of
	// the source -- and a checksum over the source would call that unchanged
	// and leave the old drawing on the page.
	//
	// The cost is that anything else changing the bytes uploads them again. d2
	// draws the same diagram identically from one run to the next, which the
	// tests pin, so in practice that means a d2 upgrade that moves a line by a
	// pixel: one re-upload, of a drawing that did change.
	checkSum, err := attachment.GetChecksum(bytes.NewReader(out))
	log.Debug().Msgf("Checksum: %q -> %s", title, checkSum)

	if err != nil {
		return attachment.Attachment{}, err
	}

	if title == "" {
		title = checkSum
	}

	return attachment.Attachment{
		ID:        "",
		Name:      title,
		Filename:  attachment.FlattenFilename(title) + ".svg",
		FileBytes: out,
		Checksum:  checkSum,
		Replace:   title,
		Width:     displayed(width, scale),
		Height:    displayed(height, scale),
	}, nil
}

// importFS is what d2 reads a diagram's imports through. Without one d2 opens
// any path it is given, relative to the working directory; this holds each
// import to the boundary every other file a document names is held to, and
// reads a relative one from the document's directory, as d2 itself does for a
// file it is run on and as an image or icon is.
//
// d2 asks for nested imports through the same Open, already joined to the
// directory of the file that imports them.
type importFS struct {
	doc document

	// refused is the first import turned down, kept because d2 passes on
	// only its message.
	refused error

	// read is every file imported, each named and in the order d2 asked for
	// it, for a checksum taken over the source to cover them too.
	read []byte
}

// Open reads the whole file up front, so that what read records is what d2
// was given.
func (f *importFS) Open(name string) (fs.File, error) {
	// d2 names the import in its own message, which is all it passes on.
	if err := f.doc.check(name); err != nil {
		if f.refused == nil {
			f.refused = err
		}

		return nil, err
	}

	file, err := f.doc.root.Open(f.doc.join(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	stat, ok := file.(interface{ Stat() (fs.FileInfo, error) })
	if !ok {
		return nil, fmt.Errorf("%s: cannot be described", name)
	}

	info, err := stat.Stat()
	if err != nil {
		return nil, err
	}

	f.read = fmt.Appendf(f.read, "%s\x00%d\x00", name, len(content))
	f.read = append(f.read, content...)

	return importedFile{Reader: bytes.NewReader(content), info: info}, nil
}

// importedFile is an import already read into memory.
type importedFile struct {
	*bytes.Reader

	info fs.FileInfo
}

func (f importedFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (importedFile) Close() error                 { return nil }

// document is the document a diagram was written in, and where the files it
// names are read from.
type document struct {
	path string
	root attachment.Root
}

func (o Options) document() document {
	return document{path: o.InputPath, root: o.Root}
}

// inFS reports whether the document's files are in an fs.FS.
func (d document) inFS() bool {
	return d.root.FS() != nil
}

// check holds a file a diagram names to the document's boundary: its project
// on disk, or its fs.FS. The error does not repeat the name: the caller says
// what the diagram did with it.
func (d document) check(name string) error {
	if d.path == "" || d.path == "-" {
		return errors.New("needs the document it was written in to be a file on disk")
	}

	return d.root.CheckReadable(filepath.Dir(d.path), name)
}

// join is name read relative to the document's directory. On disk an
// absolute name is read as written; in an fs.FS check has refused one.
func (d document) join(name string) string {
	if filepath.IsAbs(name) {
		return name
	}

	return filepath.Join(filepath.Dir(d.path), name)
}

// bundleLocalIn inlines the local images a drawing references, read from the
// document's fs.FS, as imgbundler.BundleLocal does from disk; references has
// checked each. It also returns every image read, each named, for a checksum
// taken over the source to cover them too.
func bundleLocalIn(doc document, svg []byte) (out, read []byte, err error) {
	done := map[string]bool{}
	for _, match := range image.FindAllSubmatch(svg, -1) {
		href := string(match[1])
		unescaped := stdhtml.UnescapeString(href)
		if done[href] || strings.HasPrefix(href, "data:") || isRemote(unescaped) {
			continue
		}
		done[href] = true

		content, err := doc.root.ReadFile(doc.join(unescaped))
		if err != nil {
			return nil, nil, fmt.Errorf("references %q: %w", unescaped, err)
		}

		read = fmt.Appendf(read, "%s\x00%d\x00", unescaped, len(content))
		read = append(read, content...)

		inlined := fmt.Sprintf(`<image href="data:%s;base64,%s"`,
			imageType(unescaped, content), base64.StdEncoding.EncodeToString(content))
		svg = bytes.ReplaceAll(svg, match[0], []byte(inlined))
	}

	// The type is guessed from the name, as the bundler guesses it, so what
	// came of it has to pass the check the drawing did.
	if err := checkDrawingIsSafe(svg); err != nil {
		return nil, nil, err
	}

	return svg, read, nil
}

// imageType is the media type d2's bundler gives a local image: by its
// extension, or else by its content, an SVG read as XML or as bytes being an
// SVG all the same.
func imageType(name string, content []byte) string {
	mimeType := mime.TypeByExtension(path.Ext(name))
	if mimeType == "" {
		mimeType = http.DetectContentType(content)
	}

	mimeType = strings.Replace(mimeType, "text/xml", "image/svg+xml", 1)
	if mimeType == "application/octet-stream" && bytes.Contains(content, []byte("<svg")) {
		mimeType = "image/svg+xml"
	}

	return mimeType
}

// importError is d2's own report of a failed compile, which says where the
// import is written, carrying the refusal behind it: d2 passes on only the
// refusal's message, and errors.Is needs the error itself.
type importError struct {
	err     error
	refused error
}

func (e importError) Error() string   { return e.err.Error() }
func (e importError) Unwrap() []error { return []error{e.err, e.refused} }

// image matches the pictures d2 draws into an SVG, the same way d2's own
// bundler finds them.
var image = regexp.MustCompile(`<image href="([^"]+)"`)

// references reports what kinds of thing a drawing points at, and refuses the
// ones this run may not read.
//
// A file is held to the boundary an attachment is held to -- the document's own
// directory or the one mark is running in, or the document's fs.FS -- because a
// diagram naming a file is doing what a document does, and "../../../etc/id_rsa"
// reaches no further in one than the other. It needs a document on disk to be relative to: "-" is
// d2's way of writing standard input, and reading a name against the working
// directory instead is how a diagram compiled through the library reaches a
// file nobody offered it.
//
// A URL is refused unless this run asked for remote bundling, because fetching
// one is a request made by the document rather than by the person publishing
// it. The bundler's client follows redirects and declines nothing -- not
// loopback, not link-local, not a cloud metadata address -- and what comes back
// is published inside the drawing.
func references(svg []byte, doc document, bundleRemote bool) (local, remote bool, err error) {
	for _, match := range image.FindAllSubmatch(svg, -1) {
		href := stdhtml.UnescapeString(string(match[1]))

		// Already carried by the drawing rather than pointed at by it.
		if strings.HasPrefix(href, "data:") {
			continue
		}

		if isRemote(href) {
			if err := checkRemote(href, bundleRemote); err != nil {
				return false, false, err
			}

			remote = true

			continue
		}

		if err := doc.check(href); err != nil {
			return false, false, fmt.Errorf("references %q: %w", href, err)
		}

		local = true
	}

	return local, remote, nil
}

// remoteReferences is references for a PNG: it judges only the URLs a drawing
// names. A relative path is left alone, as it always has been for a PNG, which
// the browser draws from a data: URL that a path resolves against to nothing.
func remoteReferences(svg []byte, bundleRemote bool) (remote bool, err error) {
	for _, match := range image.FindAllSubmatch(svg, -1) {
		href := stdhtml.UnescapeString(string(match[1]))
		if !isRemote(href) {
			continue
		}

		if err := checkRemote(href, bundleRemote); err != nil {
			return false, err
		}

		remote = true
	}

	return remote, nil
}

// fetchRemoteImages inlines the remote images a drawing names, and checks the
// drawing again: the bundler writes each server's Content-Type into it as the
// server gave it, so what comes back has to pass the same check the drawing
// did.
func fetchRemoteImages(ctx context.Context, svg []byte) ([]byte, error) {
	out, err := imgbundler.BundleRemote(ctx, bundleLogger{}, svg, false)
	if err != nil {
		return nil, err
	}

	if err := checkDrawingIsSafe(out); err != nil {
		return nil, err
	}

	return out, nil
}

// isRemote reports whether href is an http or https URL.
func isRemote(href string) bool {
	parsed, err := url.Parse(href)

	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

// checkRemote refuses a URL unless this run asked for remote bundling.
func checkRemote(href string, bundleRemote bool) error {
	if bundleRemote {
		return nil
	}

	return fmt.Errorf(
		"references %q, and fetching what a diagram names is off by default: "+
			"pass --d2-bundle-remote for documents whose diagrams you trust",
		href,
	)
}

// displayed reports the size the page should show a length at, as the whole
// number of pixels an attachment is measured in.
//
// Multiplied first and rounded once, since rounding before the scale scales a
// number that has already lost its fraction. Formatted as a float rather than
// converted to an int, because a length past what an int holds does not convert
// -- it becomes whatever the machine makes of it, and clamped to at least one
// pixel that would publish an enormous diagram one pixel wide.
//
// A scale that is not a number to multiply by leaves the length alone: run
// checks it, but Render is exported and reachable without that.
func displayed(length int, scale float64) string {
	size := float64(length)
	if scale > 0 && !math.IsInf(scale, 0) {
		size *= scale
	}

	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return ""
	}

	return strconv.FormatFloat(math.Max(1, math.Round(size)), 'f', -1, 64)
}

// executable are the elements that run or fetch something of their own, rather
// than drawing. d2 draws a |md | label as SVG of its own rather than passing
// the author's markup through, so nothing should reach these -- which is the
// reason to keep them: the check should not depend on a renderer continuing to
// be careful on mark's behalf.
var executable = map[string]bool{
	"script": true,
	"iframe": true,
	"object": true,
	"embed":  true,
}

// checkDrawingIsSafe refuses a rendered diagram that would do something rather
// than depict something.
//
// Both things mark does with a drawing execute it: the PNG is a screenshot
// taken by navigating a browser to the drawing as a document, and the SVG is
// uploaded to Confluence for other people's browsers to open. A diagram in a
// pull request could therefore read a cloud metadata endpoint from the CI
// runner, or wait to be opened by a colleague.
//
// What a diagram has to say for itself is mostly drawn rather than passed
// through -- but not all of it: a d2 link is an address, and it goes into the
// drawing as the anchor it was written as, whether it names a page or names
// code.
//
// Refused rather than stripped. A diagram that asked to run something is not a
// diagram somebody drew by accident, and quietly publishing a different one
// than was written is its own kind of wrong.
//
// Read with the lenient HTML tokenizer rather than an XML parser: this is
// somebody else's markup being read for what it would do, and failing to parse
// it strictly is a way to fail on drawings that were fine.
func checkDrawingIsSafe(svg []byte) error {
	tokenizer := html.NewTokenizer(bytes.NewReader(svg))

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			// Including io.EOF, which is how a document that held nothing
			// objectionable ends.
			return nil

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttributes := tokenizer.TagName()
			if executable[strings.ToLower(string(name))] {
				return fmt.Errorf(
					"%w: it contains <%s>, which would run when the diagram is rendered or opened",
					ErrUnsafeDiagram, name,
				)
			}

			for hasAttributes {
				var key, value []byte

				key, value, hasAttributes = tokenizer.TagAttr()
				if err := checkAttribute(string(key), string(value)); err != nil {
					return err
				}
			}
		}
	}
}

// checkAttribute refuses the two ways an attribute runs something: by being an
// event handler, and by naming a URL that is code rather than a picture.
func checkAttribute(key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))

	if strings.HasPrefix(key, "on") {
		return fmt.Errorf(
			"%w: it sets %s, which would run when the diagram is rendered or opened",
			ErrUnsafeDiagram, key,
		)
	}

	if key != "href" && key != "src" && key != "xlink:href" {
		return nil
	}

	scheme, _, found := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ":")
	if !found {
		// A relative reference, which the bundler decides about separately.
		return nil
	}

	switch scheme {
	case "http", "https", "mailto", "#":
		return nil

	case "data":
		// An image the bundler inlined. Anything else a data: URL can carry is
		// a document, which is to say a way to run something.
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:image/") {
			return nil
		}

		return fmt.Errorf("%w: it points at %.40s, which is not a picture", ErrUnsafeDiagram, value)

	default:
		return fmt.Errorf("%w: it points at a %s: address", ErrUnsafeDiagram, scheme)
	}
}

// bundleLogger hands what d2's bundler has to say to mark's own log.
type bundleLogger struct{}

func (bundleLogger) Debug(message string) { log.Debug().Msg(message) }
func (bundleLogger) Info(message string)  { log.Info().Msg(message) }
func (bundleLogger) Error(message string) { log.Error().Msg(message) }
