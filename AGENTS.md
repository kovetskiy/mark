# AGENTS.md

Guidance for AI coding agents (and new human contributors) working in this repository.

`mark` reads Markdown and writes **Confluence storage format** — an XML dialect, not
HTML. Most of the subtle bugs in this project's history come from forgetting that. Read
the Invariants section before changing anything under `renderer/`, `transformer/`,
`parser/`, `macro/`, `includes/`, or `stdlib/`.

## Build, test, lint

```shell
make build                 # -> ./mark  (CGO_ENABLED=0, ./cmd/mark)
make test                  # go test -race -coverprofile=profile.cov ./... -v
go test ./... -count=1     # faster loop
golangci-lint run ./...    # config in .golangci.yml; CI runs this
markdownlint-cli2          # config in .markdownlint-cli2.jsonc; CI runs this on *.md
```

Go 1.27. The module path is `github.com/kovetskiy/mark/v16` — the major version is part
of the path, so a major bump means rewriting every internal import.

`d2` and `mermaid` tests launch headless Chrome and take ~4s each; a full `go test ./...`
is dominated by them. There is currently no `-short` skip.

## Layout

| path | role |
| --- | --- |
| `cmd/mark` | thin `main`; flag wiring lives in `util/` |
| `util/` | CLI flags, config file/env sourcing, credential resolution |
| `mark.go` | orchestration: `Run` (glob → loop) and `ProcessFile` (the whole per-file pipeline) |
| `metadata/` | `<!-- Header: -->` comments and YAML front matter → `Meta` |
| `page/` | ancestry/folder resolution, relative-link rewriting, relocation |
| `confluence/` | REST client (v1 `/rest/api` + v2 `/api/v2`), page cache |
| `attachment/` | checksum, upload/update, link rewriting |
| `markdown/` | goldmark assembly; `CompileMarkdown` is the entry point |
| `parser/` | goldmark inline/block parsers (`<ac:*/>` tags, mentions, dates) |
| `transformer/` | goldmark AST transformers (macros, includes, GH alerts, details, layout, `<img>`) |
| `math/` | LaTeX → image for the `math` feature; PNG through `chrome/`, or SVG with no browser |
| `chrome/` | the one headless browser, its options, and SVG → PNG for `d2/` and `math/` |
| `renderer/` | goldmark node renderers → storage format |
| `stdlib/` | the `text/template` set that emits all `<ac:*>` markup |

Pipeline in `ProcessFile`: read → normalise CRLF → extract metadata → resolve relative
links → resolve/create page + ancestry → resolve attachments → `CompileMarkdown` →
resolve inline attachments → wrap in `ac:layout` → optionally merge inline comments →
update page → sync labels.

## Invariants

**1. Output must be well-formed XML.** Confluence rejects the whole page with
`BadRequestException` if it is not. Unbalanced tags, stray `&`, or an unescaped quote in
an attribute break the entire upload, not just one element.

**2. Escape through the `stdlib` template funcs, never by hand.**

- `xmlesc` — any interpolated text or attribute value.
- `cdata` — any text placed inside `<![CDATA[...]]>`; it splits an embedded `]]>` across
  two CDATA sections, which is the only legal way to escape it.
- `convertAttachment` — values destined for `ri:filename`; slash-flattens *and* escapes.

Titles, filenames, and `ri:content-title` have each caused a malformed-XML bug in the
past. If you add a template that interpolates user-controlled text, pipe it through one
of these.

**3. Macros and includes are expanded *before* goldmark parses.** Macro bodies routinely
contain raw `<ac:...>` XML that goldmark would escape or mangle — particularly inside
table cells. `macro.ExtractMacros` and `includes.ProcessIncludes` run on the raw bytes in
`CompileMarkdown` before the converter is built. Do not move this work into an AST
transformer without understanding why it was moved out of one.

**4. A smaller goldmark priority number wins, for parsers and renderers alike.**

- *Inline/block parsers*: a **smaller** number is tried **first**. `ConfluenceTagParser`
  uses `199` to run before goldmark's own link parser at `200`, so that `<ac:*/>` tags are
  not parsed as links.
