# Publishing

## Publishing to a page by its URL

`--url` (`-l`, `MARK_PAGE_URL`, `url` in the configuration file) publishes to
the page a browser shows at that URL, whatever the document's headers say. It
takes `.../pages/viewpage.action?pageId=ID`, `.../spaces/KEY/pages/ID/Title`
or a short `.../x/...` link, which carries the page id, and the instance comes
from it too, context path included, unless a base URL is set:

```bash
mark publish --url "https://example.atlassian.net/wiki/spaces/DOCS/pages/123456/Setup" --files setup.md
```

The page is found by its id, so the document needs no `Space` or `Title`
header. Any headers it has are ignored, with a warning, and so is
`--track-pages`, whose record is kept per space and per file. Name one file:
every file of the run would be published to the same page.

A base URL set by `--base-url`, `MARK_BASE_URL` or `base-url` in the
configuration file is where requests go, and the URL must be on that
instance: a page id names a page of one instance only, so a URL on another
one is refused before anything is sent. The comparison is the one
[`mark export --url`](export.md) makes: case, a trailing dot in the host, a
default port and a missing context path such as `/wiki` do not count, and
with the `api.atlassian.com` gateway configured any Cloud site's `https` URL
is on it.

A base URL naming an internal host refuses a URL copied from the browser on a
public alias of the same Confluence, since nothing in either URL says the two
hosts are one server. Set the base URL to the host in the URL, or leave
`--url` out and name the page with `Space` and `Title` headers.

A URL that names the instance alone -- `https://example.atlassian.net/wiki`,
the home page or dashboard a browser opens it on, or the base URL itself --
gives the base URL and no page, and each document names its page with its
headers. Any other URL has
to carry a page id. One that does not is refused before anything is sent,
since the documents' headers would otherwise decide where they go and the
page the URL names would stay as it was: a `/display/KEY/Title` address, a
space's overview, a blog post, or a bare page id in place of a URL. Open the
page and copy one of the forms above, or leave `--url` out. When the URL comes
from `MARK_PAGE_URL` or the configuration file, where `mark export` may need
it, `--url ""` on the command line sets it aside for one run.

`mark export --url` is the same setting, under the same names. It reads
`/display/KEY/Title` and blog post addresses as well, since it looks the page
up rather than writing to it.

> **Upgrading to v17:** `--target-url`, `MARK_TARGET_URL` and the configuration
> key `target-url` are now `--url`, `MARK_PAGE_URL` and `url`, and export's
> `MARK_URL` is now `MARK_PAGE_URL`. The old flag and key are refused, naming
> the new one; the old variables are ignored. Publish and export share the
> setting, so a `url` key that named a page for export now sends publish to
> that page too.

## A header on every page

`--page-header` names a template that mark places at the top of every page it
publishes, inside the page's layout. The documents themselves don't change. The
usual use is a warning that the page is generated, with a link to the file to
edit:

```bash
mark publish --page-header generated.md --files "docs/**/*.md"
```

A file with a Markdown extension (`.md`, `.markdown`, `.mdown`, `.mkd`, `.mkdn`
or `.mdwn`) is compiled like a document, with the same `--features`. Anything
else is published as Confluence storage format. Either way it is a Go template,
executed once per page with:

| field | value |
| --- | --- |
| `.Path` | the document's path relative to the working directory, with `/` separators |
| `.EscapedPath` | `.Path` with each segment escaped for a URL |
| `.Title` | the page title |
| `.Space` | the page's space key |

With `--url`, `.Title` and `.Space` come from the page being updated and
`--space`. Combined with `--compile-only`, there is no page to read, so `.Title`
is empty.

```markdown
> [!WARNING]
> This page is generated from
> [{{ .Path | xmlesc }}](https://github.com/org/repo/blob/main/{{ .EscapedPath }}).
> Edit it there: changes made in Confluence are overwritten.
```

The same in storage format:

```html
<ac:structured-macro ac:name="warning">
  <ac:parameter ac:name="title">Generated from Git</ac:parameter>
  <ac:rich-text-body>
    <p>Edit <a href="https://github.com/org/repo/blob/main/{{ .EscapedPath | xmlesc }}">{{ .Path | xmlesc }}</a> instead.</p>
  </ac:rich-text-body>
</ac:structured-macro>
```

