# Using mark from Go

`github.com/kovetskiy/mark/v17` is the library the command line is built on.
Its godoc is the reference; this page covers what a program generating its own
Markdown needs to know.

## Publishing files

`mark.Run` publishes the files `Config.Files` matches, as `mark publish -f`
does. The settings the command line takes are fields of `mark.Config`, and
the run's report is written to `PublishConfig.Output` in
`Report.OutputFormat`.

### Rendering settings

`Run`, `NewPublisher` and `PublishConfig.Validate` check
`PublishConfig.Rendering` before anything is published, and refuse a value the
renderers cannot act on.

* `Features` is the optional features to compile with, each a name
  `markdown.KnownFeatures()` lists. Left nil or empty it switches none of them
  on, which is not what the command line does when `--features` is not given:
  pass `markdown.DefaultFeatures()` for its `mermaid` and `mention`. `none` on
  its own also means no optional feature; beside another name it is refused,
  as is a name mark does not know.
* `ImageAlign` is `left`, `center` or `right`, in any case and with space
  around it, or empty for no alignment. A document's `Image-Align` header takes its place for that
  document.

## Publishing documents from memory

A program that renders Markdown from templates, or from another format, can
hand it to mark directly instead of writing it to a staging directory first.
`Config.Documents` takes a list of `mark.Document`:

```go
err := mark.Run(ctx, mark.Config{
    Connection:    connection,
    Tracking:      mark.TrackingConfig{TrackPages: true, OnOrphan: report.OnOrphanArchive},
    DocumentScope: "api-reference",
    Documents: []mark.Document{{
        Path:    "reference/users.md",
        Key:     "reference/users.md",
        Source:  "templates/users.md.tmpl",
        Content: rendered,
        FS:      os.DirFS("assets"),
    }},
})
```

A document keeps three names apart:

* `Path` is where it sits. Its relative links, images, attachments and
  includes resolve from there, and the report, warnings and errors name it.
  Nothing needs to exist at that path.
