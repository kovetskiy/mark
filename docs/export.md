# Exporting a page

`mark export` goes the other way: it reads a page from Confluence and writes it
as a Markdown document that `mark publish` publishes back to the same page.
Use it to move a page that was written in Confluence into a repository, or to
see how mark would write something.

> [!WARNING]
> `mark export` is experimental. The Markdown it writes, and its flags, may
> change between releases. Check an export before you commit it or publish it
> back.

```bash
# By id, into a file; the attachments the page shows are written beside it.
mark export -b https://example.atlassian.net/wiki --page-id 123456 -o docs/release.md

# By the address a browser shows, which also gives the base URL.
mark export --url https://example.atlassian.net/wiki/spaces/DOCS/pages/123456/Release -o docs/release.md

# By space and title, to standard output.
mark export -b https://confluence.example.com --space DOCS --title "Release process"
```

A page is named one way: `--page-id`, `--url`, or `--space` with `--title`.
A page named on the command line sets aside a `MARK_PAGE_ID`, `MARK_PAGE_URL`
or `MARK_TITLE`, and a `page-id`, `url` or `title` in the configuration file,
so a job that sets `MARK_PAGE_URL` for publishing can still export another
page. `--space` on the command line sets aside a page id or URL from them too,
since those ignore the space; a title from them is looked up in that space.
A URL set aside still gives the base URL when none is set otherwise.
`--url` understands `/spaces/KEY/pages/ID/...` and
`/spaces/KEY/blog/.../ID/...` on Cloud, and `/display/KEY/Title` and
`/pages/viewpage.action?pageId=ID` on Server and Data Center, and a short
`/x/...` link on either, which carries the page id. `--space` is the same setting publish uses, so a
configuration file that names a space for publishing serves `--title` too.
`--url` is publish's setting too, `MARK_PAGE_URL` and `url` in the
configuration file included, so a file that names a page for export sends
publish to it as well. `--url` also gives the base URL. A base URL set by
`--base-url`, `MARK_BASE_URL` or the configuration file is used instead when
the URL is on that instance; a URL on another one is refused, since a page id
names a page of one instance only.

A URL is on the configured instance when its scheme, host and port are the
configured ones -- `http` is not `https`, case and a trailing dot in the host
do not count, and a port left out is the scheme's default -- and its context
path, such as `/wiki`, is the same, or one of the two leaves it out. With the
`api.atlassian.com` gateway configured, any Cloud site's `https` URL is on it;
with a site configured, a gateway URL is not. A URL carrying a user name or
password is refused. `mark page` holds a page URL to `--base-url` the same
way, and a Go program can make the same check with `confluence.SameInstance`
before it sends credentials anywhere a URL names.

The document starts with the headers that name the page, followed by its body:

```markdown
<!-- Space: DOCS -->
<!-- Parent: Guides -->
<!-- Parent: Setup -->
<!-- Title: Release process -->
<!-- Label: how-to -->
<!-- Content-Appearance: default -->
<!-- Emoji: 🚀 -->

# Release process
...
```

`Parent` headers are the page's ancestry below the space's home page, where mark
starts when it resolves them. A page in a folder on Cloud gets a `Folder`
header for each folder directly above it, beneath its `Parent` headers, and a
folder right below the home page gets the home page as its `Parent`. A folder
higher up, with a page between it and the exported one, is left out with a
warning: mark's headers put folders beneath parents, not above them. A blog
post gets `Type: blogpost` instead.
`Content-Appearance` is left out for a full-width page, which is what mark
publishes by default. A page Confluence keeps as `fixed`, an older editor's
narrow layout, is written as `default`, the narrow layout mark publishes.
`Emoji` is there when the page has a title emoji.
Labels are written in alphabetical order.

