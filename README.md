# Mark

<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-44-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END -->

Mark — a tool for syncing your markdown documentation with Atlassian Confluence
pages.

This is very useful if you store documentation to your software in a Git
repository and don't want to do an extra job of updating Confluence page using
a tinymce wysiwyg enterprise core editor which always breaks everything.

Mark does the same but in a different way. Mark reads your markdown file, creates a Confluence page
if it's not found by its name, uploads attachments, translates Markdown into HTML and updates the
contents of the page via REST API. It's like you don't even need to create sections/pages in your
Confluence anymore, just use them in your Markdown documentation.

A document names its page in a few header comments, and the rest is the page:

```markdown
<!-- Space: DOCS -->
<!-- Parent: Guides -->
<!-- Title: Release process -->

How a release is cut.
```

```shell
mark publish -b https://example.atlassian.net/wiki -u user -p token -f docs/release.md
```

## Documentation

The reference is split by topic under [`docs/`](docs/):

* [Page metadata](docs/metadata.md): YAML front matter and header comments,
  layout and appearance, restrictions, page titles, labels and content
  properties.
* [Where pages go](docs/page-tree.md): parents and folders, parents from the
  directory tree, tracking pages across renames, removing pages whose files
  are gone, and listing, moving, copying, renaming, removing and watching pages
  by hand with `mark page`.
* [Formatting](docs/formatting.md): how Markdown becomes Confluence content,
  including code blocks, alerts, tables, task lists, Mermaid, D2 and PlantUML
  diagrams, math, footnotes and emoji.
* [Attachments and images](docs/attachments.md).
* [Links](docs/links.md): links to other pages, link checking, and heading
  anchors.
* [Includes, macros and templates](docs/templates.md), with the built-in
  templates and examples.
* [Publishing](docs/publishing.md): publishing to a page by its URL, page
  headers, run reports, what happens when Confluence is unavailable, continuous
  integration, preserving inline comments, and leaving hand-edited pages alone.
* [Exporting a page](docs/export.md), or a tree of them, from Confluence back to
  Markdown.

## Installation

### Homebrew

```bash
brew tap kovetskiy/mark
brew install mark
```

### Go Install

```bash
go install github.com/kovetskiy/mark/v17/cmd/mark@latest
```

`@latest` is the newest v17 release, so this needs v17 to be tagged. Until it
is, there is no v17 release to install: the command fails, or builds whatever
the default branch holds. Install a release from the Releases page or Homebrew
until then.

### Releases

