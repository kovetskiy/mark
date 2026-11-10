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
  a failed document's `FileError` carries it. Empty asks
  `ReportConfig.SourcePath`, as for a file.

`Content` is the Markdown, headers included.

### Where a document's files are read from

`FS` is the file system its images, attachments, linked files, includes and
macro templates are read from. `os.DirFS`, an `embed.FS` and `fstest.MapFS`
all work. With `FS` set, `Path` is a slash-separated path inside it. Nothing
outside `FS` is read: a name climbing above its root is refused, except
includes found under `RenderingConfig.IncludePath`, which stay on disk.

Without `FS`, `Path` is an ordinary file path and the files around it are read
from disk, as for a file the run read. That suits generated text whose images
already sit beside where it would have been written.

A d2 diagram that names a local image needs the document on disk: d2 reads
those images itself.

### Links between documents

A relative link to another of the run's documents, by its `Path`, or to one
of the run's files, becomes a link to that page, so documents can link to each
other the way they read in the source tree without that tree being rebuilt on
disk.

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
  document with a new `Key` whose content matches one recorded under the same
  scope is taken for a rename.
* **Without a scope**, a document's page is found again by its `Key` alone. It
  is never reported, archived or deleted because a later run did not publish
  it, and no document is taken for a rename. `OnOrphan` archive or delete on a
  run of documents alone, with no scope, is refused.

Files published alongside keep their own pattern either way, so a `Files` run
never takes a document's page for one of its orphans. A scope can never be
mistaken for a `--files` pattern.

A `Files` pattern that matches nothing fails the run, documents or not, unless
`CI` is set. Under `CI` it is only warned about, and its pages are not looked
at for orphans.

## Publishing one document at a time

`mark.NewPublisher` keeps a run's browser, templates and caches between calls,
for a program that publishes as it renders. `Publisher.Publish` takes a file,
and `Publisher.PublishDocument` a `Document`. A `Publisher` keeps no page
manifest and writes no report, so a document's `Key` is not used: its page is
found by its title and parents.

## Errors

A run that fails on a document returns a `*mark.FileError` naming its `Path`
as `File` and its `Source`. Under `ContinueOnError` the run returns a
`*mark.FilesFailedError` holding one for each, and `errors.Is` and `errors.As`
reach each one's cause.