The attachments the body shows or links to are downloaded into
`--attachments-dir`, which defaults to the directory of `--output` (or the
working directory when the document goes to standard output). Attachments the
body does not refer to are not downloaded. Keep them beside the document if you
mean to publish it back: mark names an attachment after its path, so
`--attachments-dir files` turns `chart.png` into `files_chart.png` on the next
publish, a new attachment next to the old one. The export fails before writing
anything if a file it would write is already there, unless `--overwrite` is
given, and `--no-attachments` writes the document alone, without asking
Confluence for the page's attachments. The attachments are
moved into place only once all of them have downloaded, and a download that is
not the attachment fails the export with nothing written: a login page in its
place, which an HTML answer for a file Confluence records as another type is
taken to be wherever it came from, or an answer of another size than
Confluence records. Two whose names would
share a file -- `a/b.png` and `a_b.png`, or names that differ only in case --
are told apart by a number, as `a_b-2.png`, and so is one named like the
document itself, when `--output` names a file in the same directory: a
`notes.txt` attachment written beside `-o notes.txt` becomes `notes-2.txt`.
With the document on standard output there is no file of its name, and an
attachment called `notes.txt` keeps that name. An export usually goes into a
git working tree, so an attachment is never written under a name git keeps
for itself in any directory -- `.git`, `.gitattributes`, `.gitignore`,
`.gitmodules` and `HEAD`, in any case, with any trailing dots or spaces, or
with the invisible characters HFS+ ignores -- and gets
a number instead, as `.git-2`; published back, it is uploaded under that name.
Page titles never come out as such names, since a page's directory or
concept name has only letters, digits and dashes.

An attachment is also written under another name where publishing the export
back would read its own as something other than that one file:

* A name ending in `.md`, in any case, gets `.txt` after it, `notes.md`
  becoming `notes.md.txt`. The documents are then the only `.md` files an
  export writes, so a glob such as `docs/**/*.md` publishes the pages and
  nothing else; an attachment under its own name would be published as a
  document, with whatever headers it holds.
