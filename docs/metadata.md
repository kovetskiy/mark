# Page metadata

Mark uses an extended file format, which, still being valid markdown,
contains several HTML-ish metadata headers, which can be used to locate page inside
Confluence instance and update it accordingly.

File metadata can be written as YAML front matter when the `frontmatter`
feature is enabled. Because specifying `--features` replaces the defaults,
include the default features explicitly if needed:

```shell
mark publish --features=mermaid --features=mention --features=frontmatter
```

To switch off every optional feature, mermaid and mention included, use
`--features=none`, `MARK_FEATURES=none` or `features = ["none"]`. An empty
value is refused rather than read as the defaults.

## YAML front matter

YAML front matter uses the following format:

```markdown
---
space: <space key>
parents:
  - <parent 1>
  - <parent 2>
folders:
  - <folder 1>
  - <folder 2>
title: <title>
attachments:
  - <local path>
labels:
  - <label 1>
  - <label 2>
restrictions:
  read:
    users:
      - <user name>
    groups:
      - <group name>
  update:
    users:
      - <user name>
    groups:
      - <group name>
image-align: <left|center|right>
order: <whole number>
---

<page contents>
```

Front matter has to open on the document's first line, with three or more
dashes and nothing else, and closes at the next line of the same dashes. A
document whose opening delimiter is never closed is refused rather than
published with its metadata as text.

A list key given a single value, as in `labels: release`, reads it as a list
of one. A value that is neither a string nor a list of strings is ignored, and
warned about.

Note that the front matter keys are plural where the HTML headers are singular:
`attachments`, `parents`, `folders`, `labels` and `properties`. A key mark does
not read is ignored, and warned about:

```console
WARNING doc.md: front matter key "attachment" is not read by mark and was ignored
```

Front matter shared with another tool -- a static site generator's `date` and
`draft` in the same block -- is reported the same way, since mark cannot tell a
key meant for something else from one meant for mark and misspelled.

## HTML headers

The same metadata can be written as HTML comments at the top of the document:

```markdown
<!-- Space: <space key> -->
<!-- Parent: <parent 1> -->
<!-- Parent: <parent 2> -->
<!-- Folder: <folder 1> -->
<!-- Folder: <folder 2> -->
<!-- Title: <title> -->
<!-- Attachment: <local path> -->
<!-- Label: <label 1> -->
<!-- Label: <label 2> -->
<!-- Property: <key>=<value> -->
<!-- Restriction: <read|update>.<user|group>=<name> -->
<!-- Synchronized: <true|false> -->
<!-- Image-Align: <left|center|right> -->

<page contents>
```

When both formats are present, HTML headers override matching scalar front
matter values. Repeatable parents, folders, attachments, and labels are
appended to the values from front matter.

Headers are read from the run of comments the document opens with, and only
from there. Blank lines between them are fine, so headers may be grouped —
identity, then labels, then properties — but anything else ends the run, and a
header written below the page's own text is published as text rather than
applied. Mark warns when it finds one there.

