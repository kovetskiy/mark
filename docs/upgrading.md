# Upgrading to v17

v17 is a major release. Most people running `mark` from the command line need a few small changes, listed first. Programs that import mark's Go packages have more to change; that part follows.

## Command line

### `mark` needs a command

`mark` with publish flags and no command no longer publishes. Run `mark publish` with the same flags.

```shell
# v16
mark -f docs/page.md
# v17
mark publish -f docs/page.md
```

`mark -f docs/page.md` fails, naming the command the option belongs to. The
global options (configuration file, connection, credentials, logging) can stay
before the command, and the environment variables and the configuration file
are read as before.

### Renamed flags, variables and configuration keys

The old names are no longer read. In the configuration file an old key is refused as unknown, and the error names its replacement.

| v16 | v17 |
| --- | --- |
| `--target-url`, `MARK_TARGET_URL`, `target-url` (publish) | `--url`, `MARK_PAGE_URL`, `url` |
| `MARK_URL` (export) | `MARK_PAGE_URL` |
| `--math-format`, `MARK_MATH_FORMAT`, `math-format` | `--math-output`, `MARK_MATH_OUTPUT`, `math-output` |

`--url`, `MARK_PAGE_URL` and `url` now name the page for **both** `publish` and `export`. A configuration file whose `url` was only meant for `mark export` now sends `mark publish` to that page as well. Likewise, a CI job that sets `MARK_PAGE_URL` sends every `mark publish` of the job to that one page.

### Unknown configuration keys are an error

A key in the configuration file that no command reads now fails every command that reads the file, nested tables included. Previously it was silently ignored. The error lists every unknown key on one line, with the key it is probably a typo of:

```text
unknown key in configuration file "mark.toml": "spcae" (did you mean "space"?)
```

Remove or correct the key.

### A setting on the command line beats a conflicting one from the environment or file

Before v17, a value from the environment or the configuration file could clash with a *different* flag typed on the command line. For example, `mark export --page-id 123` failed with "--page-id and --url each name a page" when `MARK_PAGE_URL` was set, and `mark export --space DOCS -r` silently exported only the URL's page. Now a setting given on the command line sets aside a conflicting one that came from the environment or the file. This covers:

- export's `--page-id`, `--url`, `--space` and `--title` against `MARK_PAGE_ID`, `MARK_PAGE_URL`, `MARK_TITLE` and their configuration keys. A set-aside page URL still provides the base URL when nothing else sets one.
- publish's `--title-from-h1` and `--title-from-filename`.
- `--watch=false` against an empty `MARK_WATCH` or `watch = ""`.
- a configured `attachments-dir` under `export --recursive`, where it is now ignored.

Two conflicting values typed on the command line, or two from the environment and the file, are still refused. The error now says where each value came from.

### Admonitions: an explicit marker, and one colour per word

**A blockquote becomes an admonition only with an explicit marker:** a word followed by a colon at the start of its first line (`> Note: …`, `> **Warning:** …`), or a comment holding only the word. `> Note that …` and a bare `> **Warn**` are now plain quotes; add the colon to keep the box.

**Each word now has one colour in every syntax**, following GitHub and, for words GitHub lacks, MkDocs Material:

| Colour (Confluence macro) | `> Word:` | `> [!WORD]` | `!!! word` (mkdocsadmonitions) |
| --- | --- | --- | --- |
| Blue (info) | note, info, important | note, important | note, info, important, abstract, summary, tldr, todo |
| Green (tip) | tip, hint | tip | tip, hint, success, check, done |
| Yellow (note) | warning, warn, attention | warning | warning, attention |
| Red (warning) | caution, danger | caution | caution, danger, error, failure, fail, missing, bug |

What changes on pages you already publish:

| You wrote | v16 | v17 | To keep the v16 colour |
| --- | --- | --- | --- |
| `> Note:` | yellow | **blue** | `> [!WARNING]` or `> Attention:` |
| `> Warn:`, `> Warning:` | red | **yellow** | `> Caution:` or `> Danger:` |
| `!!! note` | yellow | **blue** | `!!! warning` or `!!! attention` |
| `!!! warning` | red | **yellow** | `!!! danger` |

New, and published as boxes where v16 left a plain quote:

- `> Important:`, `> Hint:`, `> Attention:`, `> Caution:` and `> Danger:`;
- MkDocs `important`, `hint`, `success`, `attention`, `caution`, `danger`, `error`, `abstract`, `summary`, `tldr`, `todo`, `check`, `done`, `failure`, `fail`, `missing` and `bug`.

