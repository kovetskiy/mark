# Using mark from Go

`github.com/kovetskiy/mark/v17` is the library the command line is built on.
Its godoc is the reference; this page covers what a program generating its own
Markdown needs to know.

## Publishing files

`mark.Run` publishes the files `Config.Files` matches, as `mark publish -f`
does. The settings the command line takes are fields of `mark.Config`, and
the run's report is written to `PublishConfig.Output` in
`Report.OutputFormat`.

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

That boundary holds the names a document asks for; what the file system does
with a name is its own. `os.DirFS` follows a symbolic link out of its
directory. For a directory whose contents you do not control, such as a pull
request's checkout, use the file system of an `os.Root` instead, which does
not:

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

A d2 diagram in a document with an `FS` may not name a local image or import
a local file, and is refused if it does: d2 reads those itself, from disk.
Without `FS`, it reads them beside `Path`, as for a file.

### Links between documents

A relative link to another of the run's documents, by its `Path`, or to one
of the run's files, becomes a link to that page, so documents can link to each
other the way they read in the source tree without that tree being rebuilt on
disk. A link's target is looked for among the run's documents and files
first, then in the linking document's `FS`, or on disk without one.

### Documents and files together

`Documents` may be given with or without `Files`. Files are published first,
then documents, in the order given. Two inputs with the same `Path` or the
same `Key` are refused before anything is published.

`ParentsFromPath` is refused together with `Documents`: the hierarchy is laid
out from files on disk, and a document's `Path` need not be one.

### Orphans and renames

Under `--track-pages`, a page is recorded with the pattern that published it,
and a later run with that pattern looks for orphans among those pages only.
Documents have no pattern, so `DocumentScope` names them instead:

* **With a scope**, a later run with the same scope that no longer publishes a
  document's `Key` finds its page orphaned, and `OnOrphan` acts on it. A
  document with a new `Key`, whose title finds no page, is taken for a rename
  of the one `Key` recorded under the same scope with the same content that
  the run no longer publishes.
* **Without a scope**, a document's page is found again by its `Key` alone. It
  is never reported, archived or deleted because a later run did not publish
  it, and no document is taken for a rename. `OnOrphan` archive or delete on a
  run of documents alone, with no scope, is refused.

Files published alongside keep their own pattern either way, so a `Files` run
never takes a document's page for one of its orphans. A scope can never be
mistaken for a `--files` pattern. Nothing is archived or deleted under a
pattern or a scope that published nothing in that space on the run, unless the
run is partial (see below).

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
names in `DocumentKeys` the keys the change removed, and the old key of each
document it renamed:

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
    // The keys of the records the change removed, and the old key of each
    // record it renamed.
    DocumentKeys: []string{"adr/0007-use-kafka.md", "adr/0012-old-title.md"},
})
```

The run covers only the keys of the scope it publishes or names:

* A key in `DocumentKeys` that is still recorded and not published is an
  orphan, reported and dealt with by `OnOrphan`.
* A published document with a new `Key` is taken for a rename of a key in
  `DocumentKeys` with the same content, and its page follows it, its
  `NoOverwrite` baseline included. A page found by its title for a new `Key`
  is followed from a named key the same way. Either way the entry keeps the
  scope.
* Every other page of the scope is left alone and stays tracked. An orphan the
  run only reports stays tracked too, for a run publishing the whole scope.

Nothing is removed because it is absent, so the guard that holds back the
orphans of a scope that published nothing in a space does not apply to the
keys a partial run names: it was told to remove them. A run finds a scope's
pages only in the spaces it publishes to, though, so `DocumentKeys` with no
`Documents`, `Files` or `Paths` is refused, or under `CI` warned about and does
nothing: a change that only removes documents is left to a run publishing the
whole scope, as a change that only deletes files is.

`DocumentKeys` needs `Partial` and `DocumentScope` too, and is refused without
them: without `Partial` the run already covers every key of its scope, and
the documents left out would be removed. `Partial` with documents that have no
scope changes nothing for them, since they take part in no orphan handling or
renames either way, and is warned about when the run has no files.

Files and documents in one partial run each follow their own rule: `Files` and
`Paths` as [`--partial`](page-tree.md#publishing-only-some-of-the-files)
describes, documents by key as above.

## Publishing one document at a time

`mark.NewPublisher` keeps a run's browser, templates and caches between calls,
for a program that publishes as it renders. `Publisher.Publish` takes a file,
and `Publisher.PublishDocument` a `Document`. A `Publisher` keeps no page
manifest and writes no report, so a document's `Key` is not used: its page is
found by its title in its space, as a file's is, and placed under its parents.
Its relative links are looked for in its `FS`, or on disk around `Path`.

## Errors

A run that fails on a document returns a `*mark.FileError` naming its `Path`
as `File`, and its `Source` as `Source` when that is not `Path`. Under
`ContinueOnError` the run returns a
`*mark.FilesFailedError` holding one for each, and `errors.Is` and `errors.As`
reach each one's cause.