* `*`, `?`, `[`, `]`, `{` and `}` are each written as `_`, `report[2024].pdf`
  becoming `report_2024_.pdf`. An `Attachment` header holding one of them is
  [a pattern](attachments.md#declaring-attachments), which can match other files beside the document.

The document links to and declares the name written, and published back the
attachment is uploaded under it, a new attachment next to the one the page
had.

An attachment the page refers to
but no longer has is reported, and left out of the `Attachment` headers;
with `--no-attachments` there is no telling, so every one is declared.

Most filesystems take a file name of at most 255 bytes, and Confluence allows
longer attachment names. An attachment whose name would be longer, with the
number or the `.txt` after `.md` added, is written under a shorter one:
the part before the extension is cut on a character's boundary, and a name
too long for a file also gets a dash and eight hexadecimal digits from a hash
of its name in Confluence, so that two names that only differ past the cut
keep files of their own, whichever is exported first. The document links to
and declares the name written. Published back, the attachment is uploaded
under that shorter name, a new attachment next to the one with the long name,
as with `--attachments-dir`.

## Front matter instead of headers

`--metadata frontmatter` writes the same metadata as YAML front matter, under
the plural keys mark reads there (see [YAML front matter](metadata.md#yaml-front-matter)):

```markdown
---
space: DOCS
parents:
  - Guides
  - Setup
title: Release process
labels:
  - how-to
content-appearance: default
emoji: "🚀"
---

# Release process
...
```

mark reads front matter only with the `frontmatter` feature, and `--features`
replaces the default features rather than adding to them, so publish such a
document back with `--features=mermaid,mention,frontmatter`. Without the
feature mark does not read the metadata at all: the publish fails, since the
document has no metadata mark reads, or, with a space and title given some
other way, puts the YAML on the page as text and the page beneath the space's
home page.

Front matter can also hold an article layout's sidebar that has `-->` in it,
in a code block for instance, which a comment header cannot: under
`--metadata comments` such a layout stays in the body as storage format.
`--metadata comments`, the default, writes the comment headers. `--format okf`
writes front matter of its own and ignores `--metadata`.

## The body alone

`--metadata none` writes the body without any metadata, for a program that
takes the title and labels from elsewhere. Nothing in such a document names
its page, so `mark publish` cannot publish it back by itself: give it the
page some other way, with `--url`, or with `--space` and a title, which
`--title-from-h1` finds only if the body starts with the title as a heading.
With no `Label` headers, publishing it removes the page's
labels, unless `--append-labels` is given (see
[Labels applied in Confluence](metadata.md#labels-applied-in-confluence)).
The width goes the same way: with no `Content-Appearance` header, the page
takes `--content-appearance`, full width when that is unset.
An article layout's sidebar, which would travel in the
metadata, stays in the body, written as
[layout comments](formatting.md#customizing-the-page-layout). `--recursive`
refuses `--metadata none` given on its command line for mark's documents,
since a tree is exported to be published back where it was, and writes the
comment headers when `MARK_METADATA` or the configuration file says `none`,
which it takes for a single page's setting, with a warning saying so; with
`--format okf` it is ignored, as any `--metadata` is. A body that opens with
a horizontal rule writes it as `***`, since a document opening with `---` is
read as front matter.

A Go program calling `mark.Export` gets the title, the labels and the
document as values on the result, `Title`, `Labels` and `Markdown`, whatever
`Metadata` says; with `export.MetadataNone` and `NoAttachments`, and neither
`Output` nor `Stdout` set, nothing is written anywhere. Leave out
`NoAttachments` and the attachments the body refers to are still written: with
no `Output`, into `AttachmentsDir`. A page with attachments to write is refused
with `export.ErrNoAttachmentsDir`, having written nothing, when that is empty
too; set it to `"."` for the working directory, as the command line does. A
page with none to write needs neither:

```go
result, err := mark.Export(ctx, mark.ExportConfig{
    Connection:    mark.ConnectionConfig{BaseURL: base, Username: user, Password: token},
    PageID:        "123456",
    Metadata:      export.MetadataNone,
    NoAttachments: true,
})
// result.Title, result.Labels, result.Markdown
```

A label is a name and nothing more to mark: one written `team=docs` is
exported, and published, as that one name. Splitting it at `=` is the
caller's convention, not mark's. The body still refers to its attachments by
name, as it would beside them; `NoAttachments` leaves them in Confluence,
where `confluence.API.GetAttachments` and `DownloadAttachment`, which writes
to any `io.Writer`, read them.

## Exporting a tree of pages, or a whole space

`--recursive` exports the page and every page beneath it into the directory
`--output` names, and `--depth` stops it that many levels down, counting the
page itself. With `--space` and no page, it exports the whole space:

```bash
mark export --url https://example.atlassian.net/wiki/spaces/DOCS/pages/123456/Guides \
     --recursive -o docs/guides
mark publish --files "docs/guides/**/*.md" --attach-referenced

mark export -b https://example.atlassian.net/wiki --space DOCS --recursive -o docs
```

```text
docs/guides/index.md                  Guides
docs/guides/setup/index.md            Guides > Setup
docs/guides/setup/image.png           an attachment Setup shows
docs/guides/setup/tuning/index.md     Guides > Setup > Tuning
```

Every page gets a directory of its own, named after its title in lower case
with a dash for anything that is not a letter or digit, cut to 200 bytes so
that a long title still makes a name the file system takes, and its
attachments go beside it there. The directories of the pages beneath it are
there too, so an attachment named like one of them gets a number, as one
named like the document does: `setup` beside a page titled Setup is written
as `setup-2`. The `index.md` files are the tree's documents and its only
`.md` files, which is what lets `**/*.md` name them. A directory of its own is what keeps two pages' `image.png`
apart: mark names an attachment after its path, so two pages sharing a
directory would share their attachments' names too. A title that comes out like
a sibling's, as `C++` and `C` do, or two long titles alike up to the cut, has
the page id added to it.

A whole space puts its home page at `index.md` and the home page's children
in directories beside it. A page at the top of the space other than the home
page gets a directory there too, with a warning: mark publishes every page
below the home page, so that is where it goes when it is published back.

Each document carries the `Parent` headers of the page it came from, so it is
published back where it was whatever directory it is in, without
`--parents-from-path`.

Links between the pages stay links to their titles, `[Setup](ac:Setup)`, which
resolve once they are published back but that a repository host cannot follow.
`--relative-links` writes a link to another page of the export as a link to
its file instead, `[Setup](setup/index.md)`, so the directory can be browsed
where it is kept. So does a URL of such a page pasted into a body: a short
`/x/...` link, which is how mark publishes a relative link, or the address a
browser shows for reading the page, on the same instance and without a `#`
fragment or a query other than `pageId`, such as a focused comment or an old
version; one that shows its own address is written with the page's title. The
editor, page history and the like, and a dated `/display/` address, which names
a blog post, keep their URL. A link to a page outside the export stays `ac:`,
or the URL it was. The price is
on the way back: mark publishes a relative link as a link to the page's
address rather than as Confluence's own page link, so the storage format is no
longer the one the export started from. The link still goes to the same page,
and survives the page being renamed or moved, but it is why the option is
off by default.

The whole tree is read, and every document's path checked, before anything is
written: a document already there stops the export unless `--overwrite` is
given, and so does a folder anywhere in the tree, since the export does not
list a folder's pages. Content that is not a page, such as a whiteboard, is skipped with a
warning. `--attachments-dir` does not apply: on the command line it is an
error, and from `MARK_ATTACHMENTS_DIR` or the configuration file it is
ignored. The Markdown cannot go to standard output.

## What becomes Markdown

* headings, paragraphs, emphasis, strong, strikethrough, inline code, line
  breaks, rules, blockquotes, and lists, nested, ordered (with their start
  number) and unordered;
* task lists, as `- [ ]` and `- [x]`;
* links, and autolinks where the text is the address;
* links to another page in the same space as `[text](ac:Title)`, to a heading
  on the page as `[text](#anchor)`, and to an attachment of the page as
  `[text](file.pdf)`;
* images, attached (downloaded, and shown from the file) or by URL, as
  `![alt](path)`, or as `<img src width>` when the page shows them at a size of
  their own;
* tables with a header row and one line of inline content in each cell, as GFM
  tables, with the column alignment, when no cell has a colour or an alignment
  of its own;
* code macros, as fenced code blocks whose info string carries the language,
  `collapse`, the theme, line numbers, the first line and the title;
* info, tip, note and warning macros with the title mark gives a GitHub alert,
  as that alert (see [GitHub Alerts Support](formatting.md#github-alerts-support));
* expand macros, as `<details>` with a `<summary>`;
* a page laid out in sections, with the [layout markers](formatting.md#customizing-the-page-layout),
  and the article layout, as `Layout` and `Sidebar` headers;
* an anchor mark put into a heading for a link on the page, which mark puts back,
  as nothing, or as a `{#id}` custom id.

## What stays storage format

Everything else is written as the storage format it is, which mark publishes as
it was: other macros (a table of contents, Jira, status, anchors), user
mentions, emoticons, dates, links to pages in other spaces, tables with merged
cells or with lists or macros in them, headings and cells with an alignment or
a colour, `<pre>` blocks, and HTML that Markdown has no syntax for (`<u>`,
`<sub>`, a coloured `<span>`), and a blockquote that opens with a
[colon marker](formatting.md#colon-markers), which would come back as a macro.
An info, tip, note or warning macro
without mark's title -- the way Confluence's editor inserts them -- keeps its
tags, with its body in Markdown between them, since as an alert it would come
back with a title it did not have. A newline inside kept storage format is
written as `&#10;`, and the body of a macro such as `noformat` as one CDATA
section per line, because a blank line would end the Markdown block holding
them. An attachment that storage format refers to is named in an
`Attachment` header, since mark uploads images and linked files by itself but
does not look inside markup.

## Publishing an export back

Publishing an export of a page mark published gives the page it came from; the
tests do exactly that with mark's own fixtures. What does not come back the
same:

* A link to an attachment is published as a link to the attachment only with
  `--attach-referenced`; without it, it is published as a link to the path.
* A diagram mark drew from `mermaid`, `d2` or `plantuml` comes back as the image
  mark attached, not as the diagram's source. A code macro in one of those
  languages stays a macro, so that it is not drawn on the next publish. So does
  one whose language publishing would rename, such as `sh` (see
  [Language names](formatting.md#language-names)).
* An image loses its alignment and placement (`ac:align`, `ac:layout`), and a
  code macro the parameters its info string cannot say (Cloud's breakout
  width).
* Inline comment markers are left out: the comments belong to Confluence, and
  `--preserve-comments` puts them back on publish. So are XML comments.
* A page not below its space's home page is published below the home page, and
  one below a folder with a page between them is published below that page
  without naming the folder.
* Inline content that stands without a paragraph around it, next to other
  content, comes back inside one, and a paragraph in a table cell comes back as
  the cell's text.

Text on the page that reads like one of the HTML comments mark acts on --
`<!-- Include: ... -->`, `<!-- Macro: ... -->`, a header such as
`<!-- Title: ... -->`, `<!-- ac:ignore -->` -- comes back as that text: its
`<!` is written as `<\!`, and in a CDATA section `<!--` is split across two
sections. A code macro whose title holds `<!--` stays a macro, as does a
heading's anchor named with it. Code itself is
written as it is, since mark leaves code alone.

## Open Knowledge Format

`--format okf` writes the page as a concept of the
[Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
(OKF) 0.2, for agents and knowledge catalogs to read, instead of a document mark
publishes back. mark does not publish an OKF export back.

```shell
mark export -b https://example.atlassian.net/wiki --page-id 1234 --format okf -o setup.md
mark export -b https://example.atlassian.net/wiki --space DOCS --recursive --format okf -o docs-bundle
```

The body is the same Markdown. In place of mark's headers it has OKF front
matter:

```yaml
---
type: Confluence Page              # or Confluence Blog Post
title: Setup
resource: https://example.atlassian.net/wiki/spaces/DOCS/pages/1234/Setup
tags:                              # the page's labels
  - install
generated:
  by: mark/v17.0.0                 # what wrote the file
sources:
  - id: confluence
    resource: https://example.atlassian.net/wiki/spaces/DOCS/pages/1234/Setup
    title: Setup
    author: human:jdoe             # who last edited it: the username, or the Cloud account id; process:<account id> for a Cloud app
    last_modified: "2026-05-01T10:30:00Z"
---
```

`generated` names mark, which wrote the file, and has no `at`: the page was
written by its author, whose last edit is the source's `author` and
`last_modified`, and an export time would make every export of an unchanged
page differ. A link to another page, in this space or another, is written as a
link to its address whatever its text, and without an anchor, since Confluence
spells one in an address its own way; one to a page that is not there stays as
it was, and one mark cannot look up fails the export. A link to an anchor on
the page itself is written as `#anchor`. Each page linked this way costs one
lookup, the first time it is linked. The sidebar of an article layout stays in
the body, between layout markers, rather than going in a header.
An edit a Cloud app made, such as Automation for Confluence's, has the author
`process:<account id>`, since OKF's consumers trust `human:` as a person's
work; through the `api.atlassian.com` gateway Confluence does not say which
accounts are apps, so there every author is `human:`.
`-o index.md` and `-o log.md` are refused, since OKF reserves those names.

With `--recursive` the directory is an OKF bundle:

```text
docs-bundle/
  index.md                  # okf_version: "0.2", and the top-level pages
  home.md
  home/
    index.md                # the pages beneath Home
    setup.md
    setup/
      index.md
      linux.md
  references/
    attachments/
      1234/                 # each page's attachments, by page id
        diagram.png
```

* Each page is a concept named after its title, as a directory is above, with
  the pages beneath it in a directory of the same name beside it. A whole
  space has no document of its own for the home page, so the home page is a
  concept among the other top-level pages, where Confluence lists it.
* Every directory has an `index.md` listing its concepts and the directories
  below it, in the order Confluence lists the pages. The bundle's own declares
  the OKF version, and is headed with the space key, or the title of the page
  the export starts from.
* A page whose title would come out as `index` or `log`, which OKF reserves, or
  as `references` at the top of the bundle, has the page id added to its name.
  A page id that is not a number, which would end up in a path, fails the
  export.
* Links between the pages are relative links, as `--relative-links` writes
  them, which `--format okf` does without it, anchors included.
* An attachment referred to from storage format the export keeps as it is,
  such as a macro's, is named by its path in the bundle.
* An attachment whose name ends in `.md` is written with `.txt` after it,
  so that it is not read as a concept.
