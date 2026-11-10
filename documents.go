// Documents a library caller hands over from memory rather than as files.

package mark

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
)

// Document is a document to publish that is not read from a file: Markdown a
// caller generated, from templates or from another format, handed to Run in
// Config.Documents or to Publisher.PublishDocument.
//
// It is published as a file of that Path holding Content would be, with three
// names kept apart: Path is where it sits among the run's documents, Key is
// what the page manifest remembers it by, and Source is the file a person
// edits to change it.
type Document struct {
	// Path names the document as a file path would: the directory its
	// relative links, images, attachments and includes are resolved from, the
	// name the run's report, warnings and errors give it, and what a link
	// from another of the run's documents names it by. Nothing need exist
	// there.
	//
	// With FS set, Path is a path inside FS, slash-separated and relative to
	// its root, as fs.ValidPath requires. Without it, Path is an OS path.
	// Either way it shares one namespace with the run's files: a link from a
	// file to Path, or from a document to one of the run's files, resolves as
	// it would on disk. Any other file a link names is looked for in FS, or
	// on disk without one. Two documents, or a document and a file, may not
	// share a Path.
	Path string

	// Key is what the page manifest keys the document by under
	// Tracking.TrackPages, normalised as manifest.Key normalises a file's
	// path. It is what finds the document's page again on the next run, so
	// it has to stay the same from run to run: a temporary directory in it
	// gives every run new keys and a page the manifest never matches. Empty
	// is Path.
	Key string

	// Source names the file the document was made from, such as a template,
	// for the report, as ReportConfig.SourcePath does for a file; it is
	// asked about Path only when this is empty.
	Source string

	// Content is the Markdown, headers included.
	Content []byte

	// FS is where the files Content names relative to Path are read from:
	// images, attachments, linked files, and includes and macro templates
	// beside it. Nothing outside it is read -- a name climbing above its root
	// is refused -- except includes under RenderingConfig.IncludePath, which
	// stay on disk. Nil is the local disk, around Path, under the rules a
	// file is held to.
	//
	// A d2 diagram in a document with an FS may not name a local file; d2
	// reads those from disk itself.
	FS fs.FS
}

// input is one document a run publishes: a file it reads, or a Document it
// was handed. Everything per document is keyed on file, as it is for a file.
type input struct {
	// file is a file's path, or the Document's Path.
	file string
	// key is what the manifest keys the document by.
	key string
	// source is Document.Source.
	source string
	// content is the Document's, when document is set.
	content  []byte
	document bool
	root     attachment.Root
}

// fileInput is the file at path.
func fileInput(file string) input {
	return input{file: file, key: file}
}

// documentInput is doc, checked.
func documentInput(doc Document) (input, error) {
	if doc.Path == "" {
		return input{}, errors.New("a Document needs a Path: it is what its links, images and includes are resolved from")
	}

	if doc.FS != nil && !fs.ValidPath(doc.Path) {
		return input{}, fmt.Errorf(
			"Document Path %q is not a path inside its FS: it has to be slash-separated, "+
				"relative and clean, as fs.ValidPath requires",
			doc.Path,
		)
	}

	key := doc.Key
	if key == "" {
		key = doc.Path
	}

	return input{
		file: doc.Path, key: key, source: doc.Source,
		content: doc.Content, document: true, root: attachment.FSRoot(doc.FS),
	}, nil
}

// read reads the document as readSource reads a file.
func (in input) read() (source, markdown []byte, lineOf func(int) int, err error) {
	if !in.document {
		return readSource(in.file)
	}

	return prepareSource(in.file, in.content)
}

// documentInputs checks the run's documents and makes inputs of them, refusing
// two that would be one page's or one manifest entry's, among themselves or
// with the run's files.
func documentInputs(documents []Document, files []string) ([]input, error) {
	inputs := make([]input, 0, len(documents))

	paths := make(map[string]string, len(files)+len(documents))
	keys := make(map[string]string, len(files)+len(documents))
	for _, file := range files {
		paths[absPath(file)] = file
		keys[manifest.Key(file)] = file
	}

	for _, doc := range documents {
		in, err := documentInput(doc)
		if err != nil {
			return nil, err
		}

		if other, taken := paths[absPath(in.file)]; taken {
			return nil, fmt.Errorf("Document Path %q names %s, which the run already publishes", in.file, other)
		}
		paths[absPath(in.file)] = in.file

		if other, taken := keys[manifest.Key(in.key)]; taken {
			return nil, fmt.Errorf(
				"Document %s has the manifest key %q, which %s already has: each needs a key of its own",
				in.file, manifest.Key(in.key), other,
			)
		}
		keys[manifest.Key(in.key)] = in.file

		inputs = append(inputs, in)
	}

	return inputs, nil
}