Only a comment naming one of the headers on this page is read as one. A comment that
names something else — `<!-- TODO: ... -->`, `<!-- Author: ... -->`, an SPDX
line, an editor modeline — is an ordinary comment: it stays in the page and the
headers around it still apply. So do the `ac:` markers, which are storage-format
markup rather than metadata, and a `Macro` definition, which belongs to the
macro expander; the layout markers may therefore follow `<!-- Layout: plain -->`
directly, as [Customizing the page
layout](formatting.md#customizing-the-page-layout) shows.

A comment that is *nearly* a header is an error: `<!-- Titel: ... -->` fails the
run naming the header it was probably meant to be. The line is taken out of the
page either way, so publishing the document without its title and exiting zero
was the alternative.

An [`Include`](templates.md#includes) directive may be written among the headers or anywhere below them.
It is left in the page for the include expander to find.

There can be any number of `Parent` headers, if Mark can't find specified
parent by title, Mark creates it.

## Further headers

These headers are read as well:

```markdown
<!-- Layout: (article|plain) -->
```

* (default) article: content will be put in narrow column for ease of
  reading;
* plain: content will fill all page;

```markdown
<!-- Type: (page|blogpost) -->
```

* (default) page: normal Confluence page - defaults to this if omitted
* blogpost: [Blog post](https://confluence.atlassian.com/doc/blog-posts-834222533.html) in `Space`.  Cannot have `Parent`(s)

```markdown
<!-- Content-Appearance: (full-width|fixed|default|max) -->
```

The Confluence Cloud editor offers three widths — Narrow, Wide and Max — which
map onto these values as `default`, `full-width` and `max`.

* (default) full-width: content will fill the full page width. This is the Confluence UI's **Wide**.
* fixed: content will be rendered in a fixed narrow view
* default: sets the Confluence property value to `"default"`, which is the Confluence UI's **Narrow** layout. Note: `fixed` maps to a different Confluence property value and can cause misaligned page title and body content — use `default` instead for the narrow layout.
* max: the Confluence UI's **Max** layout, which is wider than `full-width`.

```markdown
<!-- ac:ignore -->
content that stays out of Confluence
<!-- ac:ignore end -->
```

Everything between the two markers is left out of the published page, along with
the markers themselves. It is for content that reads well in one place and badly
in the other -- a table of contents that Confluence builds for itself, or a
plain-text stand-in for a macro:

```markdown
<!-- Include: ac:profile
     Name: Doe, John -->
<!-- ac:ignore -->
John Doe's profile
<!-- ac:ignore end -->
```

Read on GitHub the file shows the name; published to Confluence it shows the
profile macro. Included files may mark regions of their own, which is stripped
as each file is read. Attachments referenced only inside an ignored region are
not uploaded, since nothing on the page would point at them. A marker without its
pair is an error rather than a guess -- quietly publishing half a page is worse
than refusing.

```markdown
<!-- Order: <number> -->
```

Positions the page among its siblings, smaller numbers first. Pages that do not
declare an order are left exactly where Confluence has them, so annotating one
page does not disturb the rest.

Only pages published in the same run are arranged, and only relative to each
other -- a run narrowed with `--files` knows nothing about the other children of
those parents and will not rearrange them. Nothing is moved that is already in
the right relative order, so a run that changes nothing performs no moves at
all.

Note that giving Confluence explicit positions takes that branch of the tree out
of its default alphabetical ordering, which is inherent to asking for a
particular order.

> [!NOTE]
> The content move endpoint mark uses for ordering and reparenting is served
> only by Confluence Cloud. On Server and Data Center mark moves pages through
> `/pages/movepage.action`, the action behind the Move dialog, which needs the
> credentials to be accepted outside `/rest`. Where it is not, a page moved to a
> different *parent* is moved with an update carrying the new ancestor instead,
> and a page that has to be repositioned among its siblings fails the run.

```markdown
<!-- Sidebar: <h2>Test</h2> -->
```

Setting the sidebar creates a column on the right side.  You're able to add any valid HTML content. Adding this property sets the layout to `article`.

```markdown
<!-- Emoji: 🚀 -->
```

You can set a page emoji icon by specifying the icon in the headers.

```markdown
<!-- Image-Align: center -->
```

You can set the alignment for all images in the page. Common values are `left`, `center`, and `right`. Can also be set globally via the `--image-align` CLI option (per-page header takes precedence).

**Note**: Images with width >= 760px automatically use `center` instead of the configured alignment, as Confluence requires this for wide images.

## Restrictions

The optional `restrictions` mapping controls who may read or update the page.
An operation it names gets exactly the users and groups listed; an operation it
leaves out keeps whatever restrictions the page has, including ones set in the
Confluence UI. An empty operation (`read: {}`, or `read:` with no value)
removes its restrictions. A single name may be written without a list. A key
written twice in ways mark reads as one, such as `users` and `Users`, is
refused rather than guessed at. Mark
adds its authenticated user to each operation that names anyone, and refuses to
apply one if it cannot identify that user, so publishing cannot lock it out of
the page.

How a user is named depends on the platform. On Server and Data Center it is
the username. On Cloud it is the full name, which has to match exactly one
user; write `accountid:<id>` for someone whose name another user shares, or
mark's own login or email for itself. A name that matches nobody, or more than
one person, fails the run -- and a dry run -- before any page is created.

Restrictions have no dedicated feature flag. YAML metadata still requires the
`frontmatter` feature, and a document whose front matter has `restrictions`
is refused without it, rather than published unrestricted; legacy restriction
headers work either way.

Repeat `Restriction` headers to add users and groups to the operation assembled
from the document:

```markdown
<!-- Restriction: read.user=jdoe -->
<!-- Restriction: read.group=docs-readers -->
<!-- Restriction: update.user=jdoe -->
<!-- Restriction: update.group=docs-editors -->
```

The singular headers append to any restrictions read from YAML front matter.
Anyone named under `update` must also be able to read the page. Once
`Restriction` is a header, a comment among the headers that reads like one
(`<!-- Restriction: internal -->`) is an error rather than an ordinary comment:
a restriction mark could not read would otherwise leave the page open.

Mark reads the page's restrictions and changes them only when they differ
from what the document declares, so an unchanged page is not written. A change
is made in two steps around the content update: first the page is restricted
to whoever both its old and its new restrictions allow, then, once the new
content is up, to exactly what the document declares. New content is never
shown to someone it is meant to be kept from, old content stays restricted
until it is replaced, and a run that fails in between leaves the page no more
open than either. A restriction-only change is still made under
`--changes-only`, and counts as a change in the run's report.

With `--edit-lock`, the edit-lock user stays among those who may update the
page throughout, so the document's restrictions never lift the lock, even
briefly; users and groups the document names under `update` are kept too.

## Automatic Page Title

If you don't want to specify the page title in the metadata of each file, `mark` provides two ways to set it automatically.

### From the first H1 heading

You can use the `--title-from-h1` flag to extract the page title from the first H1 heading in the markdown file. If no H1 heading is found, the title must be set in the page metadata.

### From the filename

You can use the `--title-from-filename` flag to use the filename (without the extension) as the page title. `mark` will automatically convert the filename to a more readable title by:

* Replacing underscores (`_`) and dashes (`-`) with spaces.
* Applying title case to the filename.

For example, a file named `my_awesome-page.md` will have the title "My Awesome Page".

These two options are mutually exclusive. If both flags are provided, `mark` will produce an error.

### Title length

Confluence takes a title of at most 255 characters, counted as UTF-16 code
units, so an emoji counts as two. Mark checks the page title, however it was
set, and the titles of the parents and folders it would create, after
`--title-append-generated-hash` has added its hash. A document with a longer one
fails the run, and a dry run, before anything is created, rather than
partway through, once its parents exist.

## Leaving a document unpublished

A document can ask to be left out of a run:

```markdown
<!-- Synchronized: false -->
```

or, in YAML front matter:

```yaml
---
title: Runbook
synchronized: false
---
```

Mark skips the file before it asks Confluence anything, so a document that has
opted out costs no page lookup and uploads no attachments. Saying nothing means
the document is published, which is the ordinary case; opting out has to be
deliberate.

A page that was published before is left exactly as it is -- Mark does not
delete it, and does not report it as having lost its source file. Setting
`Synchronized: true` again, or removing the header, resumes publishing to the
same page.

## Confluence content properties

A page can carry arbitrary key/value data that macros, reports and scripts read
back through the API. One `Property` header sets one of them, as many times as
you like:

```markdown
<!-- Property: owner=platform-team -->
<!-- Property: reviewed=2026-08 -->
```

In YAML front matter the same thing is a mapping under the plural key, the way
`Parent` headers become a `parents` list:

```yaml
---
title: Runbook
properties:
  owner: platform-team
  reviewers: 3
  tags:
    - runbook
    - on-call
---
```

A content property holds JSON, so front matter can give a number, a list or a
mapping as the value. A `Property` header can only say a string, which is the
one difference between the two forms.

`--global-properties` points at a YAML or JSON file of properties to set on
every page:

```bash
mark publish --global-properties confluence-properties.yaml --files "docs/**/*.md"
```

A document naming a property the file also names wins for its own page. A
property whose value has not changed is not written again, because Confluence
versions each one and rewriting it fills its history for nothing.

## Labels applied in Confluence

A page ends up with exactly the labels its `Label` headers name: one added in
the Confluence UI is removed on the next publish, because the document is taken
as the whole truth about its labels.

That is often not what a team wants. Labels drive macros, searches and reports,
and are frequently applied by people who are not editing the Markdown.
`--append-labels` adds what a document asks for without removing anything else:

```bash
mark publish --append-labels --files "docs/**/*.md"
```

The cost is that a label outlives the header that introduced it -- appending
cannot tell a `Label` header somebody deleted from a label somebody added in
Confluence. That is visible on the page and can be undone by hand, which the
deletion it prevents is not.
