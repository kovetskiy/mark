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
`--url` understands `/spaces/KEY/pages/ID/...` and
`/spaces/KEY/blog/.../ID/...` on Cloud, and `/display/KEY/Title` and
`/pages/viewpage.action?pageId=ID` on Server and Data Center; a short `/x/...`
link has to be opened first. `--space` is the same setting publish uses, so a
configuration file that names a space for publishing serves `--title` too.

The document starts with the headers that name the page, followed by its body:

```markdown
<!-- Space: DOCS -->
<!-- Parent: Guides -->
<!-- Parent: Setup -->
<!-- Title: Release process -->
<!-- Label: how-to -->
<!-- Content-Appearance: fixed -->
<!-- Emoji: 🚀 -->

# Release process
...
```

`Parent` headers are the page's ancestry below the space's home page, where mark
starts when it resolves them. A blog post gets `Type: blogpost` instead.
`Content-Appearance` is left out for a full-width page, which is what mark
publishes by default, and `Emoji` is there when the page has a title emoji.
Labels are written in alphabetical order.

The attachments the body shows or links to are downloaded into
`--attachments-dir`, which defaults to the directory of `--output` (or the
working directory when the document goes to standard output). Attachments the
body does not refer to are not downloaded. Keep them beside the document if you
mean to publish it back: mark names an attachment after its path, so
`--attachments-dir files` turns `chart.png` into `files_chart.png` on the next
publish, a new attachment next to the old one. The export fails before writing
anything if a file it would write is already there, unless `--overwrite` is
given, and `--no-attachments` writes the document alone. The attachments are
moved into place only once all of them have downloaded. Two whose names would
share a file -- `a/b.png` and `a_b.png`, or names that differ only in case --
are told apart by a number, as `a_b-2.png`. An attachment the page refers to
but no longer has is reported, and left out of the `Attachment` headers.

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
`<sub>`, a coloured `<span>`). An info, tip, note or warning macro
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
  languages stays a macro, so that it is not drawn on the next publish.
* An image loses its alignment and placement (`ac:align`, `ac:layout`), and a
  code macro the parameters its info string cannot say (Cloud's breakout
  width).
* Inline comment markers are left out: the comments belong to Confluence, and
  `--preserve-comments` puts them back on publish. So are XML comments.
* A page in a folder, or not below its space's home page, is published below the
  home page.
* Inline content that stands without a paragraph around it, next to other
  content, comes back inside one, and a paragraph in a table cell comes back as
  the cell's text.