* `Key` is what the [page manifest](page-tree.md#tracking-pages-across-renames)
  remembers it by under `TrackPages`. It finds the document's page again on
  the next run, so it has to be the same on every run. Empty means `Path`.
* `Source` is the file a person edits to change it, such as the template. The
  report gives it as the page's `source`, the GitHub format annotates it, and
  a failed document's `FileError` carries it unless it is the same as `Path`.
  Empty asks `ReportConfig.SourcePath`, as for a file.

`Content` is the Markdown, headers included.

### Where a document's files are read from

`FS` is the file system its images, attachments, linked files, includes and
macro templates are read from. `os.DirFS`, an `embed.FS` and `fstest.MapFS`
all work. With `FS` set, `Path` is a slash-separated path inside it. A name
climbing above its root is refused, except includes found under
`RenderingConfig.IncludePath`, which stay on disk.

The boundary holds through symbolic links too, whenever the file system can
report them (an `fs.ReadLinkFS`, as `os.DirFS` and `fstest.MapFS` are): each
name is resolved one link at a time before it is read, and a link to an
absolute path or above the root is refused like a name climbing out, as is a
chain of more than 40 links. A file system that does not report links is held
by name alone, so a wrapper around `os.DirFS` should pass `fs.ReadLinkFS` on.

For a directory whose contents may change while the run reads it, the file
system of an `os.Root` also keeps out a link swapped in between that check and
the read:

```go
assets, err := os.OpenRoot("assets")
if err != nil {
    return err
}
defer assets.Close()

doc.FS = assets.FS()
```

Without `FS`, `Path` is an ordinary file path and the files around it are read
from disk, as for a file the run read. That suits generated text whose images
already sit beside where it would have been written.

A d2 diagram's imports and local images are read from the `FS` too, around
`Path` and held to its root, symbolic links included. Its local images are
inlined into the drawing, as on disk for an SVG, and for a PNG as well, since
the browser that draws one cannot read the `FS`; a PNG's checksum covers what
it imported and inlined. Without `FS`, they are read beside `Path`, as for a
file.

### Links between documents

A relative link to another of the run's documents, by its `Path`, or to one
of the run's files, becomes a link to that page, so documents can link to each
other the way they read in the source tree without that tree being rebuilt on
disk. A link's target is looked for among the run's documents and files
first, then in the linking document's `FS`, or on disk without one.

A link written inside a fragment that a document in an `FS` includes from
`RenderingConfig.IncludePath` is written from where the fragment sits on
disk, so it is also looked for there, and only inside the include path: one
climbing out of it is left as written.

### Documents and files together

`Documents` may be given with or without `Files`. Files are published first,
then documents, in the order given. Two inputs with the same `Path` or the
same `Key` are refused before anything is published.

### Parents from the path

With `ParentsFromPath`, documents are laid out by their `Path` together with the
run's files, as one hierarchy (see
[Parents from the directory a file is in](page-tree.md#parents-from-the-directory-a-file-is-in)).
A directory that files and documents both lie in is one page. A document named
`README.md` or `index.md` is its directory's page, and titles it from its
`Content`, by its `Title` header, its front matter, or its leading heading with
`TitleFromH1`. A directory without a title of its own document takes one from
its `.pages` file, by the same rules and in the same order as on disk. The file
is read from where the directory's documents are:

* **On disk** for a directory that a file, or a document without `FS`, lies in,
  whatever else lies there.
* **From the `FS`** for a directory that only documents in an `FS` lie in, held
  to its root as any read of it is: a `.pages` file, or a directory above it,
  that is a symbolic link out of the `FS` fails the run with
  `attachment.ErrOutsideProject`. A `.pages` file on disk at the same path is
  not read.
* **From each document's `FS` in turn** when documents from several `FS`s lie
  in the directory or below it, in the order `Documents` gives them: the first
  `.pages` file with a `title:` names the directory, so it may sit in any one
  of them, and one without a title is passed over. Where more than one has a
  title, give the document whose `FS` holds the one meant first. A `.pages`
  file one of them refuses to read before a title is found fails the run.

A run of documents alone has no `--files` pattern to take the root from, so it
needs `ParentsFromPathRoot`, and is refused before publishing without it:

```go
config.ParentsFromPath = true
config.ParentsFromPathRoot = "reference" // documents' Paths lie under it
```

Under `TrackPages`, the page mark created for a directory that a file lies in is
remembered by its path, under the files' pattern, as for a run of files. One
that only documents lie in is remembered under a key of its own,
`manifest.DocumentDirectoryKey(DocumentScope, directory)`, in the documents'
scope. So a directory of files and a directory of documents with the same path
never share an entry, and the next run of the scope that leaves no document in
a directory finds its page an orphan. A partial run never takes a directory
for gone, since the keys it is given say nothing of where those documents lie;
the next full run of the scope does.

`Publisher.PublishDocument` lays out no hierarchy, since `ParentsFromPath` is a
setting of a run.

### Orphans and renames

Under `--track-pages`, a page is recorded with the pattern that published it,
and a later run with that pattern looks for orphans among those pages only.
Documents have no pattern, so `DocumentScope` names them instead:

* **With a scope**, a later run with the same scope that no longer publishes a
  document's `Key` finds its page orphaned, and `OnOrphan` acts on it. A
  document with a new `Key`, whose title finds no page, is taken for a rename
  of the one `Key` recorded under the same scope with the same content that
  the run no longer publishes, or, with `Tracking.RenameSimilarity`, of the
  one whose page's text is clearly the closest to its own (see
  [Renamed and edited at once](#renamed-and-edited-at-once)).
* **Without a scope**, a document's page is found again by its `Key` alone. It
  is never reported, archived or deleted because a later run did not publish
  it, and no document is taken for a rename. `OnOrphan` archive or delete on a
  run of documents alone, with no scope, is refused.

Files published alongside keep their own pattern either way, so a `Files` run
never takes a document's page for one of its orphans. A scope can never be
mistaken for a `--files` pattern. Nothing is archived or deleted under a
pattern or a scope that published nothing in that space on the run; a
partial run's scope is held to more (see below).

Changing a scope is like changing a `--files` pattern: the pages recorded
under the old one stay its own, and are never orphans of the new one. Keep a
scope the same from run to run, as you keep a `Key`.

A `Files` pattern that matches nothing fails the run, documents or not, unless
`CI` is set. Under `CI` it is only warned about, and its pages are not looked
at for orphans.

### Publishing some of a scope's documents

A run with a scope covers every `Key` recorded under it: one it does not
publish is an orphan. A program that renders only the documents a change
touched says so with `Tracking.Partial`, as `--partial` does for files, and
lists in `DocumentKeys` every key of the scope that still exists, which it can
usually do without rendering anything:

```go
err := mark.Run(ctx, mark.Config{
    Connection: connection,
    Tracking: mark.TrackingConfig{
        TrackPages: true,
        Partial:    true,
        OnOrphan:   report.OnOrphanArchive,
    },
    DocumentScope: "adr",
    // Rendered from the records the change added or edited, a renamed
    // record under its new key.
    Documents: changed,
    // The key of every record there is now, rendered or not. Keys of
    // Documents may be among them.
    DocumentKeys: allKeys,
})
```

The run's documents and `DocumentKeys` are then the scope's whole key set, so
the caller keeps no history:

* A key recorded under the scope and in neither is gone: an orphan, reported
  and dealt with by `OnOrphan`, as in a run publishing the whole scope. A key
  left out of the list by mistake is gone too, so the run logs how many keys
  of each scope it took as gone, and each is in the report's orphans.
* A published document with a new `Key` is taken for a rename of the one gone
  key with the same content, or with `Tracking.RenameSimilarity` of a gone
  key whose page's text is clearly the closest to its own, and its page
  follows it, its `NoOverwrite` baseline included. A page found by its title for a new `Key` is followed
  from a gone key the same way. Either way the entry keeps the scope.
* A key in `DocumentKeys` is left alone and stays tracked, its baseline
  unchanged. It is never taken for a rename, so a new document with the same
  content is a copy with a page of its own, and a page it owns that another
  document finds by title is reported as shared by both.
* An orphan the run only reports stays tracked, as for `--partial`.

A scope's orphans in a space are archived or deleted only while some key of
the scope tracked there before the run is still there: published, followed to
a new key, or listed. A list naming none of them, beside documents new to the
scope or none, would read every page of the scope there as gone, so those are
only reported, with the reason. A run finds a scope's pages only in the spaces
it publishes to, so `DocumentKeys` with no `Documents`, `Files` or `Paths` is
refused, or under `CI` warned about and does nothing: the next run publishing
a document of the scope, given its keys, finds the ones removed.

Without `DocumentKeys`, a partial run knows only the keys it publishes. It
finds no orphans in the scope, takes no document for a rename, and follows a
document by its own `Key` alone; a moved document whose page it finds by
title is reported as sharing it with the key it moved from. That is the safe
choice for a caller that cannot list its scope; a later run that can deals
with what was left.

`DocumentKeys` needs `Partial` and `DocumentScope`, and is refused without
them: without `Partial` the run's documents are the whole scope already, and
those left out would be removed. An empty `DocumentKeys` is no list: a caller
whose documents are the whole scope publishes them without `Partial`.

`Partial` with documents that have no scope changes nothing for them, since
they take part in no orphan handling or renames either way, and is warned
about when the run has no files.

Files and documents in one partial run each follow their own rule: `Files` and
`Paths` as [`--partial`](page-tree.md#publishing-only-some-of-the-files)
describes, documents by key as above.

### Renamed and edited at once

A record is often renamed, retitled and edited in one change, and its content
then matches no gone key exactly: it would get a new page, and the old one
would be an orphan. `Tracking.RenameSimilarity` follows such a rename by how
alike the texts are:

```go
Tracking: mark.TrackingConfig{
    TrackPages:       true,
    Partial:          true,
    RenameSimilarity: 0.6,
},
```

It is the least similarity, from 0 to 1, a document and a gone key's page must
have; 0, the default, follows only renames with the same content. The text
compared is the document's Markdown without its headers, against the gone
key's page as it is in Confluence, both reduced to lowercased words without
markup. The measure is the Jaccard similarity of their shingles, every run of
three consecutive words: the shingles both texts have, over those either has.
A text of fewer than ten words is like nothing, itself included: a stub such
as "TBD" says nothing about which record it was.

0.6 is a good value. On the ADRs `internal/similar` measures against, one
retitled, its status changed, a sentence reworded and a paragraph added scores
about 0.78; the same with its decision rewritten about 0.37, which is a new
decision more than the old one renamed; and two different records of one
template under 0.05.

Taking the wrong page overwrites an unrelated document, so the match is
careful:

* Exact matches come first, unchanged, and documents found by their `Key`,
  their title or their exact content, with the keys and pages they take, are
  left out of the comparison.
* Only a document and a gone key of the same scope and space are compared.
* Each gone key is taken by one document at most: a pair is taken only when
  it is at least as alike as the setting asks, and ahead by 0.1 of the
  document's next closest gone key, of the gone key's next closest document,
  and of the document's closest other document in the run. The last keeps a
  template's text, which every record of a scope shares, from pairing a
  deleted record with an unrelated new one. A document close to some gone key
  but clear of none gets a new page, as without the setting, and a warning
  naming the keys and scores.
* Under `NoOverwrite`, a gone key whose page was edited in Confluence since
  mark wrote it, or that has no version mark wrote to tell, is never taken.
  The page is read again at the document's turn, and one edited, deleted or
  taken by another document since the comparison is not taken either, with
  a warning.
* Each rename followed is logged and given to the document as a warning of
  kind `report.WarningRename`, naming the new key, the old key, the page and
  the score.

It applies wherever the run knows which keys of the scope are gone: a run
publishing the whole scope, and a partial run given `DocumentKeys`. It does
not apply to `Files` and `Paths`, to documents without a scope, or to a
partial run without `DocumentKeys`. A dry run makes the same decision and
logs it, writing nothing.

The comparison is made once a run per space, when the first document with no
page of its own asks, and only when there are both such documents and gone
keys: each gone key's page is then read once, and a page taken is read again
at the document's turn, as an exact rename's is. A run with nothing to match
makes no request for it.

A parent renamed, retitled and edited at once is followed when its own turn
comes, but a child published before it, declaring the new title, does not
find it by similarity: it creates the parent under the new title, as without
the setting.

## Publishing one document at a time

`mark.NewPublisher` keeps a run's browser, templates and caches between calls,
for a program that publishes as it renders. `Publisher.Publish` takes a file,
and `Publisher.PublishDocument` a `Document`. A `Publisher` keeps no page
manifest and writes no report, so a document's `Key` is not used: its page is
found by its title in its space, as a file's is, and placed under its parents.
Its relative links are looked for in its `FS`, or on disk around `Path`.

A `Publisher` and the `confluence.API` it is given are meant for one batch.
The API remembers the pages it has looked up by title, a title that named no
page included, for as long as it lives, and does not notice one that something
else creates, retitles or removes in the meantime. A program that runs for long
makes a new API and a new `Publisher` for each batch.

## Errors

A run that fails on a document returns a `*mark.FileError` naming its `Path`
as `File`, and its `Source` as `Source` when that is not `Path`. Under
`ContinueOnError` the run returns a
`*mark.FilesFailedError` holding one for each, and `errors.Is` and `errors.As`
reach each one's cause.
