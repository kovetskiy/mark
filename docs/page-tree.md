# Where pages go

## Parents and folders

Changing a `Parent` header on a page that has already been published moves that
page to the new parent. Mark treats your repository as the source of truth for
where a page lives, as it already does for the page's title and content, so a
page somebody has moved by hand in Confluence is moved back on the next run. A
page nested *below* its declared parent is left where it is: every parent the
headers name is still in its ancestry, so nothing is contradicted.

The `Folder` header allows organizing pages within Confluence folders. Like `Parent` headers,
if Mark can't find the specified folder by title, it creates it. Folders and parents can be
mixed in the same document to create complex hierarchies.

> [!NOTE]
> Folder support is currently only available on Confluence Cloud and is not supported in Confluence Server / Data Center.

`--parents` puts every page of the run under the pages it names, ahead of the
parents a document declares itself: with `--parents "Engineering/Handbook"`, a
document whose `Parent` is `Setup` ends up under Engineering > Handbook >
Setup. The titles are separated by `/`, and `--parents-delimiter` sets another
separator for titles that contain a slash.

## Parents from the directory a file is in

`--parents-from-path` puts each page under a page named after every directory
between the file pattern's root and the file itself, so the page tree comes out
looking like the repository:

```bash
mark publish --parents-from-path --files "docs/**/*.md"
```

```text
docs/guides/setup.md        ->  Guides > Setup
docs/guides/deep/tuning.md  ->  Guides > Deep > Tuning
```

Directory names are titled the way filenames are, so `getting-started` becomes
"Getting Started".

An `index.md` or `README.md` **is** its directory's page rather than a page
inside it, whatever the case of its name. So `docs/guides/README.md` is the
page that `setup.md` sits under. Only a document the run publishes counts: a
README the `--files` pattern leaves out neither names its directory's page nor
stands for it. The document standing for the root itself is titled after the
root directory, there being no directory above it.

What that page is called is decided once, and the documents beneath it look for
the same name, so the two cannot disagree:

1. the `Title` header or front matter of the directory's own document
2. its leading heading, with `--title-from-h1`
3. a `title:` in a `.pages` file beside the documents, for a directory that has
   no document of its own
4. the directory's name

The filename is never used. It is `README` in every directory that has one,
which names a file rather than a page.