A file name can hold `&` or `<`, so pass `.Path`, `.Title` and `.Space` through
`xmlesc`. Before anything is published, mark executes the template with such
names, so a header that wouldn't be well-formed XML stops the run instead of
failing page by page.

The template can't read environment variables, because the environment holds
the Confluence password. To show something that changes per run, such as the
commit, write it into the template before running mark, for example with
`envsubst`.

The header is part of the page body, so with `--changes-only` a change to it
republishes every page. Pair it with `--edit-lock` to keep people from editing
the pages in Confluence.

## Reporting what a run did

By default Mark prints the address of each page as it publishes, which is what
it has always done. `--output-format` offers two other shapes.

`json` describes the whole run as one object:

```bash
mark publish --output-format json --files "docs/**/*.md" | jq -r '.pages[] | select(.status=="published") | .url'
```

```json
{
  "pages": [
    {
      "file": "docs/architecture.md",
      "status": "published",
      "space": "DOCS",
      "title": "Architecture",
      "pageId": "1004",
      "url": "https://example.atlassian.net/wiki/display/DOCS/Architecture"
    },
    {
      "file": "docs/draft.md",
      "status": "skipped",
      "reason": "the document is not synchronized"
    }
  ],
  "orphans": [
    {"file": "docs/old.md", "pageId": "1007", "title": "Old", "action": "delete"}
  ]
}
```