- *Node renderers*: goldmark sorts them ascending and then calls `RegisterFuncs` from the
  **end backwards** (`renderer/renderer.go`), and each registration overwrites the last
  for a given node kind -- so the **smaller** number is the one that renders. Every
  renderer in this repo is competing with one of goldmark's: the default `html.Renderer`
  goes in at `1000` (`markdown.go`) and claims every core kind, so anything meant to
  replace it has to sit below that. Both numbers you will see are load-bearing --- the
  GH-alerts blockquote/text renderers' `200` clears the default renderer, and the
  footnote renderers' `100` has to clear the footnote extension's `500` as well.

Getting this backwards silently produces the default rendering, with no error anywhere.

**5. AST transformers cannot return errors.** goldmark's `ASTTransformer` interface has no
error return, so `transformer.PipelineTransformer` accumulates errors internally and
`CompileMarkdown` retrieves them with `Pipeline.GetError()` after `Convert`. A transformer
that fails silently without recording into the pipeline will produce a wrong page and a
zero exit code.

**6. Two compile paths must both keep working.** `CompileMarkdown` (default, GH alerts +
pipeline transformer) and `CompileMarkdownLegacy` (the original renderer set). They are
compared against each other in `markdown/transformer_comparison_test.go`.

**7. Attachment checksums are content-addressed, but rendered ones are source-addressed.**
Checksums live in the remote attachment's comment behind the `AttachmentChecksumPrefix`
(`mark:checksum:` followed by a space).
Mermaid and d2 attachments set `Checksum` from the *diagram source*, not the rendered PNG
bytes, because Chrome's output is not byte-stable across environments; `math/` does the
same with the formula, plus the format and — for a PNG — the scale, since anything that
changes the bytes has to change the name or the page keeps the attachment it had.
`ResolveAttachments` skips checksum computation when `Checksum` is already set — preserve
that.

**8. `--features` replaces the defaults, it does not add to them.** Defaults are
`mermaid,mention`. When adding a feature: register it in `markdown/markdown.go` (both
extensions if applicable), add it to the `Usage` string of the `features` flag in
`util/flags.go`, and document it in `README.md`.

**9. Everything is sequential today, and some caches rely on that.**
`page.createdFolderCache` is an unguarded map with a comment saying so, and
`confluence.API.isCloudFlag/isCloudChecked` are unsynchronised (unlike `pageCache`, which
is mutex-guarded). Anything that introduces concurrency across files must fix those first.

## Tests

Golden-file tests drive most rendering coverage. `testdata/<name>.md` is compiled and
compared against `testdata/<name>.html`; variant suffixes cover flag combinations:

- `<name>-droph1.html` — `DropFirstH1`
- `<name>-stripnewlines.html` — `StripNewlines`
- feature-specific variants, e.g. `plantuml-nofeature.html`, `inline-link-card-inlinecard.html`

Adding a `.md` fixture without its matching `.html` panics the test — the loader reads
both unconditionally. There is no `-update` flag; golden files are written by hand.

Coverage is uneven and worth knowing before you trust a green run: `page/` ~10%,
`renderer/` ~14%, `confluence/` ~19%, and `stdlib/`, `vfs/`, `cmd/mark/` at 0%. There is
no fake Confluence server, so nothing that touches the REST API is covered. If you change
`page/ancestry.go` or `confluence/api.go`, the test suite will very likely still pass.

## Conventions

- Conventional-commit subjects: `feat(scope):`, `fix(scope):`, `refactor(scope):`,
  `build(deps):`. The release changelog excludes `docs:` and `test:`.
- Errors wrap with `%w` and read bottom-up: `fmt.Errorf("unable to compile markdown: %w", err)`.
- Logging is `zerolog` via the package-level `log`; user-facing results go to
  `config.output()`, never directly to `os.Stdout`.
- Suppress a linter only with a reason attached: `//nolint:gosec // G401: ...`.
  The two existing suppressions (SHA-1 as a content fingerprint, opt-in TLS skip) are
  the model.