MkDocs types now match in any case (`!!! Note` works). `> Error: …` and `> Success: …` stay plain quotes, so quoted log lines are not turned into boxes. `!!! question`, `!!! example` and `!!! quote` stay quotes, since Confluence has no matching box. See [Admonition Words](formatting.md#admonition-words) for the full table.

### `Content-Appearance` accepts only known values

An unknown value in a `Content-Appearance` header, front matter, `--content-appearance` or `Config.ContentAppearance` is now an error. Previously the page was published full width. The accepted values are `full-width`, `default` and `max`. `fixed`, an older editor's narrow layout, is refused too: use `default`. `--changes-only` does not notice a change of appearance, so publish once without it after changing `--content-appearance`.

### `attachment://` links are plain URLs

An image or link written as `attachment://path/to/file.png` is no longer resolved to the attachment at that path. Write the path itself: `![diagram](path/to/file.png)`.

### `mark --version`

`mark --version` prints a release as `v17.0.0` (a `go install …@v17.0.0` build), a checkout build as `v17.0.0@<commit>` or a Go pseudo-version, and appends `+dirty` when the tree had changes. Adjust scripts that parse it.

### Installing

```shell
go install github.com/kovetskiy/mark/v17/cmd/mark@latest
```

Homebrew, the release archives and the `kovetskiy/mark` image are published as before.

## Go library

v17 reshapes the public API so that each package takes its settings as one options struct and a `context.Context` first. Most of the changes follow from that.

### Module path

```go
import "github.com/kovetskiy/mark/v17"
```

Every import changes from `/v16` to `/v17`.

### Running and publishing

- `mark.Run(ctx, config)` replaces `Run(config)` and `RunContext(ctx, config)`.
- `ProcessFile` is gone. Make a `mark.Publisher` with `mark.NewPublisher(ctx, api, publishConfig)`, call `Publish` for each file, and `Close` it. One publisher keeps the folder cache and the browser across files. `Close` also stops a browser or mermaid engine that is still starting.
- `mark.Cleanup`, `chrome.Cleanup`, `d2.Cleanup` and `mermaid.Cleanup` are gone: each run owns its browser and engine, and closes them.
- `mark.Config` embeds `mark.PublishConfig`, and its fields are grouped:
  - `Connection`: BaseURL, Username, Password, InsecureSkipTLSVerify.
  - `Mode`: CompileOnly, DryRun.
  - `Page`: PageID, Space, Parents, TitleFromH1, TitleFromFilename, TitleAppendGeneratedHash, ContentAppearance.
  - `Update`: MinorEdit, VersionMessage, EditLock, ChangesOnly, PreserveComments, AppendLabels, GlobalProperties.
  - `Links`: CheckLinks, CheckLinksWarnOnly.
  - `Tracking`: TrackPages, ManifestPage, ManifestPrefix, OnOrphan, OrphanUnder, NoOverwrite.
  - `Rendering`: DropH1, StripLinebreaks, the mermaid, d2 and math settings, Features, ImageAlign, AttachReferenced, IncludePath, PageHeader.
  - `Report`: OutputFormat.

  `ParentsFromPath`, `ParentsFromPathRoot`, `ContinueOnError`, `CI` and `Unwatch` sit on `Config` itself, and `Output` on `PublishConfig`. `ExportConfig` holds its connection settings in `ExportConfig.Connection`.
- `mark.UploadAttachments` replaces `attachment.ResolveAttachments`. Package `vfs` is gone: use `attachment.Opener`, `attachment.LocalOSOpener` and `attachment.LocalOS`.

### Compiling Markdown

- The package at `…/v17/markdown` is named `markdown` (it was `mark`).
- `markdown.CompileMarkdown(ctx, source, stdlib, path, markdown.Options{…})` replaces the `types.MarkConfig` argument; package `types` is gone. `DropFirstH1` and `StripNewlines` are `DropH1` and `StripLinebreaks`.
- The legacy compile path is gone: `CompileMarkdownLegacy`, `CompileMarkdownWithTransformer`, `ConfluenceLegacyExtension` and `renderer.ConfluenceBlockQuoteRenderer`.
- goldmark v2: `renderer`, `parser`, `transformer` and `markdown` take and return `github.com/yuin/goldmark/v2` types, so a program extending them moves to goldmark v2 too. Every `renderer.NewConfluence*Renderer` returns an `html.Extension`, and extensions provide `ParserOptions`/`RendererOptions` instead of `Extend`.
- Admonitions: `renderer.BlockQuoteType`, `MkDocsAdmonitionType` and the `Info`/`Note`/`Warn`/`Tip`/`None` and `A*` constants are gone; use `renderer.AdmonitionType` and `AdmonitionInfo`, `AdmonitionNote`, `AdmonitionWarning`, `AdmonitionTip`, `AdmonitionNone`. `LegacyBlockQuoteClassifier` is replaced by `renderer.ClassifyBlockQuote`.

### Diagrams and math

- `mermaid.NewEngine` returns a `*mermaid.Engine` with `Render(ctx, title, diagram, mermaid.Options{Output, Scale, Bundle})`, replacing `UseEngine` and `ProcessMermaid*`.
- `d2.Render(ctx, title, diagram, d2.Options{Output, Scale, Browser, InputPath, BundleRemote})` replaces `ProcessD2` and `ProcessD2SVG`.
- `math.Process(ctx, tex, math.Options{Display, Output, Scale, Browser})`.
- `chrome.NewBrowser` returns a `*chrome.Browser`, whose `PNGFromSVG(ctx, …)` replaces the package function. `chrome.Context` is gone.
- The new package `imageformat` (`Format`, `PNG`, `SVG`) types every output setting. `math.Format` and its constants are gone.

### Confluence client

- gopencils is replaced by an in-repo HTTP client. Every `confluence.API` method that makes a request takes `ctx` first, and so does everything above it that does: `manifest.Store`, page operations and ancestry, `LinkChecker.MissingPages`, `doctor.Run`, `stdlib.New`.
- `confluence.NewAPI(baseURL, confluence.Options{Username, Password, InsecureSkipVerify, KeepConcurrentEdits, Unwatch})`. `BaseURL` is a method.
- `UpdatePage(ctx, page, content, confluence.UpdateOptions{MinorEdit, VersionMessage, Appearance, Emoji})`.
- Errors are `*confluence.StatusError`. Match them with `errors.Is` against `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrConflict` and `ErrLoginPage`.
- `IsCloud` is gone: use `Cloud`, which also returns the error. `LookupUser` returns `*confluence.User`, and `DeletePageLabel` returns only an error.

### Options structs and named types

- `metadata.ExtractMeta(data, metadata.ExtractOptions{…})`.
- `page.Rename`/`Archive`/`Delete`/`Watch`/`Unwatch` take `RenameOptions`, `ArchiveOptions`, `DeleteOptions` and `WatchOptions`. The ancestry functions take `api` second and a trailing `page.AncestryOptions{DryRun, FolderCache, Parents, Folders}`. `page.NewLinkResolver` takes `metadata.ExtractOptions`.
- `fingerprint.Content(html, fingerprint.Properties{…})`, `doctor.Run(ctx, api, spaces, out)`.
- Named string types replace plain strings: `report.Format`, `report.Status`, `report.OnOrphan`, `metadata.Appearance`, `mermaid.EngineKind`, `confluence.ContentType`, `export.Format` and `export.Metadata`.
- `metadata.ParseContentAppearance` and `ResolveContentAppearance` replace `NormalizeContentAppearance`, and refuse unknown values.

### Moved or removed

| v16 | v17 |
| --- | --- |
| `util.Flags`, `util.RunMark`, `util.CheckFlags`, `util.CheckConfigFile` | removed; `util.Run` is the command line |
| `page.OnOrphan*`, `page.ParseOnOrphan` | `report.OnOrphan*`, `report.ParseOnOrphan` |
| `page.GenerateTinyLink`, `page.DecodeTinyLinkID` | `confluence.GenerateTinyLink`, `confluence.DecodeTinyLinkID` |
| `page.ResetFolderCache` | `page.NewFolderCache` |
| `renderer.SplitPageAnchor`, `ParseCodeBlockInfo`, `CodeLanguage`, `AdmonitionMarker`; `transformer.AnchorKey`; `parser.HeadingOptions` | internal (`internal/syntax`) |
| `metadata.CodeRegions`, `InCode`, `Region`, `IgnoreStart`, `IgnoreEnd` | internal (`internal/mdsource`) |
| `stdlib.New(api)` | `stdlib.New(ctx, users)` with a `stdlib.UserLookup`; a `*confluence.API` satisfies it |