The root is everything in `--files` before the first wildcard, and
`--parents-from-path-root` overrides that where the guess is wrong. A root given
by hand that none of the files lie under is refused, and a file outside it is
reported and published without derived parents. A document that names its own
`Parent` is left where it asks to be, and `--parents` still prefixes everything.
A library caller's documents from memory are laid out by their paths with the
files (see [Using mark from Go](library.md#parents-from-the-path)).

### One page per title

A space holds one page of a given title, so two documents wanting the same title
want the same page: the second would overwrite the first and drag it under its
own parents. Deriving parents from the path makes that likely rather than
unlucky -- every directory tends to hold a README, and several will want an
"Overview".

Mark refuses before publishing the second one, comparing titles the way
Confluence does, without regard to case:

```text
docs/api/overview.md already publishes "Overview" in space "DOCS", and a space
holds one page of a title: rename one of them, or use
--title-append-generated-hash
```

`--title-append-generated-hash` is the way to keep both titles as they are: it
appends a short hash of the page's parents, space and title, taken once the
path has supplied the parents, so it differs between two documents in
different directories. The pages standing for directories are hashed the same
way, whether a README stands for them or not, so `docs/api/guides` and
`docs/sdk/guides` are two pages, and a directory's README is the very page the
documents beside it sit under.

### Directories are remembered too

Mark creates the page standing for a directory, so it remembers doing so. When
the last document under a directory goes away, that page turns up as a page with
no source file like any other, and `--on-orphan` decides what becomes of it:

```bash
mark publish --parents-from-path --track-pages --on-orphan delete --files "docs/**/*.md"
```

A directory's page is only removable once it holds nothing, since a page with
children is always left alone. So a directory and everything in it takes two
runs to disappear: the documents first, then the page that held them.

A directory holding its own `index.md` or `README.md` is not remembered this
way, that document's own entry having the page already. Directories are
remembered by their path as the run names it, so as with the files themselves,
the working directory is part of what the manifest matches on.

### Turning it on for pages that already exist

Every page not already where its path implies will be moved on the next run.
`--dry-run` reports the moves without making them, and is worth reading first on
a space anybody else works in.

## Tracking Pages Across Renames

Mark finds an existing page by its title, and that title comes from three
independently changeable places: the `Title` header, the leading H1, and the
filename when `--title-from-filename` is set. Edit any of them and the existing
page can no longer be found, so Mark publishes a second page beside the first
and leaves the original stranded under its old name.

`--track-pages` records which page each source file published to, keyed on the
file path, and consults that record at the one moment the title lookup comes up
empty -- which is where "this page is new" and "this page was renamed" are
otherwise indistinguishable:

```bash
mark publish --track-pages --files "docs/**/*.md"
```

Nothing is written back to your repository: no page IDs in the Markdown, no lock
file, no commit. The record lives in Confluence.

### What it detects

| change | how it is found |
| --- | --- |
| `Title` header edited | the path is the key, so the lookup still hits |
| leading H1 edited | the same |
| file renamed | content fingerprint, matched against paths that stopped appearing |
| parent page renamed | the page the declared parent chain resolved to last run, whatever it is called now |
| parent and child retitled in one run | the run's own document publishing the parent's new title, and the page it published last time, before that document's turn to retitle it |
| folder renamed in Confluence | the recorded folder ID, reused rather than duplicated |
| source file deleted | a recorded path absent from a run whose pattern covered it |
| two files claiming one page | the reverse index, when the second one is recorded |
| document changed `Space` | the same path recorded against another space; the page left behind is not removed |

A retitle is written even under `--changes-only`. The content fingerprint would
otherwise match, the update would be skipped, and the page would keep its old
title indefinitely.

Renaming a file is the awkward case: the path is the key, so a rename is a miss,
and when the title comes from the filename the title lookup misses too. What
connects them is the file's own content. Mark knows the whole file set before it
publishes any of it, so a path that has stopped appearing and a new file
carrying its content are matched to each other -- much as `git log --follow`
recovers a rename after the fact rather than being told about it.

### What it does not detect

| case | why |
| --- | --- |
| a file renamed *and* rewritten in one commit | the fingerprint is exact, so any content change breaks the match. Reads as a deletion plus a new page -- Git's limit too, without an `-M50%` to loosen it |
| an ambiguous rename | two unpublished documents sharing content, or a target page another file already claimed this run. A duplicate is a nuisance; a wrong rebind overwrites someone's page |
| anything in the first run | nothing is recorded until a file publishes with the flag set, so the first run only adopts |
| a rename across two `--files` patterns | matching and reporting are both scoped to the pattern that recorded the entry, so the file is a deletion in one and a new page in the other. That scoping is what lets several Mark invocations publish different folders into one space without reaching into each other's pages |
| whether a deletion was intended | it reports; it never deletes |
| a page nested deeper than its headers declare | every parent it declares is still somewhere in its ancestry, so there is nothing to find wrong, and moving it would tear up a hierarchy nobody asked to change. A `Parent:` change that does contradict the ancestry moves the page, with tracking or without it |
| a moved space homepage (Server/DC) | the mapping is anchored to it, so tracking starts over |

A page renamed by hand in Confluence is found by its ID and renamed back to what
the file says. That is deliberate -- the repository is the source of truth, as it
already is for titles and content -- but it is worth knowing before turning this
on over a space people edit directly.

A manifest is kept per space, and its entries stand for pages and folders in
that space alone. An entry naming one in another space -- a page moved there by
hand, or a manifest copied from another space or edited -- is not followed: that
page or folder is not updated, retitled, moved, archived or deleted, and the
document is published by its title as though nothing had been recorded for it.
The run warns about it with the `other-space` kind; a page `--on-orphan` would
have removed is instead listed under `orphans` with a `reason`. The space of a
recorded page is part of reading it: a page whose space mark cannot tell stops
the run rather than being passed over and published a second time, and
`--on-orphan` leaves such a page alone and says why.

### What it never does

Mark does not delete pages, and tracking does not change that. It reports source
files it can no longer account for:

```text
space "DOCS": 2 tracked page(s) had no matching source file in this run: docs/old.md, docs/removed.md
```

The report is suppressed on runs that had errors, where a file that failed to
process is indistinguishable from one that is gone. Each deletion is reported
once: having said it, Mark stops tracking that path, so the message does not
repeat forever and the mapping does not accumulate files that no longer exist.
What becomes of the page itself is your call.

A dry run resolves exactly as a real one does and says what it would have done,
including which existing page a retitle or a rename would have updated. It
writes nothing at all -- neither to Confluence nor to the mapping.

### Where the mapping is stored

| | storage |
| --- | --- |
| Cloud | space properties `mark.manifest.0` … `mark.manifest.15`, `mark.manifest.folders` and `mark.manifest.parents` |
| Server / Data Center | content properties of the same names, on the space homepage |
| `--manifest-page`, anywhere | content properties of the same names, on the page it names |

Space properties exist only in the v2 API, so Server and Data Center anchor to
the space homepage instead.

Creating a space property on Cloud takes space administration, which is a lot
to grant a publisher for the sake of a bookkeeping record, and a scoped API
token is refused it outright. `--manifest-page` keeps the mapping as content
properties of a page of your choosing instead, given by title or by id:

```bash
mark publish --track-pages --manifest-page "Team Handbook" --files "docs/**/*.md"
```

A content property needs only the right to edit the page it sits on, which a
publisher has by definition, and on Cloud it is written through the v2 API, so
a scoped token holding the page scopes is enough. A space holding several
independent mirrors can give each its own manifest this way. A title is looked
up in each space the run publishes to; an id is used as it is, and so suits a
run confined to one space. One page holds one space's manifest, so a run given
an id that publishes to a second space fails when it reaches it, rather than
the two spaces overwriting each other's mapping: name the page by title, or
publish each space in a run of its own with its own `--manifest-prefix`.

The page is looked for the first time a document in a space needs the mapping,
before anything is published there, and the run fails if it is not a page in
that space rather than quietly keep the mapping somewhere else. A dry run looks
for it in the same way.

#### When the manifest page does not exist yet

`--manifest-page-optional` (`TrackingConfig.ManifestPageOptional` for a Go
caller) publishes to a space whose manifest page is missing untracked instead,
with a warning. That is the first run into a new space, whose landing page
holds the manifest but is published after the documents under it:

```bash
mark publish --track-pages --manifest-page "Team Handbook" --manifest-page-optional --files "docs/**/*.md"
```

Such a space starts from an empty mapping, which is what it has: nothing is
found through an earlier mapping, so it behaves as a first run of
`--track-pages`. If the run publishes the manifest page itself, the mapping it
gathered is saved there at the end, and the next run is tracked, unless the
page already holds a mapping by then, kept by another run, which is left as it
is. Otherwise nothing is saved and the space is listed under `untracked` in the
[JSON report](publishing.md#reporting-what-a-run-did).

What needs an earlier mapping does not happen in an untracked space:

* `--on-orphan` finds no orphans there, so nothing is archived or deleted.
* `--no-overwrite` leaves every existing page alone, reported `skipped`:
  nothing records the version mark last wrote, so an edit made in Confluence
  cannot be told from none. A page the run creates, and an empty parent it
  created for the documents under it, is published as usual.

> **Note:** with `--no-overwrite`, this happens on every run for as long as
> the manifest page is missing, not only on the first: each run skips every
> existing page in the space and only creates new ones. Publish the manifest
> page, in the same run or before it, to have existing pages updated again.

Only a missing page is excused. A lookup that fails for any other reason still
fails the run.

### Two projects in one space

Two projects publishing into the same space would otherwise read and rewrite
one mapping between them, and each would report the other's files as gone.
`--manifest-prefix` names the properties differently for each:

```bash
mark publish --track-pages --manifest-prefix mark.manifest.api --files "api/**/*.md"
mark publish --track-pages --manifest-prefix mark.manifest.sdk --files "sdk/**/*.md"
```

Each keeps its own `<prefix>.0` … `<prefix>.15`, `<prefix>.folders` and
`<prefix>.parents`, and sees nothing of the other's. The prefix is made of
letters, digits, `_`, `-` and dots, like the default. Changing it on a project
that has already published starts that project's mapping afresh: the old
properties stay where they were and nothing reads them, so the next run finds
every page by title as a run without `--track-pages` would.

It is split over sixteen properties rather than held in one because Confluence
bounds how large a single property value may be, and one blob would cap how many
files a repository may have. Each path is assigned a shard by hash; all of them
are read in a single request and only the ones that changed are written back.

Folders and parents live in properties of their own rather than among the
shards. They are keyed by the chain of titles a document declares rather than by
a source path, there are few enough of them that splitting them buys nothing,
and a key of theirs among the page keys would be reported as a source file that
had gone missing.

## Removing pages whose files are gone

By default Mark reports a tracked page whose source file has disappeared and
does nothing about it. `--on-orphan` chooses otherwise:

| Value | Effect |
| --- | --- |
| `report` | say so and leave the page alone (the default) |
| `archive` | archive the page. Confluence Cloud only |
| `delete` | move the page to the trash |

```bash
mark publish --track-pages --on-orphan delete --files "docs/**/*.md"
```

`delete` means the trash, which Confluence keeps recoverable. Mark never purges
a page: that second step is left to a person.

A page somebody has already deleted or purged by hand counts as dealt with
under `archive` and `delete`: Mark says it is already gone and stops tracking
it.

`archive` exists only on Confluence Cloud, and Server and Data Center report
that they have no such thing rather than appearing to have archived anything.
Confluence accepts the request and archives afterwards, so Mark reports that it
asked rather than that it finished; a failure after acceptance is not something
Mark sees.

`--orphan-under` narrows it to the pages below one page or folder, named by
title or by id:

```bash
mark publish --track-pages --on-orphan delete --orphan-under "Team Handbook" \
     --files "docs/**/*.md"
```

Without it, every tracked page the `--files` pattern would have published is in
scope -- which is already narrower than the space, since a pattern is only
evidence about where it was looking.

The scope applies to everything Mark does about orphans, not only to removing
them. A page outside it is not reported either, and is still remembered, so a
later run with a wider scope knows about it.

### Publishing only some of the files

A job that publishes only the files a change touched, beside the run that
publishes them all, says so with `--partial`, and names the paths the change
touched, deleted and renamed ones included:

```bash
git -c core.quotePath=false diff --no-renames --name-only "$BASE" -- 'docs/*.md' |
  mark publish --track-pages --partial --files-from -
```

`--no-renames` lists a renamed file under its old path as well as its new one,
which the partial run needs to follow the rename to its page: with rename
detection on, as it is by default, `git diff` names only the new path, and
the renamed file gets a page of its own. `core.quotePath=false` keeps a path
holding characters outside ASCII as written instead of quoted.

`--files-from` reads one path a line, from a file or from standard input for
`-`, and takes each as written: a path holding `*`, `[` or `{` names that file
and nothing else, and one that no longer exists is not published but still
looked for. It can stand beside `--files`, or replace it; a pattern such as
`--files "docs/{setup,install,old-name}.md"` works too, for paths with no glob
characters in them, when the run only reports orphans. A library caller gives
the paths as `Config.Paths`.

Without it, Mark ties a tracked file to the `--files` pattern that last
published it. A run with a narrower pattern then neither notices a deleted file
nor follows a renamed one, which the full pattern recorded, and it records the
pages it publishes under its own pattern, so the full run no longer notices
those files being deleted either. And the full pattern on a checkout holding
only the changed files reads every other file as deleted.

A run with `--partial` looks for deleted and renamed files among the tracked
files its `--files` pattern and `--files-from` paths name, whichever run
recorded them, and leaves every other tracked file alone: one of those it
names that is missing is reported, and dealt with by `--on-orphan`, as in a
full run, except that one only reported stays tracked for the run publishing
them all; a renamed one is followed to its page with its `--no-overwrite`
baseline. A page it publishes keeps the pattern its file was tracked under, or
that of the file it was renamed from while the new path lies in it, so the
full run still covers it. A file no run has tracked yet is recorded under the
`--files` pattern when a run publishing that whole pattern has tracked files
under it, and otherwise under its own path until the full run publishes it, or
finds it deleted, so any partial run naming it covers it, and one reporting it
gone forgets it.

A path given with `--files-from` that the `--files` pattern does not match is
recorded the same way, under its own path, in a run with or without
`--partial`: a later run naming it looks for it, however many paths each run
names.

The full pattern with `--partial` names every file, so on a checkout holding
only some of them it cannot tell a deleted file from one not checked out. A
partial run leaves out the tracked files recorded under the very `--files`
pattern it was given, unless `--files-from` names them: like a page outside
`--orphan-under`, they are not reported, not touched, not taken for the old
name of a moved or renamed file, and stay tracked for the run publishing them
all. The log names them.

With `--on-orphan archive` or `delete`, a partial run takes only exact paths:
`--files-from`, or a `--files` that names one file (a glob character escaped
with `\` counts as written). A wildcard is refused before anything is
published, since it reaches every tracked file it matches in the manifest,
another job's included, and would remove the pages of those missing from the
checkout. A `files` set in the configuration file or `MARK_FILES` counts too:
give `--files ""` beside `--files-from`. A library caller gets an error
`errors.Is` matches to `mark.ErrPartialPattern`.

A change that only deletes files leaves the partial run nothing to publish: it
fails with "no files matched", or under `--ci` does nothing about them, and the
run publishing them all deals with those files instead.

A library caller sets `TrackingConfig.Partial`; a caller driving
`manifest.Store` itself calls `SetPartial`. For documents handed over from
memory, see [publishing some of a scope's documents](library.md#publishing-some-of-a-scopes-documents).

### Flags that need other flags

A combination that would leave you believing you are protected is refused
outright rather than warned about, because the belief comes from silence:

* `--on-orphan archive` or `delete` without `--track-pages`
* `--no-overwrite` without `--track-pages`
* `--partial` without `--track-pages`
* `--partial` with `--on-orphan archive` or `delete` and a `--files` wildcard
* `--manifest-page-optional` without `--manifest-page`
* `--check-links-warn-only` without `--check-links`, or with one that names no
  kind of link
* `--links-staged-root` without `--links-source-root`, or the other way round
* `--links-source-root-check-only` without `--links-source-root`

A combination that merely does nothing -- `--track-pages` alongside a page ID,
where the mapping cannot apply -- is a warning.

### What stops a page being removed

* `--track-pages` is required, and Mark refuses to start without it. Only the
  manifest knows which pages Mark published, and a guess from titles is not one
  worth making about deletion.
* A page holding **child pages** is left alone and reported, because removing it
  would take them with it and they may be pages nobody wrote in this repository.
* A run in which **any file failed** removes nothing: a file that failed to
  process is indistinguishable from one that was deleted.
* A run that **published nothing** removes nothing, so a failed checkout cannot
  empty a space.
* A page in **another space** than the manifest that records it is left alone
  and reported. Its entry is kept, so each run reports it again until the entry
  is corrected or the page is back in the space.
* The **space homepage** and the **page the manifest is kept on**
  (`--manifest-page`, or the homepage on Server and Data Center) are left alone
  and reported, as `mark page delete` and `archive` leave the homepage. A
  document titled like one of them publishes to it, and removing the document
  does not remove the page. So is every page of a space whose homepage Mark
  could not read, since any of them may be it. A run with `--on-orphan report`
  stops tracking such a page, as it does every page it reports.
* A page in **a space the run was not given** is left alone and reported, and
  stays tracked. With `--space`, a run looks for orphans in that space alone: a
  document's `Space` header says where that document is published, not that the
  other pages tracked in its space are this run's to remove. A run with no
  `--space` takes its spaces from its documents and looks for orphans in each,
  so publish such a space in a run given it, or none.
* A document whose **`Space` changed** is not gone. The page it published
  before is left behind in the space it left, which stops tracking it; the run
  says so and reports no orphan.
* A document that opted out with `Synchronized: false` still counts as present.
* `--dry-run` reports what would go and touches nothing.

A page that is left alone stays in the manifest, so the next run finds it again
rather than losing sight of it.

### Worth knowing

* Paths are keyed relative to the directory Mark runs in and with forward
  slashes, so `--files "$PWD/docs/*.md"` and `--files "docs/*.md"` share one
  mapping when run from the same place, as do a Windows workstation and Linux
  CI. A file outside that directory has no better anchor and is keyed by its
  absolute path.
* `--track-pages` has no effect when publishing straight to a page ID, since the
  mapping is per space and per file and a page ID is neither.

## Listing, moving, copying, renaming, removing and watching pages by hand

`mark page` acts on one page in Confluence directly, without a Markdown file.
It is experimental: its commands and flags may change between releases.

```bash
mark page tree   <page> [--depth N]          # a page, or a folder by id
mark page move   <page> --parent <page>      # under a page, or a folder by id
mark page move   <page> --before <sibling>   # or --after, beside a sibling
mark page copy   <page> --parent <page> [--title T] [--prefix P] [--recursive]
mark page rename <page> "New title"
mark page archive <page>                     # Confluence Cloud only
mark page delete <page> [--recursive]
mark page watch   <page> [--user <name>]... [--recursive]  # or unwatch
```

A `<page>` is an id, a URL as the browser shows it or a short `/x/...` link,
or a title together with `--space`. A URL also gives the base URL when `--base-url` is not set; when it
is set, a URL on another instance is refused, as [export](export.md) refuses
one. A title made only of digits is read as an id, so name such a page by its URL.

Every command takes `--dry-run`, which says what it would do and changes
nothing.

| command | what it does | what it refuses |
| --- | --- | --- |
| `tree` | prints the page and everything beneath it, with ids; folders and, on Cloud, other content such as whiteboards are marked with their type. `--depth` counts the page itself as one level | nothing |
| `copy` | copies the page under `--parent`, with its attachments, labels and content properties but not its restrictions. `--recursive` copies the pages beneath it too, each under the copy of its parent. Confluence Cloud only | a title its space already has, or that two copies would share; with `--recursive`, a folder or other non-page content in the tree, or a `--parent` inside it |
| `move` | moves the page, and everything beneath it, under `--parent` or beside a sibling. `--parent` with `--before` or `--after` checks the sibling is that parent's child | a blog post; another space; a page beneath itself; a sibling at the top of the space |
| `rename` | sets the title and keeps the content, as a minor edit with the version message `renamed from "<old title>"` | a title another page in the space already has |
| `archive` | asks Confluence to archive the page; Confluence archives it afterwards | a page with children; the space homepage |
| `delete` | moves the page to the trash. `--recursive` trashes its descendants too, deepest first | a page with children without `--recursive`; a page with a folder beneath it; the space homepage, or with `--recursive` a page it is beneath |
| `watch`, `unwatch` | starts or stops Confluence notifying you of the page's changes, or each `--user`. `--recursive` does the same for every page beneath it, from the top down | a `--user` on Cloud whose full name matches no user, or more than one; with `--recursive`, a page with a folder anywhere beneath it |

`copy` names the top copy `--title`, or else `--prefix` and the page's own
title, and every copy beneath it `--prefix` and its own title. A copy in the
same space as its source therefore needs one of them. Every title is checked
before the first copy is made, so a clash stops the run before it changes
anything. A copy is as open as the parent it lands under: restrictions are not
copied, which is worth knowing before copying a restricted page.

`--user` names a user the way a `Restrictions` header does: on Cloud by full
name, which has to match exactly one user, or as `accountid:<id>`; on Server and
Data Center by username. Watching for anyone but yourself takes space
administration. A user already in the state asked for is reported and left
alone. Confluence watches a page alone, so `--recursive` watches each page
beneath it in turn: pages added later are not watched. It reads the whole tree
before changing anything, so a folder in it, whose pages it cannot list, stops
the run before the first watch rather than halfway.

Nothing is purged: the trash stays recoverable, as it does for `--on-orphan
delete`. `--recursive`, on any command, is read only from the command line, never from the
configuration file or the environment.

These commands do not read or update what `--track-pages` recorded. A page
that `publish` manages goes back to what its file says the next time `publish`
runs: its title, and its parent if the move contradicts the file's `Parent`
headers. Delete a managed page's file as well, or the next `publish` tries to
create the page again. To rename or move such a page for good, change the file.