// checkDocuments refuses what Documents and DocumentScope cannot be given:
// settings that would do nothing while seeming to, before anything is
// published.
func (c Config) checkDocuments(onOrphan report.OnOrphan) error {
	if c.DocumentScope != "" && !c.Tracking.TrackPages {
		return errors.New("DocumentScope requires Tracking.TrackPages: " +
			"it names the documents in the page manifest, and nothing else keeps one")
	}

	if len(c.Documents) == 0 {
		return nil
	}

	if c.ParentsFromPath {
		return errors.New("ParentsFromPath cannot place Documents: " +
			"it lays out the hierarchy from the files on disk, and a Document's Path need not be there")
	}

	// Removing a page needs to know which run it belonged to, and documents
	// without a scope belong to none: --on-orphan would have nothing to act on
	// while saying it had looked.
	if onOrphan != report.OnOrphanReport && c.Files == "" && c.DocumentScope == "" {
		return fmt.Errorf(
			"--on-orphan %s needs a DocumentScope for a run of Documents alone: "+
				"documents without one take part in no orphan handling",
			onOrphan,
		)
	}

	return nil
}

// inputNamed is the input of inputs whose name is file.
func inputNamed(inputs []input, file string) input {
	for _, in := range inputs {
		if in.file == file {
			return in
		}
	}

	return fileInput(file)
}

// documentScopePattern is what the manifest records a run's documents under
// in place of a --files pattern: none for no scope, and otherwise one no
// pattern can be, since manifest.Key never leaves "//" inside a path. A
// --files run therefore never takes a document's page for one of its own
// orphans, nor the other way round.
func documentScopePattern(scope string) string {
	if scope == "" {
		return ""
	}

	return "documents://" + scope
}

// documentFiles is where a link from one of the run's documents, or to one,
// is looked for: the run's own documents and files by their name first, then
// what root holds. A file of the run is read from disk even by a document in
// an fs.FS, since it is one of the documents the run publishes.
type documentFiles struct {
	byPath map[string]input
	root   attachment.Root
}

// newDocumentFiles indexes inputs by their name.
func newDocumentFiles(inputs []input) map[string]input {
	byPath := make(map[string]input, len(inputs))
	for _, in := range inputs {
		byPath[absPath(in.file)] = in
	}

	return byPath
}

func (f documentFiles) Stat(name string) (fs.FileInfo, error) {
	if in, ok := f.byPath[absPath(name)]; ok {
		if !in.document {
			return attachment.Root{}.Stat(in.file)
		}

		return documentInfo{name: filepath.Base(name), size: int64(len(in.content))}, nil
	}

	return f.root.Stat(name)
}

func (f documentFiles) ReadFile(name string) ([]byte, error) {
	if in, ok := f.byPath[absPath(name)]; ok {
		if !in.document {
			return attachment.Root{}.ReadFile(in.file)
		}

		return in.content, nil
	}

	return f.root.ReadFile(name)
}

// documentInfo describes a document that is not on disk.
type documentInfo struct {
	name string
	size int64
}

func (i documentInfo) Name() string       { return i.name }
func (i documentInfo) Size() int64        { return i.size }
func (i documentInfo) Mode() fs.FileMode  { return 0o444 }
func (i documentInfo) ModTime() time.Time { return time.Time{} }
func (i documentInfo) IsDir() bool        { return false }
func (i documentInfo) Sys() any           { return nil }

// linkFiles is where doc's relative links are looked for, or nil for the
// local disk alone, which is all a run of files ever needs.
func (r *publishRun) linkFiles(doc *document) page.LinkFiles {
	if len(r.documents) == 0 && doc.root.FS() == nil {
		return nil
	}

	return documentFiles{byPath: r.documents, root: doc.root}
}

// PublishDocument publishes doc as Publish publishes a file, and checks the
// ac: links it holds once it has. Its relative links resolve against
// doc.FS, or the disk around doc.Path without one; a Publisher keeps no page
// manifest, so doc.Key is not used. The *FileError of a document that fails
// names doc.Path, and doc.Source as its Source.
func (p *Publisher) PublishDocument(ctx context.Context, doc Document) (*confluence.PageInfo, error) {
	if p == nil || p.run == nil {
		return nil, errNoPublisher
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	in, err := documentInput(doc)
	if err != nil {
		return nil, err
	}

	target, err := p.publish(ctx, in)
	if err != nil {
		return target, &FileError{File: in.file, Source: in.source, Err: err}
	}

	return target, nil
}

// sourcesOf is the run's ReportConfig.SourcePath, answered first by each
// document's own Source. Nil when neither names anything.
func sourcesOf(inputs []input, sourcePath func(string) string) func(string) string {
	named := map[string]string{}
	for _, in := range inputs {
		if in.source != "" {
			named[in.file] = in.source
		}
	}

	if len(named) == 0 {
		return sourcePath
	}

	return func(file string) string {
		if source, ok := named[file]; ok {
			return source
		}

		if sourcePath == nil {
			return file
		}

		return sourcePath(file)
	}
}
