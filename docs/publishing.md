# Publishing

## A header on every page

`--page-header` names a template that mark places at the top of every page it
publishes, inside the page's layout. The documents themselves don't change. The
usual use is a warning that the page is generated, with a link to the file to
edit:

```bash
mark --page-header generated.md --files "docs/**/*.md"
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

With `--page-id`, `.Title` and `.Space` come from the page being updated and
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
only with the default `url` output format: with `json` or `github` the report
is all that is written, so a CI step can parse it. A page `--no-overwrite`
would leave alone is `skipped`, as it is on a real run, and is not printed
either. A real run's report only tells a content update apart, so a page
previewed as `would-update` only for a move, its labels or an attachment is
reported `unchanged` by the real run that makes that change.

Not compared, so a page differing only in them is `unchanged` although a real
run still applies them: [content properties](metadata.md#confluence-content-properties),
the `--edit-lock` restriction, and the `Order` of a page among its siblings.
The restrictions a document declares are compared: a page whose restrictions
would change is `would-update`.

`orphans` lists the [tracked pages whose source file is
gone](page-tree.md#removing-pages-whose-files-are-gone), under `--track-pages`, with the
`--on-orphan` action taken: `report` for a page that was only reported and left
where it is, `archive` or `delete` for one that was archived or trashed.

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
::warning file=docs/draft.md::the document is not synchronized
::error file=docs/broken.md::unable to compile markdown: ...
::warning file=docs/old.md::page "Old" was deleted: its source file is gone
::error::unable to save page manifest: ...
```

An error that belongs to no one file, from `errors` above, is annotated without
one, and appears in the run's summary rather than against a line of the diff.

```yaml
- run: mark publish --output-format github --files "docs/**/*.md"
```

## When Confluence is busy or unavailable

Mark tries a request up to four times, waiting longer before each retry and
for as long as a `Retry-After` header asks, up to 30 seconds. It does this for
requests Confluence throttles (429), and for reads it answers with a 5xx or
never answers at all.

A write is different: a gateway that gives up waiting (502, 503 or 504), or a
connection that drops, does not mean Confluence did not carry the write out.
Mark therefore sends a page create, a page update, or an attachment upload
again only after it has read that the first one did not land. A page with the
title, the version the update would have made, or an attachment with the
upload's checksum means it did, and Mark goes on from there instead of
creating a duplicate. Any other failure of a write, a 500 included, fails the
file straight away.

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
        mark publish -u $MARK_USER -p $MARK_PASS -b $MARK_URL -f $file || exit 1;
        echo;
      done
```

In this example, I'm using the `kovetskiy/mark` image for creating a job container where the
repository with documentation will be cloned to. The following command finds all `*.md` files and runs mark against them one by one:

```bash
for file in $(find -type f -name '*.md'); do
    echo "> Sync $file";
    mark publish -u $MARK_USER -p $MARK_PASS -b $MARK_URL -f $file || exit 1;
    echo;
done
```

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
3. It searches the new body for the same selected text and picks the occurrence whose surrounding context best matches the original (using Levenshtein distance), so the marker lands in the right place even if nearby text has shifted.
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