[Download a release from the Releases page](https://github.com/kovetskiy/mark/releases)

### Docker

```bash
docker run --rm -i kovetskiy/mark:latest mark publish <params>
```

### Compile and install using docker-compose

Mostly useful when you intend to enhance `mark`.

```bash
# Create the binary
$ docker-compose run markbuilder
# "install" the binary
$ cp mark /usr/local/bin
```

## Usage

`mark` does one thing per command. `mark publish` compiles markdown files and
publishes them to Confluence:

```bash
mark publish -u user -p token -b https://example.atlassian.net/wiki -f "docs/**/*.md"
```

and the experimental `mark export` writes a Confluence page out as markdown
that publishes back to it; see [Exporting a page](docs/export.md#exporting-a-page).

`mark doctor` checks a setup without changing anything. It logs in with the
global options, says which platform answered, and checks that each space it is
given exists and lets the account create pages and upload attachments. A login
that Confluence answers as the anonymous user fails, since a proxy that drops
the credentials looks like a working login otherwise, and so does an empty
space key, as an unset `$SPACE` gives. A space without a home page is only a
warning: a page with no `Parent` header is then published at the top of the
space. When a check fails, it says what to change:

```text
$ mark doctor -b https://example.atlassian.net/wiki -u jane@example.com -p "$TOKEN" DOCS
[ok] login: logged in as Jane Doe (jane@example.com, 5b10ac8d82e05b22cc7d4ef5)
[ok] platform: Confluence Cloud
[ok] space DOCS: "Documentation" is readable
[ok] space DOCS: home page "Documentation Home" (98306)
[ok] space DOCS: can create pages, upload attachments, create blog posts (Type: blogpost)
[warn] space DOCS: cannot delete pages (--on-orphan delete), archive pages (--on-orphan archive), restrict pages (--edit-lock, Restriction headers)
       a space administrator grants these under the space's permissions
```

It exits with an error when any check fails, and without one when there are
only warnings, so it can also run as the first step of a pipeline. With no
space given it checks `--space`, from the command line, `MARK_SPACE` or the
configuration file.

The global options -- the configuration file, how to reach and authenticate
against Confluence, and logging -- belong to every command, and can be given
before or after its name: `mark -c ci.toml publish -f doc.md` and
`mark publish -c ci.toml -f doc.md` are the same run. The options of a command
go after its name.

```text
NAME:
   mark - A tool for updating Atlassian Confluence pages from markdown.

USAGE:
   mark [global options] [command [command options]]

VERSION:
   vX.Y.Z

DESCRIPTION:
   Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark

COMMANDS:
   publish  publish markdown files to Confluence
   export   export a Confluence page, or a tree of them, to markdown (experimental)
   page     list, move, copy, rename, archive, delete or watch Confluence pages (experimental)
   doctor   check that mark can reach Confluence, log in, and publish to a space
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --config string, -c string    use the specified configuration file. (default: "${HOME}/.config/mark.toml") [$MARK_CONFIG]
   --username string, -u string  use specified username to authenticate with Confluence. [$MARK_USERNAME]
   --password string, -p string  use specified token to authenticate with Confluence. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication. [$MARK_PASSWORD]
   --password-command string     run the specified command and use the first line of its stdout as the token to authenticate with Confluence. Runs without a shell. Mutually exclusive with password. [$MARK_PASSWORD_COMMAND]
   --base-url string, -b string  base URL for Confluence. Alternative to the base-url config file key. [$MARK_BASE_URL]
   --insecure-skip-tls-verify    skip TLS certificate verification (useful for self-signed certificates) [$MARK_INSECURE_SKIP_TLS_VERIFY]
   --log-level string            set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL. (default: "info") [$MARK_LOG_LEVEL]
   --color string                display logs in color. Possible values: auto, never. (default: "auto") [$MARK_COLOR]
   --help, -h                    show help
   --version, -v                 print the version
```

### mark publish

```text
NAME:
   mark publish - publish markdown files to Confluence

USAGE:
   mark publish [options]

DESCRIPTION:
   Compile each markdown file --files names into Confluence storage format and create or update the page its metadata names.

OPTIONS:
   --files string, -f string                      use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted). [$MARK_FILES]
   --continue-on-error                            don't exit if an error occurs while processing a file, continue processing remaining files. [$MARK_CONTINUE_ON_ERROR]
   --compile-only                                 show resulting HTML and don't update Confluence page content. [$MARK_COMPILE_ONLY]
   --dry-run, -n                                  resolve page and ancestry, show resulting HTML and exit. With --changes-only, say which pages would change and show the HTML of only those, and none with --output-format json or github. [$MARK_DRY_RUN]
   --edit-lock, -k                                lock page editing to current user only to prevent accidental manual edits over Confluence Web UI. [$MARK_EDIT_LOCK]
   --drop-h1                                      don't include the first H1 heading in Confluence output. [$MARK_DROP_H1]
   --strip-linebreaks, -L                         remove linebreaks inside of tags, to accommodate non-standard Confluence behavior [$MARK_STRIP_LINEBREAKS]
   --title-from-h1                                extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata. Mutually exclusive with --title-from-filename. [$MARK_TITLE_FROM_H1]
   --title-from-filename                          use the filename (without extension) as the Confluence page title if no explicit page title is set in the metadata. Mutually exclusive with --title-from-h1. [$MARK_TITLE_FROM_FILENAME]
   --parents-from-path                            place each page under a page named after every directory between the file pattern's root and the file itself. An index.md or README.md is the page for its own directory rather than a page inside it. A document that names its own Parent is left alone. [$MARK_PARENTS_FROM_PATH]
   --parents-from-path-root string                the directory --parents-from-path measures from. Taken from the --files pattern when not given, which is everything before its first wildcard. [$MARK_PARENTS_FROM_PATH_ROOT]
   --title-append-generated-hash                  appends a short hash generated from the path of the page (space, parents, and title) to the title [$MARK_TITLE_APPEND_GENERATED_HASH]
   --minor-edit                                   don't send notifications while updating Confluence page. [$MARK_MINOR_EDIT]
   --watch                                        let the publishing account watch the pages mark creates or edits, as Confluence makes it (default: true, which leaves Confluence's own behaviour alone); --watch=false stops it watching them, though pages it watched already stay watched. [$MARK_WATCH]
   --version-message string                       add a message to the page version, to explain the edit (default: "") [$MARK_VERSION_MESSAGE]
   --target-url string, -l string                 edit the Confluence page at this URL, as a browser shows it: .../pages/viewpage.action?pageId=ID or .../spaces/KEY/pages/ID/Title. The instance is taken from it too, context path included, unless --base-url names it. Without it, each file must name its page with Space and Title metadata headers. [$MARK_TARGET_URL]
   --ci                                           run on CI mode. It won't fail if files are not found. [$MARK_CI]
   --space string, -s string                      use specified space key. If the space key is not specified, it must be set in the page metadata. [$MARK_SPACE]
   --parents string                               A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be prepended to the ones defined in the document itself. [$MARK_PARENTS]
   --parents-delimiter string                     The delimiter used for the parents list (default: "/") [$MARK_PARENTS_DELIMITER]
   --content-appearance string                    default content appearance for pages without a Content-Appearance header. Possible values: full-width (Wide), fixed, default (Narrow), max (Max). [$MARK_CONTENT_APPEARANCE]
   --mermaid-scale float                          defines the scaling factor for mermaid renderings: the pixels of a png, and the size the page displays an svg at. (default: 1) [$MARK_MERMAID_SCALE]
   --mermaid-engine string                        what mermaid diagrams are drawn by: chrome (the default, a headless browser running mermaid.js) or merman (experimental, a native reimplementation that needs no browser and must be installed separately). (default: "chrome") [$MARK_MERMAID_ENGINE]
   --mermaid-output string                        image a mermaid diagram is published as: png (rasterised, and scaled by --mermaid-scale) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment). (default: "png") [$MARK_MERMAID_OUTPUT]
   --mermaid-bundle                               keep the diagram's own source inside the SVG published for it, in its <desc> element, so the drawing can be edited again from the attachment. Needs --mermaid-output=svg. [$MARK_MERMAID_BUNDLE]
   --math-format string                           image a formula is published as with --features=math: png (rasterised through the same headless Chrome mermaid uses) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment). (default: "png") [$MARK_MATH_FORMAT]
   --math-scale float                             defines the scaling factor for PNG formula renderings; ignored when math-format is svg. (default: 2) [$MARK_MATH_SCALE]
   --include-path string                          Path for shared includes, used as a fallback if the include doesn't exist in the current directory. [$MARK_INCLUDE_PATH]
   --changes-only                                 Avoids re-uploading pages that haven't changed since the last run. [$MARK_CHANGES_ONLY]
   --output-format string                         how to report what the run did: "url" prints the address of each published page (the default), "json" prints one object describing the whole run, "github" prints GitHub Actions workflow commands so that failures appear against the file that caused them. (default: "url") [$MARK_OUTPUT_FORMAT]
   --on-orphan string                             what to do about a page whose source file is gone: "report" says so and does nothing (the default), "archive" archives the page (Confluence Cloud only), "delete" moves it to the trash. Requires --track-pages. (default: "report") [$MARK_ON_ORPHAN]
   --orphan-under string                          limit --on-orphan, and the reporting it does, to pages below this page or folder, given by title or id. Without it, every tracked page the --files pattern would have published is in scope. [$MARK_ORPHAN_UNDER]
   --check-links string [ --check-links string ]  fail on links that do not resolve. Repeat or comma-separate any of: "internal" (relative links to other files in the repository), "confluence" (ac: links naming a page by title), "external" (requests each URL to see whether it answers), or "all". [$MARK_CHECK_LINKS]
   --global-properties string                     path to a YAML or JSON file of Confluence content properties to set on every page. A Property header or properties front matter in a document wins over the file for that page. [$MARK_GLOBAL_PROPERTIES]
   --append-labels                                add the labels a document asks for without removing any others, so that labels applied in Confluence survive a publish. Without it, a page ends up with exactly the labels its Label headers name. [$MARK_APPEND_LABELS]
   --check-links-warn-only                        report links that do not resolve without failing the run. Only meaningful together with --check-links. [$MARK_CHECK_LINKS_WARN_ONLY]
   --no-overwrite                                 Leave alone any page that has been edited in Confluence since mark last published it, instead of overwriting the edit. Requires --track-pages, which is where the last published version is remembered. [$MARK_NO_OVERWRITE]
   --track-pages                                  Remember which page each file publishes to, so renaming a file or changing its title updates the existing page instead of creating a second one. Stores the mapping in Confluence (a space property on Cloud, a homepage content property on Server/Data Center, or the page --manifest-page names); nothing is written to the repository. [$MARK_TRACK_PAGES]
   --manifest-page string                         keep the --track-pages mapping as content properties of this page, given by title or id, instead of as space properties. Needs only the right to edit that page where a space property needs space administration, and works with a scoped API token. Requires --track-pages. [$MARK_MANIFEST_PAGE]
   --manifest-prefix string                       name the --track-pages mapping's properties under this prefix, so that two projects publishing into one space keep manifests of their own instead of sharing, and reporting each other's files as gone. Letters, digits, '_', '-' and dots. Requires --track-pages. (default: "mark.manifest") [$MARK_MANIFEST_PREFIX]
   --preserve-comments                            Fetch and preserve inline comments on existing Confluence pages. [$MARK_PRESERVE_COMMENTS]
   --d2-output string                             image a d2 diagram is published as: png (rasterised) or svg (vector and sharp at any zoom, with whatever the diagram references inlined into it, where the instance displays an SVG attachment). (default: "png") [$MARK_D2_OUTPUT]
   --d2-bundle-remote                             let a d2 diagram published as svg have mark fetch the URLs it names, and publish what comes back inside the drawing. Off by default, which refuses a diagram that names a URL: the request is made by the document rather than by you, to any address it likes. [$MARK_D2_BUNDLE_REMOTE]
   --d2-scale float                               defines the scaling factor for d2 renderings: the pixels of a png, and the size the page displays an svg at. (default: 1) [$MARK_D2_SCALE]
   --features string [ --features string ]        Enables optional features, replacing the defaults (mermaid, mention) rather than adding to them; none switches every one off. Current features: d2, date, emoji, frontmatter, inline-link-card, math, mention, mermaid, mkdocsadmonitions, plantuml (default: "mermaid", "mention") [$MARK_FEATURES]
   --attach-referenced                            upload a local file that a link points at, and link to the attachment. Without it the link is published as the path the document wrote, which means nothing once the page is on Confluence. Images are attached either way. [$MARK_ATTACH_REFERENCED]
   --image-align string                           set image alignment (left, center, right). Can be overridden per-file via the Image-Align header. [$MARK_IMAGE_ALIGN]
   --page-header string                           path to a template placed at the top of every page: Markdown if it has a Markdown extension such as .md, storage format otherwise. It is given .Path, .EscapedPath, .Title and .Space. [$MARK_PAGE_HEADER]
   --help, -h                                     show help

GLOBAL OPTIONS:
   --config string, -c string    use the specified configuration file. (default: "${HOME}/.config/mark.toml") [$MARK_CONFIG]
   --username string, -u string  use specified username to authenticate with Confluence. [$MARK_USERNAME]
   --password string, -p string  use specified token to authenticate with Confluence. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication. [$MARK_PASSWORD]
   --password-command string     run the specified command and use the first line of its stdout as the token to authenticate with Confluence. Runs without a shell. Mutually exclusive with password. [$MARK_PASSWORD_COMMAND]
   --base-url string, -b string  base URL for Confluence. Alternative to the base-url config file key. [$MARK_BASE_URL]
   --insecure-skip-tls-verify    skip TLS certificate verification (useful for self-signed certificates) [$MARK_INSECURE_SKIP_TLS_VERIFY]
   --log-level string            set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL. (default: "info") [$MARK_LOG_LEVEL]
   --color string                display logs in color. Possible values: auto, never. (default: "auto") [$MARK_COLOR]
```

### mark export

```text
NAME:
   mark export - export a Confluence page, or a tree of them, to markdown (experimental)

USAGE:
   mark export [options]

DESCRIPTION:
   Experimental: the Markdown it writes may change between releases. Write the page --page-id, --url or --space and --title names as markdown, with the metadata headers that publish it back to the same page, and download the attachments it shows or links to.

OPTIONS:
   --page-id string            export the page or blog post with this id. [$MARK_PAGE_ID]
   --url string, -l string     export the page at this address, as a browser shows it: /spaces/KEY/pages/ID/... on Cloud, /display/KEY/Title or /pages/viewpage.action?pageId=ID on Server and Data Center, or a short /x/... link on either. Also gives the base URL when --base-url is not set. [$MARK_URL]
   --space string, -s string   the space key --title looks the page up in. [$MARK_SPACE]
   --title string, -t string   export the page or blog post with this title in --space. [$MARK_TITLE]
   --output string, -o string  write the Markdown to this file rather than to standard output ("-"). [$MARK_OUTPUT]
   --attachments-dir string    write the attachments the page shows or links to into this directory. Defaults to the directory of --output, or the working directory; a directory of their own changes their names when the document is published back, since mark names an attachment after its path. [$MARK_ATTACHMENTS_DIR]
   --overwrite                 replace the Markdown file and attachments if they are already there. Without it, the export fails before writing anything. [$MARK_OVERWRITE]
   --no-attachments            write the Markdown only, and download no attachments. The document still refers to them where they would have been written. [$MARK_NO_ATTACHMENTS]
   --recursive, -r             export the pages beneath it too, into the directory --output names: the page to index.md there, and each page beneath it to index.md in a directory of its own named after its title.
   --depth int, -d int         with --recursive, export this many levels, counting the page itself; 0 is every level. (default: 0)
   --relative-links            with --recursive, write a link between two exported pages, or a URL of one pasted into a page, as a relative link between their files, which a repository host can follow, rather than as ac:Title or the URL. Published back, such a link becomes a link to the page's address rather than Confluence's own page link.
   --help, -h                  show help

GLOBAL OPTIONS:
   --config string, -c string    use the specified configuration file. (default: "${HOME}/.config/mark.toml") [$MARK_CONFIG]
   --username string, -u string  use specified username to authenticate with Confluence. [$MARK_USERNAME]
   --password string, -p string  use specified token to authenticate with Confluence. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication. [$MARK_PASSWORD]
   --password-command string     run the specified command and use the first line of its stdout as the token to authenticate with Confluence. Runs without a shell. Mutually exclusive with password. [$MARK_PASSWORD_COMMAND]
   --base-url string, -b string  base URL for Confluence. Alternative to the base-url config file key. [$MARK_BASE_URL]
   --insecure-skip-tls-verify    skip TLS certificate verification (useful for self-signed certificates) [$MARK_INSECURE_SKIP_TLS_VERIFY]
   --log-level string            set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL. (default: "info") [$MARK_LOG_LEVEL]
   --color string                display logs in color. Possible values: auto, never. (default: "auto") [$MARK_COLOR]
```

### Running mark without a command

Before it had commands, `mark` took the publish options directly, and that
still works: `mark -f doc.md` publishes exactly as `mark publish -f doc.md`
does, reading the same flags, environment variables and configuration file, and
logs a deprecation warning, so move pipelines over to `mark publish` at your
convenience. A few command lines read differently than they used to:

* `mark` on its own publishes what `MARK_FILES` or the configuration file names.
  With nothing to publish it prints the help and fails.
* An argument that is not a flag was ignored. One that looks like a mistyped
  command (`mark publsh -f doc.md`) is now an error, and so is any such
  argument when `--files` is not given. Others are still ignored, with a
  warning: usually they are a `--files` glob the shell expanded.
* A publish option written before a command (`mark -f doc.md publish`) is an
  error rather than silently ignored.
* `mark -f doc.md --help` shows mark's own help, which lists the commands; the
  publish options are under `mark publish --help`.

You can store user credentials in the configuration file, which should be
located in a system specific directory (or specified via `-c --config <path>`) with the following format (TOML):

```toml
username = "your-email"
password = "password-or-api-key-for-confluence-cloud"
# Or name a command that prints the token, instead of storing it here:
# password-command = "pass show confluence/api-token"
# If you are using Confluence Cloud add the /wiki suffix to base-url
base-url = "http://confluence.local"
title-from-h1 = true
drop-h1 = true
image-align = "center"
```

**NOTE**: `password-command` keeps the token out of both the configuration file and the environment.
A password manager or keychain helper supplies it per run.

The command runs without a shell, so the string is split on whitespace and
nothing is expanded: there is no quoting, and a program path or argument
containing a space cannot be written here. Wrap such a helper in a small script
and name the script instead. The command also gets no standard input, so a
helper that prompts on stdin cannot be used -- one that opens the terminal
itself, as `pinentry` does, is fine.

Only the first line of what the command prints becomes the token, so a helper
that prints a whole entry with the password on top -- `pass show` does -- needs
no wrapper to trim it.

The command has to fetch the secret rather than contain it. It is handed to the
operating system as written, so anyone who can list processes can read it for as
long as it runs -- which is the exposure `password-command` exists to avoid, and
writing a token into the command itself puts it straight back.

`password` and `password-command` are mutually exclusive, and setting both --
in any combination of flag, environment variable and configuration file -- is an
error rather than a choice mark makes for you. Moving to a command therefore
means taking the `password` out of the configuration file, not leaving it there
beside the new setting.

A compile resolves the command like any other run, since a relative link is
followed by looking that page up. A helper that cannot produce a token there is
a warning and not an error, because `--compile-only` has to keep validating
documents on a machine -- a CI image, typically -- that has no password manager
on it.

### Scoped API tokens

An Atlassian scoped API token only works through the `api.atlassian.com`
gateway, so that is what the base URL has to name:

```toml
base-url = "https://api.atlassian.com/ex/confluence/<cloud-id>"
```

The gateway checks the token's scopes against the endpoint being called, and
the older v1 endpoints check content scopes a scoped token is not usually
minted with. Through the gateway mark therefore reads, creates and updates
pages with the v2 API, which needs:

| scope | what it is for |
| --- | --- |
| `read:space:confluence` | resolving a space key and finding its home page |
| `read:page:confluence`, `write:page:confluence` | finding, reading, creating and updating pages |
| `read:blogpost:confluence`, `write:blogpost:confluence` | the same for blog posts (`Type: blogpost`) |
| `read:attachment:confluence` | listing what is attached to a page, and `mark export` downloading it |
| `read:label:confluence` | reading the labels a page carries |
| `read:content.property:confluence`, `write:content.property:confluence` | the content appearance and emoji title of a page |
| `read:page:confluence`, `write:page:confluence` | `--track-pages` with `--manifest-page`; without it the mapping is a space property, whose creation a scoped token is refused |
| `read:folder:confluence`, `write:folder:confluence`, `read:hierarchical-content:confluence` | `Folder` headers: reading and creating folders, and finding one among its parent's direct children |

Uploading attachments, applying `Label` headers, `--preserve-comments`,
`--on-orphan`, `--watch=false`, page restrictions, mentions, moving a page among its siblings and
finding a folder left at the space root have no v2 endpoint and stay on v1. To use those with a scoped token, grant the
classic scopes (`read:confluence-content.all`, `write:confluence-content`,
`read:confluence-user`) alongside the granular ones.

**NOTE**: Labels aren't supported when using `minor-edit`!

**NOTE**: Attachments are always uploaded as minor edits, whether or not
`--minor-edit` is set. Watchers are notified about the page update that carries
them, which `--minor-edit` controls, rather than once more for every file.

**NOTE**: Confluence makes whoever creates or edits a page watch it, so the
account Mark publishes with is notified of every later change to every page it
touched. `--watch=false` stops it watching the pages it creates, updates,
moves, labels or attaches files to, parents included, once the run's last write
is done. A page the account already watched before Mark first wrote to it in the
run stays watched, and so does a page an earlier run left watched: Confluence
does not say why an account watches a page. Where the account can be changed,
turning off its own preference to watch what it creates or edits does the same
with no requests, and `mark page unwatch --recursive` stops it watching pages it
already does.

**NOTE**: See [Preserving Inline Comments](docs/publishing.md#preserving-inline-comments) for a detailed description of the `--preserve-comments` flag.

**NOTE**: The system specific locations are described in here:
<https://pkg.go.dev/os#UserConfigDir>.
Currently, these are:
On Unix systems, it returns $XDG_CONFIG_HOME as specified by https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html if non-empty, else $HOME/.config. On Darwin, it returns $HOME/Library/Application Support. On Windows, it returns %AppData%. On Plan 9, it returns $home/lib.
Where none of these is set, as in a minimal container or a systemd unit, there is
no default file, and one is read only if `--config` or `MARK_CONFIG` names it.

## Issues, Bugs & Contributions

I've started the project to solve my own problem and open sourced the solution so anyone who has a problem like me can solve it too.
I have no profits/sponsors from these projects which means I don't really prioritize working on this project in my free time.
I still check the issues and do code reviews for Pull Requests which means if you encounter a bug in
the program, you should not expect me to fix it as soon as possible, but I'll be very glad to
merge your own contributions into the project and release the new version.

I try to label all new issues, so it's easy to find a bug or a feature request to fix/implement, if
you are willing to help with the project, you can use the following labels to find issues, just make
sure to reply in the issue to let everyone know you took the issue:

* [label:feature-request](https://github.com/kovetskiy/mark/issues?q=is%3Aissue+is%3Aopen+label%3Afeature-request)
* [label:bug](https://github.com/kovetskiy/mark/issues?q=is%3Aissue+is%3Aopen+label%3Abug)

## Contributors ✨

Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://mastodon.social/@mrueg"><img src="https://avatars.githubusercontent.com/u/489370?v=4?s=100" width="100px;" alt="Manuel Rüger"/><br /><sub><b>Manuel Rüger</b></sub></a><br /><a href="#maintenance-mrueg" title="Maintenance">🚧</a> <a href="https://github.com/kovetskiy/mark/commits?author=mrueg" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/kovetskiy"><img src="https://avatars.githubusercontent.com/u/8445924?v=4?s=100" width="100px;" alt="Egor Kovetskiy"/><br /><sub><b>Egor Kovetskiy</b></sub></a><br /><a href="#maintenance-kovetskiy" title="Maintenance">🚧</a> <a href="https://github.com/kovetskiy/mark/commits?author=kovetskiy" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://klauer.dev/"><img src="https://avatars.githubusercontent.com/u/4735?v=4?s=100" width="100px;" alt="Nick Klauer"/><br /><sub><b>Nick Klauer</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=klauern" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/rofafor"><img src="https://avatars.githubusercontent.com/u/9297850?v=4?s=100" width="100px;" alt="Rolf Ahrenberg"/><br /><sub><b>Rolf Ahrenberg</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=rofafor" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/csoutherland"><img src="https://avatars.githubusercontent.com/u/840471?v=4?s=100" width="100px;" alt="Charles Southerland"/><br /><sub><b>Charles Southerland</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=csoutherland" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/snejus"><img src="https://avatars.githubusercontent.com/u/16212750?v=4?s=100" width="100px;" alt="Šarūnas Nejus"/><br /><sub><b>Šarūnas Nejus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=snejus" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/brnv"><img src="https://avatars.githubusercontent.com/u/1925213?v=4?s=100" width="100px;" alt="Alexey Baranov"/><br /><sub><b>Alexey Baranov</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=brnv" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/princespaghetti"><img src="https://avatars.githubusercontent.com/u/2935312?v=4?s=100" width="100px;" alt="Anthony Barbieri"/><br /><sub><b>Anthony Barbieri</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=princespaghetti" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/dauc"><img src="https://avatars.githubusercontent.com/u/29129213?v=4?s=100" width="100px;" alt="Devin Auclair"/><br /><sub><b>Devin Auclair</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=dauc" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://gezimsejdiu.github.io/"><img src="https://avatars.githubusercontent.com/u/5259296?v=4?s=100" width="100px;" alt="Gezim Sejdiu"/><br /><sub><b>Gezim Sejdiu</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=GezimSejdiu" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jcavar"><img src="https://avatars.githubusercontent.com/u/3751289?v=4?s=100" width="100px;" alt="Josip Ćavar"/><br /><sub><b>Josip Ćavar</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jcavar" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Hi-Fi"><img src="https://avatars.githubusercontent.com/u/1499780?v=4?s=100" width="100px;" alt="Juho Saarinen"/><br /><sub><b>Juho Saarinen</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Hi-Fi" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/lukiffer"><img src="https://avatars.githubusercontent.com/u/2278911?v=4?s=100" width="100px;" alt="Luke Fritz"/><br /><sub><b>Luke Fritz</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=lukiffer" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/MattyRad"><img src="https://avatars.githubusercontent.com/u/1143595?v=4?s=100" width="100px;" alt="Matt Radford"/><br /><sub><b>Matt Radford</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=MattyRad" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Planktonette"><img src="https://avatars.githubusercontent.com/u/5514719?v=4?s=100" width="100px;" alt="Planktonette"/><br /><sub><b>Planktonette</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Planktonette" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="http://www.stefanoteodorani.it/"><img src="https://avatars.githubusercontent.com/u/2573389?v=4?s=100" width="100px;" alt="Stefano Teodorani"/><br /><sub><b>Stefano Teodorani</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=teopost" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/tillepille"><img src="https://avatars.githubusercontent.com/u/16536696?v=4?s=100" width="100px;" alt="Tim Schrumpf"/><br /><sub><b>Tim Schrumpf</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tillepille" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/tyler-copilot"><img src="https://avatars.githubusercontent.com/u/18539108?v=4?s=100" width="100px;" alt="Tyler Cole"/><br /><sub><b>Tyler Cole</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=tyler-copilot" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/elgreco247"><img src="https://avatars.githubusercontent.com/u/8968417?v=4?s=100" width="100px;" alt="elgreco247"/><br /><sub><b>elgreco247</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=elgreco247" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/emead-indeed"><img src="https://avatars.githubusercontent.com/u/44018145?v=4?s=100" width="100px;" alt="emead-indeed"/><br /><sub><b>emead-indeed</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=emead-indeed" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://wbhegedus.me/"><img src="https://avatars.githubusercontent.com/u/11506822?v=4?s=100" width="100px;" alt="Will Hegedus"/><br /><sub><b>Will Hegedus</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=wbh1" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/carnei-ro"><img src="https://avatars.githubusercontent.com/u/42899277?v=4?s=100" width="100px;" alt="Leandro Carneiro"/><br /><sub><b>Leandro Carneiro</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=carnei-ro" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/beeme1mr"><img src="https://avatars.githubusercontent.com/u/682996?v=4?s=100" width="100px;" alt="beeme1mr"/><br /><sub><b>beeme1mr</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=beeme1mr" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Taldrain"><img src="https://avatars.githubusercontent.com/u/1081600?v=4?s=100" width="100px;" alt="Taldrain"/><br /><sub><b>Taldrain</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Taldrain" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="http://www.devin.com.br/"><img src="https://avatars.githubusercontent.com/u/349457?v=4?s=100" width="100px;" alt="Hugo Cisneiros"/><br /><sub><b>Hugo Cisneiros</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=eitchugo" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jevfok"><img src="https://avatars.githubusercontent.com/u/54530686?v=4?s=100" width="100px;" alt="jevfok"/><br /><sub><b>jevfok</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jevfok" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://dev.to/mmiranda"><img src="https://avatars.githubusercontent.com/u/16670310?v=4?s=100" width="100px;" alt="Mateus Miranda"/><br /><sub><b>Mateus Miranda</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=mmiranda" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/Skeeve"><img src="https://avatars.githubusercontent.com/u/725404?v=4?s=100" width="100px;" alt="Stephan Hradek"/><br /><sub><b>Stephan Hradek</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=Skeeve" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="http://huangx.in/"><img src="https://avatars.githubusercontent.com/u/353644?v=4?s=100" width="100px;" alt="Dreampuf"/><br /><sub><b>Dreampuf</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=dreampuf" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/JAndritsch"><img src="https://avatars.githubusercontent.com/u/190611?v=4?s=100" width="100px;" alt="Joel Andritsch"/><br /><sub><b>Joel Andritsch</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=JAndritsch" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/guoweis-outreach"><img src="https://avatars.githubusercontent.com/u/639243?v=4?s=100" width="100px;" alt="guoweis-outreach"/><br /><sub><b>guoweis-outreach</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=guoweis-outreach" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/klysunkin"><img src="https://avatars.githubusercontent.com/u/2611187?v=4?s=100" width="100px;" alt="klysunkin"/><br /><sub><b>klysunkin</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=klysunkin" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/EppO"><img src="https://avatars.githubusercontent.com/u/6111?v=4?s=100" width="100px;" alt="Florent Monbillard"/><br /><sub><b>Florent Monbillard</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=EppO" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/jfreeland"><img src="https://avatars.githubusercontent.com/u/30938344?v=4?s=100" width="100px;" alt="Joey Freeland"/><br /><sub><b>Joey Freeland</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=jfreeland" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/prokod"><img src="https://avatars.githubusercontent.com/u/877414?v=4?s=100" width="100px;" alt="Noam Asor"/><br /><sub><b>Noam Asor</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=prokod" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/PhilippReinke"><img src="https://avatars.githubusercontent.com/u/81698819?v=4?s=100" width="100px;" alt="Philipp"/><br /><sub><b>Philipp</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=PhilippReinke" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/vpommier"><img src="https://avatars.githubusercontent.com/u/8139328?v=4?s=100" width="100px;" alt="Pommier Vincent"/><br /><sub><b>Pommier Vincent</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=vpommier" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/ToruKawaguchi"><img src="https://avatars.githubusercontent.com/u/17423222?v=4?s=100" width="100px;" alt="Toru Kawaguchi"/><br /><sub><b>Toru Kawaguchi</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=ToruKawaguchi" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://coaxialflutter.com/"><img src="https://avatars.githubusercontent.com/u/49793?v=4?s=100" width="100px;" alt="Will Gorman"/><br /><sub><b>Will Gorman</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=willgorman" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://zackery.dev/"><img src="https://avatars.githubusercontent.com/u/15172516?v=4?s=100" width="100px;" alt="Zackery Griesinger"/><br /><sub><b>Zackery Griesinger</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=zgriesinger" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/chrisjaimon2012"><img src="https://avatars.githubusercontent.com/u/57173930?v=4?s=100" width="100px;" alt="cc-chris"/><br /><sub><b>cc-chris</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=chrisjaimon2012" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/datsickkunt"><img src="https://avatars.githubusercontent.com/u/105289244?v=4?s=100" width="100px;" alt="datsickkunt"/><br /><sub><b>datsickkunt</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=datsickkunt" title="Code">💻</a></td>
    </tr>
    <tr>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/recrtl"><img src="https://avatars.githubusercontent.com/u/14078835?v=4?s=100" width="100px;" alt="recrtl"/><br /><sub><b>recrtl</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=recrtl" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/seletskiy"><img src="https://avatars.githubusercontent.com/u/674812?v=4?s=100" width="100px;" alt="Stanislav Seletskiy"/><br /><sub><b>Stanislav Seletskiy</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=seletskiy" title="Code">💻</a></td>
      <td align="center" valign="top" width="14.28%"><a href="https://github.com/nr18"><img src="https://avatars.githubusercontent.com/u/1660601?v=4?s=100" width="100px;" alt="Joris Conijn"/><br /><sub><b>Joris Conijn</b></sub></a><br /><a href="https://github.com/kovetskiy/mark/commits?author=nr18" title="Code">💻</a></td>
    </tr>
  </tbody>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
