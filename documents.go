// Documents a library caller hands over from memory rather than as files.

package mark

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog/log"
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
	// beside it. A name climbing above its root is refused, except includes
	// under RenderingConfig.IncludePath, which stay on disk. Nil is the local
	// disk, around Path, under the rules a file is held to.
	//
	// Only names are held to the root: os.DirFS follows a symbolic link out
	// of its directory, where the FS of an os.Root, from os.OpenRoot, does
	// not. Use that for a directory whose contents you do not control.
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

// runInputs is what a run publishes: its files, then its Documents, indexed
// by name and by manifest key.
type runInputs struct {
	all       []input
	documents []input

	// byPath holds each input by its name made absolute against cwd, which is
	// read once rather than on every lookup.
	cwd    string
	byPath map[string]input
	// byKey names the input each manifest key belongs to.
	byKey map[string]string
}

// indexInputs checks the run's documents and indexes them with its files,
// refusing two that would be one page's or one manifest entry's.
func indexInputs(files []string, documents []Document) (*runInputs, error) {
	cwd, _ := os.Getwd()

	r := &runInputs{
		all:    make([]input, 0, len(files)+len(documents)),
		cwd:    cwd,
		byPath: make(map[string]input, len(files)+len(documents)),
		byKey:  make(map[string]string, len(files)+len(documents)),
	}

	for _, file := range files {
		in := fileInput(file)
		r.all = append(r.all, in)
		r.byPath[r.pathKey(file)] = in
		r.byKey[manifest.Key(file)] = file
	}

	for _, doc := range documents {
		in, err := documentInput(doc)
		if err != nil {
			return nil, err
		}

		path := r.pathKey(in.file)
		if other, taken := r.byPath[path]; taken {
			return nil, fmt.Errorf("Document Path %q names %s, which the run already publishes", in.file, other.file)
		}

		key := manifest.Key(in.key)
		if other, taken := r.byKey[key]; taken {
			return nil, fmt.Errorf(
				"Document %s has the manifest key %q, which %s already has: each needs a key of its own",
				in.file, key, other,
			)
		}

		r.byPath[path], r.byKey[key] = in, in.file
		r.all = append(r.all, in)
		r.documents = append(r.documents, in)
	}

	return r, nil
}

// pathKey is name as byPath keys it: as absPath makes it, without asking the
// operating system for the working directory each time.
func (r *runInputs) pathKey(name string) string {
	if filepath.IsAbs(name) || r.cwd == "" {
		return absPath(name)
	}

	return filepath.Join(r.cwd, name)
}

// named is the input called file, for the second pass, which knows the
// documents waiting on a link only by name. Nil, as a run of files alone
// leaves it, is a file.
func (r *runInputs) named(file string) input {
	if r != nil {
		if in, ok := r.byPath[r.pathKey(file)]; ok {
			return in
		}
	}

	return fileInput(file)
}

// checkDocuments refuses what Documents and DocumentScope cannot be given:
// settings that would do nothing while seeming to, before anything is
// published.
func (c Config) checkDocuments() error {
	if c.DocumentScope != "" && !c.Tracking.TrackPages {
		return errors.New("DocumentScope requires Tracking.TrackPages: " +
			"it names the documents in the page manifest, and nothing else keeps one")
	}

	if len(c.DocumentKeys) > 0 {
		// Without Partial the run covers every key of its scope, so a caller
		// publishing some documents and naming the rest would remove those it
		// left out.
		if !c.Tracking.Partial || c.DocumentScope == "" {
			return errors.New("DocumentKeys requires Tracking.Partial and a DocumentScope: " +
				"they name the keys of a scope a partial run covers without publishing them")
		}

		if slices.Contains(c.DocumentKeys, "") {
			return errors.New("DocumentKeys holds an empty key")
		}

		// A run finds a scope's pages only in the spaces it publishes to.
		if len(c.Documents) == 0 && !c.namesFiles() {
			msg := "DocumentKeys with nothing to publish finds no space to look for them in: " +
				"leave a change that only removes documents to a run publishing the whole scope"
			if !c.CI {
				return errors.New(msg)
			}

			log.Warn().Msg(msg)
		}
	}

	if len(c.Documents) == 0 {
		return nil
	}

	if c.ParentsFromPath {
		return errors.New("ParentsFromPath cannot place Documents: " +
			"it lays out the hierarchy from the files on disk, and a Document's Path need not be there")
	}

	if c.Tracking.Partial && c.DocumentScope == "" && !c.namesFiles() {
		log.Warn().Msg("Tracking.Partial does nothing for Documents without a DocumentScope: " +
			"they take part in no orphan handling and no renames, partial or not")
	}

	return nil
}

// namesFiles reports whether the run is given files to publish, by pattern or
// by path, rather than Documents alone.
func (c Config) namesFiles() bool {
	return c.Files != "" || len(c.Paths) > 0
}

// checkUnscopedOrphans refuses --on-orphan archive or delete on a run that
// publishes no files and whose documents have no scope: removing a page needs
// to know which run it belonged to, and such documents belong to none, so it
// would have nothing to act on while saying it had looked. A pattern let
// through by CI matching nothing is only warned about, as that is.
func (c Config) checkUnscopedOrphans(onOrphan report.OnOrphan, files []string) error {
	if onOrphan == report.OnOrphanReport || len(files) > 0 ||
		len(c.Documents) == 0 || c.DocumentScope != "" {
		return nil
	}

	msg := fmt.Sprintf(
		"--on-orphan %s needs a DocumentScope when no files are published: "+
			"documents without one take part in no orphan handling",
		onOrphan,
	)
	if c.namesFiles() {
		log.Warn().Msg(msg)

		return nil
	}

	return errors.New(msg)
}

// documentFiles is where a link from one of the run's documents, or to one,
// is looked for: the run's own documents and files by their name first, then
// what root holds. A file of the run is read from disk even by a document in
// an fs.FS, since it is one of the documents the run publishes.
type documentFiles struct {
	inputs *runInputs
	root   attachment.Root
}

func (f documentFiles) Stat(name string) (fs.FileInfo, error) {
	if in, ok := f.inputs.byPath[f.inputs.pathKey(name)]; ok {
		if !in.document {
			return attachment.Root{}.Stat(in.file)
		}

		return documentInfo{name: filepath.Base(name), size: int64(len(in.content))}, nil
	}

	return f.root.Stat(name)
}

func (f documentFiles) ReadFile(name string) ([]byte, error) {
	if in, ok := f.inputs.byPath[f.inputs.pathKey(name)]; ok {
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

// linkFiles is where doc's relative links are looked for: its root alone,
// unless the run has documents of its own to answer for first.
func (r *publishRun) linkFiles(doc *document) page.LinkFiles {
	if r.inputs == nil {
		return doc.root
	}

	return documentFiles{inputs: r.inputs, root: doc.root}
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
		// As Run names it: empty when the document is its own source.
		source := in.source
		if source == in.file {
			source = ""
		}

		return target, &FileError{File: in.file, Source: source, Err: err}
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
