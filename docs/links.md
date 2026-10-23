# Links

## Insert link to existing confluence page by title

```markdown
This is a [link to an existing confluence page](ac:Pagetitle)

And this is how to link when the linktext is the same as the [Pagetitle](ac:)

Link to a [page title containing spaces](<ac:With Multiple Words>)
```

## Link to another page in the same repository

A relative link to another Markdown file is replaced with a link to the
Confluence page that file publishes:

```markdown
See [the other page](./other.md) and [a heading in it](./other.md#setup).
```

The target file is read to find its `Space` and `Title`, and the page is looked
up by those. Links are rewritten to Confluence [tiny
links](https://support.atlassian.com/confluence/kb/how-to-programmatically-generate-the-tiny-link-of-a-confluence-page)
(`/x/AbCdEf`), which survive page renames and moves.

The path is read the way an image's is: `other%20page.md` and
`<other page.md>` both name `other page.md`, and `other\_page.md` and
`a&amp;b.md` name `other_page.md` and `a&b.md`. The name as written is tried
first, so a file whose name really contains a `%` still resolves to itself.

A link is left exactly as written when it has a scheme (`https:`, `mailto:`),
is a bare `#fragment`, points at a directory or a non-text file, or names a
file that has no mark metadata and so is never published. Links are resolved on
the parsed document, so a link that appears inside a code span or a fenced or
indented code block is left alone -- a page documenting Markdown gets to show
its examples unchanged. Files pulled in with `Include` are resolved along with
the document that includes them.

## Links between pages published together

A link is resolved by finding the page it points at, so a document linking to
another that the same run is creating has nothing to find yet.

Mark notes those documents and publishes them again once everything exists, so
links resolve within a single run. Only the documents that were waiting are
published a second time, and only on the run that creates their targets: once
the pages are there, later runs find them the first time and nothing is
published twice.

Nothing is published again on `--dry-run` or `--compile-only`, where no page is
being created for a link to wait for.

## Checking that links go somewhere

By default a link that cannot be resolved is left exactly as written, which in
Confluence means a link that leads nowhere. `--check-links` makes that a failure
instead:

```bash
mark publish --check-links internal,confluence --files "docs/**/*.md"
```

```text
ERR unable to compile markdown: link "./architecure.md" does not resolve:
    there is no such file
```

It reports in every mode, including `--compile-only` and `--dry-run`, so a pull
request gate can check links without Confluence credentials:

```bash
mark publish --compile-only --check-links all --files "docs/**/*.md"
```

| Value | Checks |
| --- | --- |
| `internal` | relative links to other Markdown files in the repository |
| `confluence` | `ac:` links, which name a Confluence page by title |
| `external` | requests each URL with a scheme to see whether it answers |
| `all` | all three |

The values are a set, not a mode: repeat the flag or separate them with commas,
and pick whichever combination suits. The three cost very different things --
`internal` is answered from the filesystem, `confluence` costs a lookup per link,
and `external` leaves the building -- so `internal,confluence` in CI with no
network checking is a perfectly reasonable choice, and the one above.

An `internal` link fails the run when the file it names is missing, is a
directory, or is a document that never becomes a page -- one with no title, so
there is nothing for the link to point at.

A link is looked for beside the document that contains it, and then beside each
file that document includes. A fragment reads as a document in its own right, so
a link inside one is written from where the fragment lives rather than from
wherever it is pulled into. The document's own directory is always tried first,
so this cannot change what an unambiguous link already meant. Only the files a
document includes directly are considered, not what those files include in turn.

One case is reported but does not fail: a link to a document that exists and has
a title, but is not in the space and is not being published by this run either.
A page the run is about to create is waited for rather than complained about --
see below -- so this is left for the genuinely absent.

A `confluence` link is checked by looking for a page of that title in the
document's own space, which is what an `ac:` link resolves against. The title is
read the way the renderer reads it: whatever follows the colon, or the link text
when nothing does, so `[Some Page](ac:)` is checked as `Some Page`.

These are checked once the run has finished rather than as each document
compiles, because a page named this way is often published by another file in
the same run. Each page is looked up once however many documents link to it.

`external` needs network access from wherever Mark runs, and makes publishing
dependent on every site you link to being up. Each URL is requested once per run
however many pages mention it. `HEAD` is tried first and a refusal is retried
with `GET`, since plenty of servers reject `HEAD` while serving the URL
perfectly well.

Bare `#fragments`, `mailto:` links and rooted paths are not links Mark resolves,
and are never checked.

Every broken link in a document is reported, not just the first, so a page with
several of them takes one run to find out rather than one run each.

### Adopting it on a repository that already publishes

`--check-links-warn-only` reports the same links without failing the run, which
is how to see the list before the build starts failing over it:

```bash
mark publish --check-links all --check-links-warn-only --files "docs/**/*.md"
```

Pages still publish exactly as they would have. It is a separate flag from
`--continue-on-error`, which is about files rather than links and still fails
the run at the end: use `--check-links-warn-only` to not fail at all, and
`--continue-on-error` to attempt every file before failing.

With `--output-format github` each warning is annotated against the file and the
line-less document it came from, so it appears in the pull request rather than
only in the build log:

```console
::notice file=docs/doc.md::published "Doc" to https://confluence.example.com/display/DOCS/1003
::warning file=docs/doc.md::link "guide" does not resolve: it is a directory, not a document
```

Without the flag the same link fails the run and is annotated as an `::error`
instead.

## Naming a Heading's Anchor

By default a heading's anchor is derived from its text. To name it yourself, use
the `{#custom-id}` syntax:

```markdown
## Release Notes {#rel}

[link to it](#rel)
```

```html
<h2 id="rel">Release Notes</h2>
```

The braces are consumed rather than rendered, so the heading reads as written.

A custom id replaces the derived one rather than adding to it, so the slug of
the heading text no longer resolves -- `[link](#release-notes)` above would be
left exactly as written. That is what other Markdown tools do too: a custom id
is the id, not an alias.

## Links to Headings on the Same Page

Mark generates Confluence-compatible heading anchors, which keep their capitals
and punctuation: `## My Heading` becomes `id="My-Heading"`. A link written the
way every other Markdown tool expects -- `[jump](#my-heading)` -- would not name
that anchor, and would quietly go nowhere.

Mark now points such links at the anchor it actually generated, so both spellings
work:

```markdown
## My Heading

[slug style](#my-heading) and [exact style](#My-Heading) both resolve.
```

Matching is on the letters and digits only, because the two conventions disagree
about which punctuation survives: a heading `API/v2 Guide` becomes
`API/v2-Guide`, while a slug of it is `apiv2-guide`. If two headings differ only
in punctuation that matching drops, the link is left exactly as written rather
than guessed at.

Both ends are published in the storage format's own terms rather than as HTML.
Confluence keeps no `id` on a heading — it generates its own from the element's
text — so a heading a link points at carries the Anchor macro, and the link
becomes `<ac:link ac:anchor="…">`. That is what footnotes have always used, and
it is the difference between a link that works and one that renders, is
clickable, and does nothing.

Only a heading something links to carries an anchor; the rest are left as they
were.

To mark a place no heading names, write an anchor by hand — Confluence keeps
neither the `name` nor the `id`, so mark turns it into the same macro a heading
gets:

```markdown
<a name="details"></a>

...later: [see the details](#details)
```

An anchor on *another* page is written after the page's title:

```markdown
[the setup](<ac:Other Page#Setup>)
```

The part after the last `#` is taken as an anchor only if it has no whitespace
in it, which is true of every anchor mark generates — so a page whose title
contains a `#`, like `<ac:C# Guide>`, is still just a title.

A relative link to a section of another document works the same way, and is
written the way every other Markdown tool expects:

```markdown
[the setup](./other.md#setup-guide)
```

The slug is folded onto the id mark gave that heading, exactly as a same-page
link is, and the link is published as an anchor on that page rather than as a
URL with a fragment — a fragment on a Confluence address names nothing, since
Confluence keeps a heading's anchor in the macro rather than in the address.

Two limits, both deliberate. The document has to be published to the same space,
because a page named without a space key is looked for in the current one; a
link across spaces keeps its URL. And a fragment naming no heading in that
document is left exactly as written, since guessing which section was meant
would send the reader somewhere the author did not choose.