A page is `published`, `unchanged` (`--changes-only` found nothing to do),
`skipped` (not synchronized, or edited in Confluence under `--no-overwrite`) or
`failed`, with `reason` saying which in the last two cases. A page is only
`published` once everything the run had to do to it -- its body, labels and
properties -- has been done. Each document appears once, with what finally
became of it, so one [published again](links.md#links-between-pages-published-together)
whose second publish failed is `failed`.

With `--dry-run` and `--changes-only` together, the report says what a real run
would do: `would-create` for a page that does not exist yet, `would-update` for
one a real run would change, and `unchanged` for one it would leave alone. A
page would change when its content, title, emoji or appearance differs, when it
would be moved under the parent its headers declare, when its labels differ
from its `Label` headers, or when an attachment -- declared, embedded or
rendered -- would be uploaded; the `reason` of a `would-update` names any of
the last three. Only the pages that would change have their HTML printed, and
only where any dry run's is, as said below. A page
`--no-overwrite` would leave alone is `skipped`, as it is on a real run, and
is not printed either. A real run's report only tells a content update apart,
so a page previewed as `would-update` only for a move, its labels or an attachment is
reported `unchanged` by the real run that makes that change.

Not compared, so a page differing only in them is `unchanged` although a real
run still applies them: [content properties](metadata.md#confluence-content-properties),
the `--edit-lock` restriction, and the `Order` of a page among its siblings.
The restrictions a document declares are compared: a page whose restrictions
would change is `would-update`.

A run that publishes nothing and compares nothing still lists each document it
got through, so that its warnings can be read: `compiled` under
`--compile-only`, and `would-publish` under `--dry-run` without
`--changes-only`. Such a page has the `space` and `title` its headers give, and
a `pageId` and `url` only when `--dry-run`, given with `--compile-only` or not,
found the page it would publish to; a compile alone looks for none. A page
`--no-overwrite` would leave alone is `skipped` under `--dry-run` too, and its
HTML is not printed. `github` annotates its warnings and writes no notice
for the status itself, as for `unchanged`, so a pull request gate needs no
Confluence credentials:

```bash
mark publish --compile-only --output-format github --files "docs/**/*.md"
```

`--compile-only` and `--dry-run` print each document's HTML only with the
default `url` output format, whose report is the addresses of published pages
and so empty here, or with a [`--report-file`](#writing-the-report-to-a-file),
which keeps the report apart. With `json` or `github` on standard output the
report alone is written. HTML ahead of a `json` report would leave it
unparseable, and a line of a document's HTML on the same output as a `github`
report is read by GitHub Actions as a workflow command: a code block holding
`::error file=README.md::...` would add an annotation of the pull request's
making to the gate, and one holding `::stop-commands::` would hide the gate's
own. Give `--report-file` to have both:

```bash
mark publish --compile-only --output-format json --report-file report.json --files "docs/**/*.md" > compiled.html
```

`warnings` lists what was wrong with a document that did not stop it
publishing, and `warningDetails` says the same with each warning's `kind`, the
`file` it is about and, where it is about one, the `line`:

```json
{
  "file": "docs/guide.md",
  "status": "published",
  "warnings": ["front matter key \"colour\" is not read by mark and was ignored"],
  "warningDetails": [
    {"kind": "unknown-key", "file": "docs/guide.md", "line": 4, "message": "front matter key \"colour\" is not read by mark and was ignored"}
  ]
}
```

A line is given for a front matter key, a value one cannot hold (the key's
line), and a header below the opening comments. A document that fails keeps
what was found before it did.

A `kind` is one of:

| kind | what it is |
| --- | --- |
| `unresolved-link` | a link that does not resolve, under `--check-links-warn-only`; an `ac:` link is checked once the whole run has published, and listed against every document that holds it. A link to a page nothing will publish -- in a dry run, or still missing on the second pass -- is one whenever `--check-links` covers internal links |
| `source-only-link` | under `--links-source-root-check-only`, a relative link to a file only the source tree holds: it passes the check and is left as written, a path that leads nowhere in Confluence |
| `unknown-key` | a front matter key mark does not read: another tool's, or a misspelled setting |
| `ignored-value` | a value its front matter key cannot hold, such as a number among labels or a list for a title, dropped |
| `stranded-header` | a header below the opening comments, published as text rather than applied |
| `metadata-ignored` | the document's metadata set aside, because `--url` names the page |
| `attachment` | a file not uploaded, or uploaded and never used |
| `invalid-characters` | text XML cannot carry, replaced with U+FFFD |
| `column-widths` | a `Column-Widths` comment that was not applied |
| `inline-comment` | a comment `--preserve-comments` could not keep |
| `include` | an include in a fragment that now resolves beside the fragment, not the document |
| `placement` | a page not placed where its document asks: found outside its declared parents, outside `--parents-from-path-root`, under a parent title several documents declare, given an `Order` its parent cannot be found for, or left behind in the space it used to publish to |
| `shared-page` | a page the manifest records for this document and another, listed against both |
| `missing-page` | a page or folder the manifest recorded, for the document or a parent, that no longer exists |
| `other-space` | a page or folder the manifest recorded, for the document, the file it was renamed from or a parent, that is in another space; it is left alone and the document is published by title |

Each is also logged, and listed once even when a document is published twice in
a run. `github` annotates a warning against its file and line; when
a library caller names the file's source with `ReportConfig.SourcePath`, it is
annotated against the source without the line, which belongs to the file mark
read. The warning then gives the source as `source`.

`orphans` lists the [tracked pages whose source file is
gone](page-tree.md#removing-pages-whose-files-are-gone), under `--track-pages`, with the
`--on-orphan` action taken: `report` for a page that was only reported and left
where it is, `archive` or `delete` for one that was archived or trashed. A page
`--on-orphan` would have removed but left where it is -- because it has child
pages or folders, because it is in another space than the manifest that records
it, or because nothing was published in its space this run -- is
`report`, with a `reason` saying why.

`untracked` lists the spaces published to without a page manifest under
[`--manifest-page-optional`](page-tree.md#when-the-manifest-page-does-not-exist-yet),
whose mapping was not saved because the manifest page was still missing at the
end of the run, each with the page it was given as. Under `--no-overwrite`,
every existing page in such a space was skipped, and is on every run until the
manifest page exists:

```json
"untracked": [
  {"space": "DOCS", "manifestPage": "Team Handbook"}
]
```

`errors` lists what failed that was not one document's doing: `ac:` links that
still resolve to no page once everything is published, a page manifest that
could not be saved, a failed ordering pass, an orphan that could not be
handled. The report is written however the run ends, so a run that fails still
says which pages it published and why it failed. A document that fails is
recorded against that document, as `failed` with its `reason`, and is not
repeated here.

```json
{
  "pages": [
    {"file": "docs/guide.md", "status": "published", "title": "Guide", "pageId": "1009"}
  ],
  "errors": [
    "1 link does not resolve:\n  docs/guide.md: link \"ac:Nowhere\" does not resolve: there is no page \"Nowhere\" in space \"DOCS\""
  ]
}
```

`github` prints [workflow
commands](https://docs.github.com/actions/reference/workflow-commands-for-github-actions),
so that a failure appears against the file that caused it in a pull request:

```text
::notice file=docs/architecture.md::published "Architecture" to https://...
::warning file=docs/notes.md::edited in Confluence since mark published it (version 7, mark wrote 5)
::error file=docs/broken.md::unable to compile markdown: ...
::warning file=docs/old.md::page "Old" was deleted: its source file is gone
::warning::space "DOCS" was published untracked: no page mapping was saved on its manifest page "Team Handbook"
::error::unable to save page manifest: ...
```

A document that is not synchronized is left out, its warnings included: it
was skipped because it asked to be. An error that belongs to no one file, from `errors` above, is
annotated without one, and appears in the run's summary rather than against a
line of the diff.

```yaml
- run: mark publish --output-format github --files "docs/**/*.md"
```

### Writing the report to a file

`--report-file PATH` writes the report to a file instead of standard output.
Standard output then keeps what it prints without a report: the address of each
page published, or the HTML of `--compile-only` and `--dry-run`, whatever the
`--output-format`. So a CI step can read the report from the file and still
show the pages in its log, and the HTML of a compile is never mixed with a
report a script has to parse.

```bash
mark publish --output-format json --report-file report.json --files "docs/**/*.md"
jq -r '.pages[] | "\(.status) \(.file)"' report.json
```

The file is created, or emptied, as the run starts, so a report an earlier run
left there is never read as this one's; a run that fails is reported in it as
it would be on standard output. A run refused before it publishes anything,
for its settings or because no file matched, writes no report there or on
standard output, so it leaves the file empty: its exit status says why. With the `url` format the file holds the same
addresses standard output does. `-` names standard output, as leaving the flag
out does.

GitHub reads workflow commands only from what a step prints, so the `github`
format's annotations appear only once the file is printed, for example by a
later step that runs even when publishing failed:

```yaml
- run: mark publish --output-format github --report-file mark-report.txt --files "docs/**/*.md"
- if: always()
  run: cat mark-report.txt
```

A library caller sets `ReportConfig.Output` to a writer of its own to the same
effect; `PublishConfig.Output` then carries the URLs and compiled bodies.

## When Confluence is busy or unavailable

Mark tries a request up to four times, waiting longer before each retry and
for as long as a `Retry-After` header asks, up to 30 seconds. It does this for
requests Confluence throttles (429), and for reads it answers with a 5xx other
than 501, or never answers at all.

A write is different: a gateway that gives up waiting (502, 503 or 504), or a
connection that drops, does not mean Confluence did not carry the write out.
Mark therefore sends a page create, a page update, or an attachment upload
again only after it has waited as above and read that the first one did not
land. A page with the title under the same parent, or the version the update
would have made, means it did when Mark's account wrote it; so does an
attachment with the upload's checksum. Mark then goes on from there instead of
creating a duplicate. A page or version Mark cannot tell is its own, because
the account or the author could not be read, counts as somebody else's. The
same read settles a write sent again that Confluence refuses because the first
one landed in the meantime. Any other failure of a write, a 500 included,
fails the file straight away.

## Continuous Integration

It's quite trivial to integrate Mark into a CI/CD system, here is an example with [Snake CI](https://snake-ci.com/)
in case of self-hosted Bitbucket Server / Data Center.

```yaml
stages:
  - sync

Sync documentation:
  stage: sync
  only:
    branches:
      - main
  image: kovetskiy/mark
  commands:
    - for file in $(find -type f -name '*.md'); do
        echo "> Sync $file";
        mark publish -f "$file" || exit 1;
        echo;
      done
```

In this example, I'm using the `kovetskiy/mark` image for creating a job container where the
repository with documentation will be cloned to. The following command finds all `*.md` files and runs mark against them one by one:

```bash
for file in $(find -type f -name '*.md'); do
    echo "> Sync $file";
    mark publish -f "$file" || exit 1;
    echo;
done
```

Mark reads the credentials and the instance from the job's `MARK_USERNAME`,
`MARK_PASSWORD` and `MARK_BASE_URL` variables, which keeps the password off the
command line. Keep the page address out of a variable a job shares:
`MARK_PAGE_URL` sends every `mark publish` of the job to that one page, and
every `mark export` that names no other page on its command line.

The following directive tells the CI to run this particular job only if the changes are pushed into the
`main` branch. It means you can safely push your changes into feature branches without being afraid
that they have automatically shown in Confluence, then go through the reviewal process and automatically
deploy them when PR got merged.

```yaml
only:
  branches:
    - main
```

## File Globbing

Rather than running `mark` multiple times, or looping through a list of files from `find`, you can use file globbing (i.e. wildcard patterns) to match files in subdirectories. For example:

```bash
mark publish -f "helpful_cmds/*.md"
```

You can also use `**` to get all files recursively.

```bash
mark publish -f "**/docs/*.md"
```

A document is held to the same boundary as the files it names
([attachments](attachments.md#where-an-attachment-may-come-from), includes):
one named inside the directory Mark is running in has to resolve inside it,
symlinks followed. A glob such as `docs/**/*.md` follows symlinks, so a
`docs/notes.md` or a `docs/shared` directory linking out of the repository fails
that file, with `outside the project`, rather than being published; a symlink
that stays inside the repository publishes as usual. A document named by a path
outside the directory Mark is running in, such as `../docs/*.md`, is held to the
closest directory holding both, or to its own directory when they share nothing
but the filesystem's root. The same applies to a path given to the Go library's
`Publisher`.

## Linting markdown

We recommend to lint your markdown files with [markdownlint-cli2](https://github.com/DavidAnson/markdownlint-cli2) before publishing them to confluence to catch any conversion errors early.

## Preserving Inline Comments

When collaborators leave inline comments on a Confluence page, updating the page via `mark` will normally erase those comments because the stored body is fully replaced. The `--preserve-comments` flag re-attaches inline comment markers to the new page body before uploading, so existing review threads survive updates.

```bash
mark publish --preserve-comments -f docs/page.md
```

Or via environment variable:

```bash
MARK_PRESERVE_COMMENTS=true mark publish -f docs/page.md
```

**How it works:**

1. Before uploading, `mark` fetches the current page body and all inline comment markers from the Confluence API.
2. For each existing `<ac:inline-comment-marker>` tag it records the content wrapped by that marker plus a short context window immediately before the opening tag and immediately after the closing tag in the old body (not around the raw selection text, so the context is stable even when the marker wraps additional inline markup such as `<strong>`).
3. It searches the text of the new body (not its tags, attribute values, macro parameters, task statuses or code blocks) for the same selected text and picks the occurrence whose surrounding context best matches the original (using Levenshtein distance), so the marker lands in the right place even if nearby text has shifted.
4. The updated body—with all markers re-embedded—is then uploaded as normal.

**Limitations:**

* If the commented text was deleted from the document, the inline comment cannot be relocated and will be lost. `mark` logs a warning in this case.
* Overlapping selections (two comments anchored to the same stretch of text) are detected; the earlier overlapping match is dropped with a warning, and the later one (higher byte offset) is kept, rather than producing malformed markup.
* `--preserve-comments` is automatically skipped for newly created pages (there are no comments to preserve yet).
* When combined with `--changes-only`, the comment-preservation API calls are skipped entirely on runs where the page content has not changed, avoiding unnecessary round-trips.

## Leaving hand-edited pages alone

By default Mark overwrites whatever a page currently holds. `--no-overwrite`
makes it skip any page that has been edited in Confluence since Mark last
published it:

```bash
mark publish --track-pages --no-overwrite --files "docs/**/*.md"
```

```text
WRN page "Runbook" was edited in Confluence since mark published it
    (version 9, mark wrote 7); leaving it alone
```

It needs `--track-pages`, because the version Mark last wrote is remembered in
the same manifest, and Mark refuses to run without it rather than appear to
guard pages it is not guarding.

A skipped page still counts as published: it is not reported as an orphan and
not pruned. Nothing else about it is touched either -- no labels, no ordering,
and its attachments are not re-uploaded.

The warning repeats on every run until the difference is resolved, which is
deliberate: a page drifting from its source is a standing problem, not a one-off
event. To resolve it, either bring the edit back into the Markdown, or publish
once without `--no-overwrite` to let Mark reclaim the page.

Comparison is by version number, not by content. Confluence rewrites storage
markup on save often enough that comparing bodies would report a difference on
pages nobody had touched.

### Worth knowing about --no-overwrite

* A page Mark has no recorded version for -- first run, or one published before
  the version was tracked -- is published normally and recorded, rather than
  frozen on the suspicion that it might have changed.
* Anything that creates a version counts as an edit, including a comment added
  in the web UI.
* An edit that lands while Mark is publishing the page -- after the check,
  before the update -- makes the update fail rather than be retried over it.
  Without the flag, Mark retries such an update once against the page's
  current version, which overwrites the edit.
