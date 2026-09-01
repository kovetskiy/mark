package mark

import (
	"bytes"
	// SHA-1 is used only as a content fingerprint for --changes-only, never as
	// a security primitive. The digest is embedded in the page version message
	// and matched back with a 40-hex-character regex, so widening it would stop
	// mark from recognising pages published by earlier versions.
	"crypto/sha1" //nolint:gosec // G505: non-cryptographic content fingerprint
	"encoding/hex"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/d2"
	"github.com/kovetskiy/mark/v16/includes"
	"github.com/kovetskiy/mark/v16/manifest"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/mermaid"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/rs/zerolog/log"
)

var markerRegex = regexp.MustCompile(`(?s)<ac:inline-comment-marker ac:ref="([^"]+)">(.*?)</ac:inline-comment-marker>`)

// Config holds all configuration options for running Mark.
type Config struct {
	// Connection settings
	BaseURL               string
	Username              string
	Password              string
	PageID                string
	InsecureSkipTLSVerify bool

	// File selection
	Files string

	// Behaviour
	CompileOnly     bool
	DryRun          bool
	ContinueOnError bool
	CI              bool

	// Page content
	Space                    string
	Parents                  []string
	TitleFromH1              bool
	TitleFromFilename        bool
	TitleAppendGeneratedHash bool
	ContentAppearance        string

	// Page updates
	MinorEdit          bool
	VersionMessage     string
	EditLock           bool
	ChangesOnly        bool
	PreserveComments   bool
	TrackPages         bool
	NoOverwrite        bool
	CheckLinks         []string
	CheckLinksWarnOnly bool
	AppendLabels       bool
	GlobalProperties   string
	OnOrphan           string
	OutputFormat       string
	OrphanUnder        string

	// Rendering
	DropH1          bool
	StripLinebreaks bool
	MermaidScale    float64
	D2Scale         float64
	MathFormat      string
	MathScale       float64
	Features        []string
	ImageAlign      string
	IncludePath     string

	// Output is the writer used for result output (e.g. published page URLs,
	// compiled HTML). If nil, output is discarded; the CLI sets this to
	// os.Stdout.
	Output io.Writer
}

// output returns the configured writer, falling back to io.Discard so that
// library callers that do not set Output receive no implicit stdout writes.
func (c Config) output() io.Writer {
	if c.Output != nil {
		return c.Output
	}
	return io.Discard
}

// Run processes all files matching Config.Files and publishes them to Confluence.
func Run(config Config) error {
	// Settings are checked before anything else happens. A value that cannot be
	// acted on should be said so plainly, not after a glob has been resolved
	// and a connection opened -- and least of all part way through publishing.
	//
	// A combination that would leave somebody believing they are protected is
	// refused rather than warned about. "I asked for links to be reported and
	// saw none" and "I asked not to overwrite" are both conclusions people draw
	// from silence, and silence is exactly what these produce on their own. A
	// combination that merely does nothing, and that nobody would read anything
	// into, is a warning.
	outputFormat, err := report.ParseFormat(config.OutputFormat)
	if err != nil {
		return err
	}

	onOrphan, err := page.ParseOnOrphan(config.OnOrphan)
	if err != nil {
		return err
	}

	// Removing a page needs to know mark put it there, and only the manifest
	// knows that. Without it the flag could only guess from titles, which is
	// not a guess worth making about deletion.
	if onOrphan != page.OnOrphanReport && !config.TrackPages {
		return fmt.Errorf(
			"--on-orphan %s requires --track-pages: "+
				"only the page manifest knows which pages mark published",
			onOrphan,
		)
	}

	if config.CheckLinksWarnOnly && len(config.CheckLinks) == 0 {
		return fmt.Errorf(
			"--check-links-warn-only requires --check-links: " +
				"on its own no links are checked, so the silence means nothing",
		)
	}

	if config.NoOverwrite && !config.TrackPages {
		return fmt.Errorf("--no-overwrite requires --track-pages: " +
			"the version mark last published is remembered in the page manifest")
	}

	linkChecks, err := page.ParseLinkChecks(config.CheckLinks)
	if err != nil {
		return err
	}

	api := confluence.NewAPI(config.BaseURL, config.Username, config.Password, config.InsecureSkipTLSVerify)

	// Folder resolutions are cached in a package-level map that outlives this
	// call, so a second run in the same process -- against another instance,
	// even -- would otherwise start with the first run's answers.
	page.ResetFolderCache()

	files, err := doublestar.FilepathGlob(config.Files)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		msg := "no files matched"
		if config.CI {
			log.Warn().Msg(msg)
		} else {
			return errors.New(msg)
		}
	}

	// The standard library is a fixed set of templates that does not depend on
	// the file being processed, so it is built once for the whole run rather
	// than once per file. Building it cannot reach the network -- the "user"
	// template func captures the API but is only invoked during rendering.
	std, err := stdlib.New(api)
	if err != nil {
		return fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	// Read once and before anything is published: a file that cannot be parsed
	// should not be discovered halfway through a repository.
	globalProperties, err := page.LoadGlobalProperties(config.GlobalProperties)
	if err != nil {
		return err
	}

	checker := page.NewLinkChecker(linkChecks)

	// The manifest is only consulted when asked for. It changes how an existing
	// page is found, which is not something to switch on under anyone without
	// their say-so.
	var tracker *manifest.Store
	if config.TrackPages {
		// A dry run resolves exactly as a real one does -- otherwise its preview
		// is fiction -- and resolving records what it finds. The store it gets
		// cannot write, which is a guard that holds however it is used.
		if config.DryRun {
			tracker = manifest.NewReadOnlyStore(api)
		} else {
			tracker = manifest.NewStore(api)
		}
		if config.PageID != "" {
			// The mapping is keyed on a source path within a space, and neither
			// is known when publishing straight to a page id. Better said once
			// than discovered later as a manifest with holes in it.
			log.Warn().Msg(
				"--track-pages has no effect together with a page id: " +
					"the mapping is per space and per file, and neither applies here",
			)
			tracker = nil
		}
	}

	// What has been recorded has already been published, so the mapping is
	// worth keeping however the run ends -- including when it ends early.
	// Returning on the first failing file used to skip the save entirely,
	// throwing away the mapping for every page that had published perfectly
	// well, and the next run then resolved all of them by title alone: no
	// rename detection, and no version baseline for --no-overwrite.
	//
	// Deferred rather than repeated before each return, because there are five
	// of them and the next one added would miss it too. The explicit save at
	// the end stays: it is the one whose failure the caller hears about, and
	// once it succeeds this becomes a no-op.
	defer func() {
		if tracker == nil {
			return
		}
		if err := tracker.Save(); err != nil {
			log.Error().Err(err).Msg("unable to save page manifest")
		}
	}()

	// A nil *manifest.Store put into a non-nil interface is still a non-nil
	// interface, so page would see tracking as enabled and call through a nil
	// receiver. Build the interface value only when there is a store behind it.
	var ancestryTracker page.AncestryTracker
	if tracker != nil {
		ancestryTracker = tracker
		// The whole file set is known before any of it is processed, which is
		// what lets a recorded path missing from the run be read as a rename
		// rather than a guess made one document at a time.
		tracker.SetRunFiles(config.Files, files)
	}

	// A link is resolved by finding the page it points at, so a document linking
	// to another this run is about to create has nothing to find. Collected
	// while publishing and dealt with afterwards.
	//
	// Not on a dry run or a compile: nothing is published, so nothing a link is
	// waiting for will come to exist, and the wait is worth reporting instead.
	var deferrals *page.Deferrals
	if !config.DryRun && !config.CompileOnly {
		deferrals = page.NewDeferrals()
	}

	// What the run did, for whatever is reading the output rather than the log.
	results := report.New()

	// Pages that asked for a position among their siblings, collected as they
	// publish and applied once at the end -- the order of one page only means
	// anything alongside the others.
	var ordered []page.Ordered

	var hasErrors bool
	for _, file := range files {
		log.Info().Msgf("processing %s", file)

		target, placement, err := processFile(file, api, config, std, tracker, ancestryTracker, checker, globalProperties, deferrals, results)
		if placement != nil {
			ordered = append(ordered, *placement)
		}
		if err != nil {
			results.AddPage(report.Page{
				File: file, Status: report.StatusFailed, Reason: err.Error(),
			})

			if config.ContinueOnError {
				log.Error().Err(err).Msgf("processing %s", file)
				hasErrors = true
				continue
			}

			if writeErr := results.Write(config.output(), outputFormat); writeErr != nil {
				log.Error().Err(writeErr).Msg("unable to write the run report")
			}

			return err
		}

		if target != nil {
			log.Info().Msgf("page successfully updated: %s", api.BaseURL+target.Links.Full)

			// The other formats describe the whole run at the end, where a
			// page published twice can be reported once.
			if outputFormat == report.FormatURL {
				if _, err := fmt.Fprintln(config.output(), api.BaseURL+target.Links.Full); err != nil {
					return err
				}
			}
		}
	}

	// Everything exists now, so the documents that were waiting on a page this
	// run created can be published again with their links resolved.
	if waiting := deferrals.Files(); len(waiting) > 0 && !hasErrors {
		log.Info().Msgf(
			"%d document(s) linked to pages created in this run; publishing them again",
			len(waiting),
		)

		for _, file := range waiting {
			log.Info().Msgf("processing %s again", file)

			// Nil deferrals: this is the last look, so a link that still does
			// not resolve is reported rather than waited on again.
			if _, _, err := processFile(
				file, api, config, std, tracker, ancestryTracker, checker, globalProperties, nil, results,
			); err != nil {
				if config.ContinueOnError {
					log.Error().Err(err).Msgf("processing %s", file)
					hasErrors = true

					continue
				}

				return err
			}
		}
	}

	// Once everything that was going to be published has been, an ac: link that
	// still names no page names none.
	missingPages, err := checker.MissingPages(api)
	if err != nil {
		return err
	}

	for _, item := range missingPages {
		log.Warn().Msg(item)
	}

	// The manifest is saved before the run's own outcome is decided. Returning
	// early on hasErrors used to skip it entirely, so a single bad file threw
	// away the mapping for every page that had published perfectly well --
	// worst under --continue-on-error, which exists precisely to keep going.
	// What was recorded actually happened, and is worth keeping either way.
	if !hasErrors && len(ordered) > 0 {
		// Not attempted when a file failed: the pages that did not publish are
		// missing from the sequence, and ordering the rest against each other
		// would arrange them as though the absent ones were gone for good.
		if err := page.OrderChildren(api, config.DryRun, ordered); err != nil {
			return fmt.Errorf("unable to order pages: %w", err)
		}
	}

	var saveErr error
	if tracker != nil {
		if err := handleOrphans(tracker, api, config, onOrphan, hasErrors, results); err != nil {
			return err
		}
		if saveErr = tracker.Save(); saveErr != nil {
			saveErr = fmt.Errorf("unable to save page manifest: %w", saveErr)
		}
	}

	// After the manifest is written: an unresolved link says nothing about
	// whether the pages that did publish are recorded correctly, and throwing
	// the mapping away over it would be a far worse outcome than the link.
	if len(missingPages) > 0 && !config.CheckLinksWarnOnly {
		return fmt.Errorf(
			"%s:\n  %s",
			pluraliseLinks(len(missingPages)), strings.Join(missingPages, "\n  "),
		)
	}

	if err := results.Write(config.output(), outputFormat); err != nil {
		return fmt.Errorf("unable to write the run report: %w", err)
	}

	if hasErrors {
		// The files are the more useful complaint; a failed manifest write is
		// secondary and must not displace it.
		if saveErr != nil {
			log.Error().Err(saveErr).Msg("page manifest was not saved")
		}
		return fmt.Errorf("one or more files failed to process")
	}

	return saveErr
}

// ProcessFile processes a single markdown file and publishes it to Confluence.
// Returns nil for the page info when compile-only or dry-run mode is active.
//
// Callers processing several files should prefer Run, which builds the standard
// library once instead of once per file.
func ProcessFile(file string, api *confluence.API, config Config) (*confluence.PageInfo, error) {
	std, err := stdlib.New(api)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	linkChecks, err := page.ParseLinkChecks(config.CheckLinks)
	if err != nil {
		return nil, err
	}

	globalProperties, err := page.LoadGlobalProperties(config.GlobalProperties)
	if err != nil {
		return nil, err
	}

	checker := page.NewLinkChecker(linkChecks)

	target, _, err := processFile(file, api, config, std, nil, nil, checker, globalProperties, nil, nil)
	if err != nil {
		return target, err
	}

	missing, err := checker.MissingPages(api)
	if err != nil {
		return target, err
	}
	if len(missing) > 0 && !config.CheckLinksWarnOnly {
		return target, fmt.Errorf("%s", strings.Join(missing, "\n  "))
	}
	for _, item := range missing {
		log.Warn().Msg(item)
	}

	return target, nil
}

func processFile(file string, api *confluence.API, config Config, std *stdlib.Lib, tracker *manifest.Store, ancestryTracker page.AncestryTracker, checker *page.LinkChecker, globalProperties map[string]any, deferrals *page.Deferrals, results *report.Report) (*confluence.PageInfo, *page.Ordered, error) {
	markdown, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file %q: %w", file, err)
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	// A byte-order mark is not content, and leaving it in front of the first
	// header comment makes the file look to every parser here like one with no
	// metadata at all -- reported as "doesn't contain metadata", which is not
	// where the author would look. Windows editors write one routinely.
	markdown = bytes.TrimPrefix(markdown, []byte{0xEF, 0xBB, 0xBF})

	// Fingerprint the source as read, before metadata is stripped and links are
	// substituted. Both of those depend on state outside the file -- what is
	// already published, what other files resolve to -- and a fingerprint that
	// moves with the remote would not survive the round trip it exists for.
	sourceHash := sha1Hash(string(markdown))

	frontMatterEnabled := slices.Contains(config.Features, "frontmatter")

	// Before the headers are read, so that the line numbers in any complaint
	// are the ones in the file the author is looking at rather than offsets
	// into what is left after the header block is taken off. It also means an
	// ignored region is ignored entirely, headers and all, which is what the
	// markers say on the tin.
	markdown, err = metadata.StripIgnoredBlocks(markdown)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to process %q: %w", file, err)
	}

	meta, markdown, err := metadata.ExtractMeta(
		markdown,
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		file,
		config.Parents,
		config.TitleAppendGeneratedHash,
		config.ContentAppearance,
		frontMatterEnabled,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to extract metadata from file %q: %w", file, err)
	}

	if config.PageID != "" && meta != nil {
		log.Warn().Msg(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)
		meta = nil
	}

	// Before anything is asked of Confluence: a document that has opted out
	// should cost nothing, not a page lookup and an attachment upload it will
	// not use.
	if !meta.Publish() {
		log.Info().Msgf("%s is not synchronized; leaving it alone", file)
		results.AddPage(report.Page{
			File: file, Status: report.StatusSkipped,
			Reason: "the document is not synchronized",
			Space:  spaceOf(meta), Title: titleOf(meta),
		})

		// Looked up purely to mark the path as seen. A page somebody stopped
		// synchronising is one they chose to keep, and a run that did not
		// account for the file would read it as gone: the page is reported as
		// having no source file and its mapping is dropped, so synchronising it
		// again later would no longer know which page was already its own.
		if tracker != nil && meta != nil && meta.Space != "" {
			if _, _, err := tracker.Lookup(meta.Space, file); err != nil {
				return nil, nil, fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
			}
		}

		return nil, nil, nil
	}

	if config.PageID == "" && meta == nil {
		return nil, nil, fmt.Errorf(
			"specified file doesn't contain metadata and URL is not specified " +
				"via command line or doesn't contain pageId GET-parameter",
		)
	}

	if meta != nil {
		if meta.Space == "" {
			return nil, nil, fmt.Errorf(
				"space is not set ('Space' header is not set and '--space' option is not set)",
			)
		}
		if meta.Title == "" {
			return nil, nil, fmt.Errorf(
				"page title is not set: use the 'Title' header, " +
					"or the --title-from-h1 / --title-from-filename flags",
			)
		}
	}

	// Links are rewritten while the document is being rendered, by walking the
	// parsed tree. Doing it here, over the text, meant a fenced block showing
	// Markdown syntax had its example links turned into Confluence URLs.
	// A fragment's links are written from where the fragment lives, so the
	// directory of everything this document includes is worth looking in too.
	// One level: a fragment that itself includes another is not followed here.
	searchDirs := includeSearchDirs(filepath.Dir(file), config.IncludePath, markdown)

	resolver := page.NewLinkResolver(
		api,
		meta,
		filepath.Dir(file),
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		config.Parents,
		config.TitleAppendGeneratedHash,
		frontMatterEnabled,
		checker,
		searchDirs,
	)
	resolver.SourceFile = file
	resolver.Deferrals = deferrals

	resolveLink := resolver.Resolve

	if config.DryRun {
		if meta != nil {
			if _, pg, err := page.ResolvePage(true, api, meta, ancestryTracker); err != nil {
				return nil, nil, fmt.Errorf("unable to resolve page location: %w", err)
			} else if pg == nil {
				// The title found nothing, which is where a real run consults
				// the manifest. Saying so is the whole point of a dry run:
				// otherwise it reports a new page for every rename and retitle
				// the run would actually have handled in place.
				previewTrackedResolution(tracker, api, meta, file, sourceHash)
			}
		} else if config.PageID != "" {
			if _, err := api.GetPageByID(config.PageID); err != nil {
				return nil, nil, fmt.Errorf("unable to resolve page by ID: %w", err)
			}
		}
	}

	if config.CompileOnly || config.DryRun {
		if config.DropH1 {
			log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
		}

		imageAlign, err := getImageAlign(config.ImageAlign, meta)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to determine image-align: %w", err)
		}

		cfg := types.MarkConfig{
			MermaidScale:  config.MermaidScale,
			D2Scale:       config.D2Scale,
			MathFormat:    config.MathFormat,
			MathScale:     config.MathScale,
			DropFirstH1:   config.DropH1,
			StripNewlines: config.StripLinebreaks,
			Features:      config.Features,
			ImageAlign:    imageAlign,
			IncludePath:   config.IncludePath,
			ResolveLink:   resolveLink,
		}
		html, _, err := markmd.CompileMarkdown(markdown, std, file, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to compile markdown: %w", err)
		}
		if _, err := fmt.Fprintln(config.output(), html); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}

	var target *confluence.PageInfo
	var pageCreated bool
	// A page whose title changed has to be written even when its content did
	// not, or --changes-only would leave it under the old title forever.
	var titleChanged bool

	if meta != nil {
		// Parents are named by title, so a parent that has itself been renamed
		// leaves this document pointing at a name nothing carries any more.
		// Do this before resolution, so ancestry is walked against titles that
		// exist rather than creating an empty page under the old one.
		if err := refreshStaleParents(tracker, api, meta); err != nil {
			return nil, nil, err
		}

		parent, pg, err := page.ResolvePage(false, api, meta, ancestryTracker)
		if err != nil {
			return nil, nil, fmt.Errorf("error resolving page %q: %w", meta.Title, err)
		}

		// The title lookup found nothing, which is how both "this page is new"
		// and "this page was renamed" look. Ask the manifest which of the two
		// this is before creating a second page beside the first.
		if pg == nil {
			pg, err = resolveTrackedPage(tracker, api, meta, file)
			if err != nil {
				return nil, nil, err
			}
			if pg == nil {
				// Not published under this path before. It may have been
				// published under another: a renamed file is a new key holding
				// an old document.
				pg, err = resolveRenamedFile(tracker, api, meta, file, sourceHash)
				if err != nil {
					return nil, nil, err
				}
			}
			if pg != nil && pg.Title != meta.Title {
				pg.Title = meta.Title
				titleChanged = true
			}
		}

		if pg == nil {
			if parent != nil && parent.Type == "folder-parent" {
				pg, err = api.CreatePageWithFolderParent(meta.Space, meta.Type, parent.ID, meta.Title, ``)
			} else {
				pg, err = api.CreatePage(meta.Space, meta.Type, parent, meta.Title, ``)
			}
			if err != nil {
				return nil, nil, fmt.Errorf("can't create %s %q: %w", meta.Type, meta.Title, err)
			}
			// A delay between the create and update call helps mitigate a 409
			// conflict that can occur when attempting to update a page just
			// after it was created. See issues/139.
			time.Sleep(1 * time.Second)
			pageCreated = true
		} else if parent != nil && parent.Type == "folder-parent" {
			if err := page.EnsurePageUnderFolderParent(api, pg, parent.ID); err != nil {
				return nil, nil, fmt.Errorf("error relocating page %q: %w", meta.Title, err)
			}
		} else if parent != nil && !page.UnderDeclaredParents(pg, meta.Parents) {
			// A page resolved through the manifest rather than by title never
			// passed the ancestry check, because that check starts from a title
			// lookup that just missed. So an edit changing the title and the
			// parent together would retitle the page and leave it where it was.
			if err := page.EnsurePageUnderParent(api, pg, parent.ID); err != nil {
				return nil, nil, fmt.Errorf("error relocating page %q: %w", meta.Title, err)
			}
		}

		// Relocating refreshes the page from Confluence, which still carries the
		// old title -- the retitle above is staged and not published until the
		// update below. Without this it is overwritten by the move and the page
		// keeps its old name, silently and only when both changed at once.
		if titleChanged && pg != nil {
			pg.Title = meta.Title
		}

		target = pg
	} else {
		pg, err := api.GetPageByID(config.PageID)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		if pg == nil {
			return nil, nil, fmt.Errorf("page with id %q not found", config.PageID)
		}
		target = pg
	}

	if target != nil && target.Type == "" {
		if meta != nil && meta.Type != "" {
			target.Type = meta.Type
		} else {
			target.Type = "page"
		}
	}

	// Checked here, before anything is written: attachments are uploaded a few
	// lines below, and there is no point sending them for a page that is about
	// to be left alone.
	if config.NoOverwrite && !pageCreated && tracker != nil && meta != nil && target != nil {
		drifted, recorded, err := hasDrifted(tracker, meta.Space, file, target)
		if err != nil {
			return nil, nil, err
		}

		if drifted {
			log.Warn().Msgf(
				"page %q was edited in Confluence since mark published it "+
					"(version %d, mark wrote %d); leaving it alone",
				target.Title, target.Version.Number, recorded,
			)

			results.AddPage(report.Page{
				File: file, Status: report.StatusSkipped,
				Reason: fmt.Sprintf(
					"edited in Confluence since mark published it (version %d, mark wrote %d)",
					target.Version.Number, recorded,
				),
				Space: spaceOf(meta), Title: target.Title,
				PageID: target.ID, URL: api.BaseURL + target.Links.Full,
			})

			// Recorded, so the page still counts as seen and is not mistaken
			// for an orphan and deleted. The version is deliberately not
			// updated: until somebody resolves the difference, every run should
			// say so again.
			if err := tracker.Record(meta.Space, file, target.ID, target.Title, sourceHash); err != nil {
				return nil, nil, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
			}

			return target, nil, nil
		}
	}

	// Collect attachments declared via <!-- Attachment: --> directives.
	var declaredAttachments []string
	if meta != nil {
		declaredAttachments = meta.Attachments
	}

	localAttachments, err := attachment.ResolveLocalAttachments(
		vfs.LocalOS,
		filepath.Dir(file),
		declaredAttachments,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	// The page's remote attachment list is fetched once and threaded through
	// both resolve passes. Attachments are resolved twice per page -- declared
	// attachments here, then diagrams discovered while rendering -- and each
	// pass previously issued its own paginated fetch of the same list.
	remoteAttachments, err := api.GetAttachments(target.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	attaches, remoteAttachments, err := attachment.ResolveAttachmentsWithRemotes(
		api, target, localAttachments, remoteAttachments,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	attachmentLinks := attachment.NewResolver(attaches)

	if config.DropH1 {
		log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
	}

	imageAlign, err := getImageAlign(config.ImageAlign, meta)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to determine image-align: %w", err)
	}

	cfg := types.MarkConfig{
		MermaidScale:  config.MermaidScale,
		D2Scale:       config.D2Scale,
		MathFormat:    config.MathFormat,
		MathScale:     config.MathScale,
		DropFirstH1:   config.DropH1,
		StripNewlines: config.StripLinebreaks,
		Features:      config.Features,
		ImageAlign:    imageAlign,
		IncludePath:   config.IncludePath,
		ResolveLink:   resolveLink,

		ResolveAttachment: attachmentLinks.Resolve,
	}

	html, inlineAttachments, err := markmd.CompileMarkdown(markdown, std, file, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to compile markdown: %w", err)
	}

	if err := reportBrokenLinks(resolver.Broken(), file, config.CheckLinksWarnOnly); err != nil {
		return nil, nil, err
	}

	// Only knowable once the document has been walked.
	for _, unused := range attachmentLinks.Unused(attaches) {
		log.Warn().Msgf("unused attachment: %s", unused)
	}

	if _, _, err = attachment.ResolveAttachmentsWithRemotes(
		api, target, inlineAttachments, remoteAttachments,
	); err != nil {
		return nil, nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	var layout, sidebar string
	var labels []string
	var contentAppearance, emoji string

	if meta != nil {
		layout = meta.Layout
		sidebar = meta.Sidebar
		labels = meta.Labels
		contentAppearance = meta.ContentAppearance
		emoji = meta.Emoji
	}

	{
		var buffer bytes.Buffer
		err := std.Templates.ExecuteTemplate(
			&buffer,
			"ac:layout",
			struct {
				Layout  string
				Sidebar string
				Body    string
			}{
				Layout:  layout,
				Sidebar: sidebar,
				Body:    html,
			},
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to execute layout template: %w", err)
		}
		html = buffer.String()
	}

	var finalVersionMessage string
	shouldUpdatePage := true

	if config.ChangesOnly {
		contentHash := sha1Hash(html)
		log.Debug().Msgf("content hash: %s", contentHash)

		if previous := readContentHash(target.Version.Message); previous != "" {
			log.Debug().Msgf("previous content hash: %s", previous)
			if previous == contentHash {
				if titleChanged {
					log.Info().Msgf("page %q is unchanged but is being retitled", target.Title)
				} else {
					log.Info().Msgf("page %q is already up to date", target.Title)
					shouldUpdatePage = false
				}
			}
		}

		finalVersionMessage = formatVersionMessage(config.VersionMessage, contentHash)
	} else {
		finalVersionMessage = config.VersionMessage
	}

	// Only fetch the old body and inline comments when we know the page will
	// actually be updated. This avoids unnecessary API round-trips for no-op
	// runs (e.g. when --changes-only determines the content is unchanged).
	if shouldUpdatePage && config.PreserveComments && !pageCreated {
		pg, err := api.GetPageByIDExpanded(target.ID, "ancestors,version,body.storage")
		if err != nil {
			return nil, nil, fmt.Errorf("unable to retrieve page body for comments: %w", err)
		}
		target = pg

		// The refetch carries the title Confluence holds now, which is the old
		// one whenever this run is renaming the page: the new title is staged
		// on the object that was just replaced and is not published until the
		// update below. Without this the rename is dropped silently, and the
		// manifest goes on to record a title the page does not carry -- which
		// then misresolves every parent that names it.
		if titleChanged && meta != nil {
			target.Title = meta.Title
		}

		comments, err := api.GetInlineComments(target.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to retrieve inline comments: %w", err)
		}

		html, err = mergeComments(html, target.Body.Storage.Value, comments)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to merge inline comments: %w", err)
		}
	}

	if tracker != nil && meta != nil {
		if err := tracker.Record(meta.Space, file, target.ID, meta.Title, sourceHash); err != nil {
			return nil, nil, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
		}
	}

	status := report.StatusPublished
	if !shouldUpdatePage {
		status = report.StatusUnchanged
	}
	results.AddPage(report.Page{
		File: file, Status: status,
		Space: spaceOf(meta), Title: target.Title,
		PageID: target.ID, URL: api.BaseURL + target.Links.Full,
	})

	if shouldUpdatePage {
		// Checked here rather than anywhere earlier because this is the body
		// that is actually sent: after the layout wrap, and after any inline
		// comments have been merged back into it.
		if err := markmd.CheckWellFormed(html); err != nil {
			return nil, nil, err
		}

		err = api.UpdatePage(
			target,
			html,
			config.MinorEdit,
			finalVersionMessage,
			contentAppearance,
			emoji,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to update page: %w", err)
		}
	}

	// What --no-overwrite compares against on the next run. UpdatePage advances
	// the version it was given, so this reads the same either way: a run that
	// wrote nothing still records what it found, so that a page nobody touches
	// gets a version on the first run rather than staying unguarded forever.
	if tracker != nil && meta != nil {
		if err := tracker.RecordVersion(meta.Space, file, target.Version.Number); err != nil {
			return nil, nil, fmt.Errorf("unable to record page version for %q: %w", file, err)
		}
	}

	if meta != nil {
		if err := updateLabels(api, target, labels, config.AppendLabels); err != nil {
			return nil, nil, err
		}
	}

	var documentProperties map[string]any
	if meta != nil {
		documentProperties = meta.Properties
	}

	// No dry-run branch here: a dry run returns long before this, when the
	// compiled page would have been printed.
	if err := page.ApplyProperties(
		api, target.ID,
		page.MergeProperties(globalProperties, documentProperties),
	); err != nil {
		return nil, nil, err
	}

	// Where this page asked to sit among its siblings. Applied once the whole
	// run is known, since a position only means something next to the others.
	var placement *page.Ordered
	if meta != nil && meta.Order != nil {
		placement = &page.Ordered{
			PageID:   target.ID,
			ParentID: page.ImmediateParentID(target),
			Title:    target.Title,
			Order:    *meta.Order,
		}
	}

	if config.EditLock {
		log.Info().Msgf(
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			config.Username,
		)
		if err := api.RestrictPageUpdates(target, config.Username); err != nil {
			return nil, nil, fmt.Errorf("unable to restrict page updates: %w", err)
		}
	}

	return target, placement, nil
}

// previewTrackedResolution says what a real run would have done with a document
// the title lookup could not place.
//
// A dry run that reports a new page for every rename and retitle the real run
// would have handled in place is worse than no dry run, because it is confidently
// wrong about the one thing somebody is checking.
func previewTrackedResolution(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
	sourceHash string,
) {
	if tracker == nil || meta == nil {
		return
	}

	if pg, err := resolveTrackedPage(tracker, api, meta, file); err == nil && pg != nil {
		log.Info().Msgf(
			"%s would be published to the existing page %s, retitled from %q to %q",
			file, pg.ID, pg.Title, meta.Title,
		)
		return
	}

	if pg, err := resolveRenamedFile(tracker, api, meta, file, sourceHash); err == nil && pg != nil {
		log.Info().Msgf(
			"%s would be treated as a rename of an already published document, updating page %s",
			file, pg.ID,
		)
		return
	}

	log.Info().Msgf("%s would be published as a new page %q", file, meta.Title)
}

// resolveTrackedPage returns the page this file published to on a previous run,
// for the case where looking the page up by title found nothing.
//
// That lookup failing is ambiguous: the page may be new, or it may be the same
// page under a title that has since changed -- through the Title header, the
// leading H1, or a rename feeding --title-from-filename. Without the manifest
// the two are indistinguishable and mark publishes a duplicate.
//
// Returning (nil, nil) means "no answer, carry on and create", which covers both
// a file that has genuinely never been published and one whose recorded page has
// since been deleted in Confluence. Neither is a failure worth stopping for.
func resolveTrackedPage(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
) (*confluence.PageInfo, error) {
	if tracker == nil || meta == nil {
		return nil, nil
	}

	entry, ok, err := tracker.Lookup(meta.Space, file)
	if err != nil {
		return nil, fmt.Errorf("unable to read page mapping for %q: %w", file, err)
	}
	if !ok {
		return nil, nil
	}

	pg, err := api.GetPageByID(entry.PageID)
	if err != nil {
		// Only a page that is genuinely gone may fall through to being created
		// again. Treating every failure that way would mean a network blip
		// publishes a duplicate -- precisely the outcome this exists to prevent
		// -- so anything else stops the run and is reported.
		if !errors.Is(err, confluence.ErrNotFound) {
			return nil, fmt.Errorf(
				"unable to load page %s recorded for %q: %w", entry.PageID, file, err,
			)
		}
		log.Warn().Msgf(
			"%s was published to page %s, which no longer exists; a new page will be created",
			file, entry.PageID,
		)
		return nil, nil
	}

	if pg.Title != meta.Title {
		log.Info().Msgf(
			"%s was published as %q and is now %q; retitling page %s rather than creating a second one",
			file, pg.Title, meta.Title, pg.ID,
		)
	}

	return pg, nil
}

// refreshStaleParents rewrites parent titles that no longer resolve to the
// titles those pages carry now.
//
// mark names a parent by title and creates it when the title is not found, so
// renaming a page that others declare as their parent strands them: an empty
// page appears under the old title and the real one's children are moved
// beneath it. Tracking makes that more likely rather than less, because the
// parent is now renamed in place instead of being duplicated somewhere visible.
//
// Two records are consulted, in order of how precisely each answers the
// question. The chain a document declares was recorded when it last resolved,
// so it names the page that held this exact position -- including a parent
// nobody publishes from the repository, which is what most parents are. Failing
// that, a title mark itself published is followed, and only when exactly one
// page was published under it. Anything ambiguous, or a title neither record
// has seen, is left exactly as written -- a parent that is genuinely meant to
// be created still is.
//
// The rewrite happens here rather than inside ancestry resolution because
// everything downstream compares titles: validation, the decision that a page
// is misplaced, and the lookup that walks the chain. Repairing the title once,
// before any of them run, leaves them all agreeing.
func refreshStaleParents(tracker *manifest.Store, api *confluence.API, meta *metadata.Meta) error {
	if tracker == nil {
		return nil
	}

	// Keys name the chain as the document declares it, which is not what
	// meta.Parents holds once an earlier entry has been rewritten. A chain
	// whose first parent was also renamed would otherwise be looked up under a
	// name it was never recorded under.
	declared := slices.Clone(meta.Parents)

	for i, title := range declared {
		// FindPage is memoised, and ancestry resolution is about to ask the
		// same question, so this costs nothing on the common path where the
		// parent is exactly where it says it is.
		existing, err := api.FindPage(meta.Space, title, "page")
		if err != nil || existing != nil {
			continue
		}

		pageID, ok, err := tracker.LookupParent(meta.Space, page.ParentPathKey(declared[:i+1]))
		if err != nil {
			return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
		}
		if !ok {
			pageID, ok, err = tracker.ResolveStaleTitle(meta.Space, title)
			if err != nil {
				return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
			}
			if !ok {
				continue
			}
		}

		renamed, err := api.GetPageByID(pageID)
		if err != nil || renamed == nil || renamed.Title == title {
			continue
		}

		log.Info().Msgf(
			"parent %q was renamed to %q; using it rather than creating a page under the old title",
			title, renamed.Title,
		)
		meta.Parents[i] = renamed.Title
	}

	return nil
}

// resolveRenamedFile returns the page a document published to under a previous
// path, for a document whose own path is not recorded.
//
// A rename is not visible as an event. It shows up as one path that has stopped
// appearing and another that has started, and the only thing tying them
// together is what the file contains -- which is how git recovers renames too,
// after the fact and by similarity rather than by being told.
//
// Returning (nil, nil) means no confident answer, and a new page is created.
// That is the right way to be wrong here: a duplicate is a nuisance, where
// rebinding onto the wrong page overwrites a document nobody asked to change.
func resolveRenamedFile(
	tracker *manifest.Store,
	api *confluence.API,
	meta *metadata.Meta,
	file string,
	sourceHash string,
) (*confluence.PageInfo, error) {
	if tracker == nil || meta == nil {
		return nil, nil
	}

	previous, entry, ok, err := tracker.ResolveRenamed(meta.Space, sourceHash)
	if err != nil {
		return nil, fmt.Errorf("unable to check whether %q was renamed: %w", file, err)
	}
	if !ok {
		return nil, nil
	}

	pg, err := api.GetPageByID(entry.PageID)
	if err != nil {
		if !errors.Is(err, confluence.ErrNotFound) {
			return nil, fmt.Errorf(
				"unable to load page %s recorded for %q: %w", entry.PageID, previous, err,
			)
		}
		// Recorded but gone, so there is nothing to rename. Drop the entry so it
		// stops being offered, and let the document be published afresh.
		log.Warn().Msgf("%s was published to page %s, which no longer exists", previous, entry.PageID)
		return nil, tracker.Forget(meta.Space, previous)
	}

	log.Info().Msgf(
		"%s has the content %s published as page %s; treating it as a rename rather than a new page",
		file, previous, pg.ID,
	)

	// The old path is gone. Left in place it would be reported as a deleted
	// file on every run from here on.
	if err := tracker.Forget(meta.Space, previous); err != nil {
		return nil, err
	}

	return pg, nil
}

// updateLabels brings a page's labels in line with what its document asks for.
//
// Whether that means removing the others is the caller's choice. A page's
// labels are not only mark's to decide: teams label pages in Confluence to
// drive macros, searches and reports, and removing a label somebody added there
// destroys work no document ever mentioned. Appending leaves them, at the cost
// of a label outliving the Label header that introduced it -- which is visible
// and reversible, unlike the deletion.
func updateLabels(api *confluence.API, target *confluence.PageInfo, metaLabels []string, appendOnly bool) error {
	labelInfo, err := api.GetPageLabels(target, "global")
	if err != nil {
		return err
	}

	log.Debug().Msg("Page Labels:")
	log.Debug().Interface("labels", labelInfo.Labels).Send()
	log.Debug().Msg("Meta Labels:")
	log.Debug().Interface("labels", metaLabels).Send()

	var delLabels []string
	if !appendOnly {
		delLabels = determineLabelsToRemove(labelInfo, metaLabels)
	}
	log.Debug().Msg("Del Labels:")
	log.Debug().Interface("labels", delLabels).Send()

	addLabels := determineLabelsToAdd(metaLabels, labelInfo)
	log.Debug().Msg("Add Labels:")
	log.Debug().Interface("labels", addLabels).Send()

	if len(addLabels) > 0 {
		if _, err = api.AddPageLabels(target, addLabels); err != nil {
			return fmt.Errorf("error adding labels: %w", err)
		}
	}

	for _, label := range delLabels {
		if _, err = api.DeletePageLabel(target, label); err != nil {
			return fmt.Errorf("error deleting label %q: %w", label, err)
		}
	}

	return nil
}

func determineLabelsToRemove(labelInfo *confluence.LabelInfo, metaLabels []string) []string {
	var labels []string
	for _, label := range labelInfo.Labels {
		if !slices.ContainsFunc(metaLabels, func(metaLabel string) bool {
			return strings.EqualFold(metaLabel, label.Name)
		}) {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

func determineLabelsToAdd(metaLabels []string, labelInfo *confluence.LabelInfo) []string {
	var labels []string
	for _, metaLabel := range metaLabels {
		if !slices.ContainsFunc(labelInfo.Labels, func(label confluence.Label) bool {
			return strings.EqualFold(label.Name, metaLabel)
		}) {
			labels = append(labels, metaLabel)
		}
	}
	return labels
}

func getImageAlign(align string, meta *metadata.Meta) (string, error) {
	if meta != nil && meta.ImageAlign != "" {
		align = meta.ImageAlign
	}

	if align != "" {
		align = strings.ToLower(strings.TrimSpace(align))
		if align != "left" && align != "center" && align != "right" {
			return "", fmt.Errorf(
				`unknown image-align %q, expected one of: left, center, right`,
				align,
			)
		}
		return align, nil
	}

	return "", nil
}

func sha1Hash(input string) string {
	h := sha1.New() //nolint:gosec // G401: see the crypto/sha1 import comment
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

var (
	contentHashLeading  = regexp.MustCompile(`^\[v([a-f0-9]{40})]`)
	contentHashTrailing = regexp.MustCompile(`\[v([a-f0-9]{40})]$`)
)

// readContentHash recovers the fingerprint formatVersionMessage wrote, or "" if
// the message carries none.
//
// Both layouts have to be read: mark writes the tag first, but pages stamped
// before that change carry it last, and if those stopped matching then the
// first run after upgrading would rewrite every page in every space once --
// exactly what --changes-only exists to prevent.
//
// Both are anchored. Matching the tag anywhere would also read one the operator
// happened to put in --version-message -- someone quoting a previous message,
// say -- and prefer it over the real one. The consequence is mild, an update
// that was not needed rather than a change that was missed, but the position is
// free information and there is no reason to discard it.
//
// Leading wins, because that is what mark writes now; trailing is only
// consulted for pages it has not yet restamped.
func readContentHash(message string) string {
	if m := contentHashLeading.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	if m := contentHashTrailing.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	return ""
}

// formatVersionMessage puts the content fingerprint in front of the operator's
// message.
//
// Confluence bounds the version message, and it used to be appended -- so a
// long --version-message (a commit subject, say) could push the tag past the
// limit and have it truncated away. Nothing failed loudly: the tag simply never
// matched on the next run, and --changes-only quietly degraded to updating
// every page every time, which is precisely the failure nobody notices.
//
// Leading with the tag means truncation eats the operator's prose, which is
// recoverable and visible, instead of the machinery that has to round-trip.
func formatVersionMessage(message, contentHash string) string {
	tag := fmt.Sprintf("[v%s]", contentHash)
	if message == "" {
		return tag
	}
	return tag + " " + message
}

// htmlEscapeText escapes only the characters that Confluence storage HTML
// always encodes in text nodes (&, <, >). Unlike html.EscapeString it does NOT
// escape single-quotes or double-quotes, because those are frequently left
// unescaped inside text nodes by the Confluence editor and by mark's own
// renderer, so escaping them would prevent the selection-search from finding
// a valid match.
var htmlTextReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func htmlEscapeText(s string) string {
	return htmlTextReplacer.Replace(s)
}

// truncateSelection returns a truncated preview of s for use in log messages,
// capped at maxRunes runes, with an ellipsis appended when trimmed.
func truncateSelection(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// contextBefore returns up to maxBytes of s ending at byteEnd, trimmed
// forward to the nearest valid UTF-8 rune start so the slice is never
// split across a multi-byte sequence.
func contextBefore(s string, byteEnd, maxBytes int) string {
	start := byteEnd - maxBytes
	if start < 0 {
		start = 0
	}
	for start < byteEnd && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:byteEnd]
}

// contextAfter returns up to maxBytes of s starting at byteStart, trimmed
// back to the nearest valid UTF-8 rune start so the slice is never split
// across a multi-byte sequence.
func contextAfter(s string, byteStart, maxBytes int) string {
	end := byteStart + maxBytes
	if end >= len(s) {
		return s[byteStart:]
	}
	for end > byteStart && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[byteStart:end]
}

func levenshteinDistance(s1, s2 string) int {
	r1 := []rune(s1)
	r2 := []rune(s2)

	if len(r1) == 0 {
		return len(r2)
	}
	if len(r2) == 0 {
		return len(r1)
	}

	// Use two rolling rows instead of a full matrix to reduce allocations
	// from O(m×n) to O(n). Swap r1/r2 so r2 is the shorter string, keeping
	// the row width (len(r2)+1) as small as possible.
	if len(r1) < len(r2) {
		r1, r2 = r2, r1
	}

	prev := make([]int, len(r2)+1)
	curr := make([]int, len(r2)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(r1); i++ {
		curr[0] = i
		for j := 1; j <= len(r2); j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(r2)]
}

type commentContext struct {
	before string
	after  string
}

// mergeComments re-embeds inline comment markers from the Confluence API into
// newBody (the updated storage HTML about to be uploaded). It extracts context
// from each existing marker in oldBody and uses Levenshtein distance to
// relocate each marker to the best-matching position in newBody, so comment
// threads survive page edits even when the surrounding text has shifted.
//
// At most maxCandidates occurrences of each selection are evaluated with
// Levenshtein distance; further occurrences are ignored to bound CPU cost on
// pages where a selection is short or very common.
const maxCandidates = 100

// contextWindowBytes is the number of bytes of surrounding text captured as
// context around each inline-comment marker. It is used both when extracting
// context from oldBody and when scoring candidates in newBody.
const contextWindowBytes = 100

func mergeComments(newBody string, oldBody string, comments *confluence.InlineComments) (string, error) {
	if comments == nil {
		return newBody, nil
	}
	// 1. Extract context for each comment from oldBody
	contexts := make(map[string]commentContext)
	matches := markerRegex.FindAllStringSubmatchIndex(oldBody, -1)
	for _, match := range matches {
		ref := oldBody[match[2]:match[3]]
		// context around the tag
		before := contextBefore(oldBody, match[0], contextWindowBytes)
		after := contextAfter(oldBody, match[1], contextWindowBytes)
		contexts[ref] = commentContext{
			before: before,
			after:  after,
		}
	}

	type replacement struct {
		start     int
		end       int
		ref       string
		selection string
	}
	var replacements []replacement
	seenRefs := make(map[string]bool)

	for _, comment := range comments.Results {
		if comment.Extensions.Location != "inline" {
			log.Debug().
				Str("location", comment.Extensions.Location).
				Str("ref", comment.Extensions.InlineProperties.MarkerRef).
				Msg("comment ignored during inline marker merge: not an inline comment")
			continue
		}

		ref := comment.Extensions.InlineProperties.MarkerRef
		selection := comment.Extensions.InlineProperties.OriginalSelection

		if seenRefs[ref] {
			// Multiple results share the same MarkerRef (e.g. threaded replies).
			// The marker only needs to be inserted once; skip duplicates.
			continue
		}
		// Mark ref as seen immediately so subsequent results for the same ref
		// (threaded replies) are always deduplicated, even if this one is dropped.
		seenRefs[ref] = true

		if selection == "" {
			log.Warn().
				Str("ref", ref).
				Msg("inline comment skipped: original selection is empty; comment will be lost")
			continue
		}

		ctx, hasCtx := contexts[ref]

		// Build the list of forms to search for in newBody. The escaped form
		// is tried first (normal XML text nodes). The raw form is appended as a
		// fallback for text inside CDATA-backed macro bodies (e.g. ac:code),
		// where < and > are stored unescaped inside <![CDATA[...]]>.
		escapedSelection := htmlEscapeText(selection)
		searchForms := []string{escapedSelection}
		if selection != escapedSelection {
			searchForms = append(searchForms, selection)
		}

		var bestStart = -1
		var bestEnd = -1
		var minDistance = 1000000

		// Iterate over search forms; stop as soon as we have a definitive best.
		candidates := 0
		stopSearch := false
		for _, form := range searchForms {
			if stopSearch {
				break
			}
			currentPos := 0
			for {
				index := strings.Index(newBody[currentPos:], form)
				if index == -1 {
					break
				}
				start := currentPos + index
				end := start + len(form)

				// Skip candidates that start or end in the middle of a multi-byte
				// UTF-8 rune; such a match would produce invalid UTF-8 output.
				if !utf8.RuneStart(newBody[start]) || (end < len(newBody) && !utf8.RuneStart(newBody[end])) {
					currentPos = start + 1
					continue
				}

				candidates++
				if candidates > maxCandidates {
					stopSearch = true
					break
				}

				if !hasCtx {
					// No context available; use the first occurrence.
					bestStart = start
					bestEnd = end
					stopSearch = true
					break
				}

				newBefore := contextBefore(newBody, start, contextWindowBytes)
				newAfter := contextAfter(newBody, end, contextWindowBytes)

				// Fast path: exact context match is the best possible result.
				if newBefore == ctx.before && newAfter == ctx.after {
					bestStart = start
					bestEnd = end
					stopSearch = true
					break
				}

				// Lower-bound pruning: Levenshtein distance is at least the
				// absolute difference in rune counts. Use rune counts (not byte
				// lengths) to match the unit levenshteinDistance operates on,
				// avoiding false skips for multibyte UTF-8 content.
				lbBefore := utf8.RuneCountInString(ctx.before) - utf8.RuneCountInString(newBefore)
				if lbBefore < 0 {
					lbBefore = -lbBefore
				}
				lbAfter := utf8.RuneCountInString(ctx.after) - utf8.RuneCountInString(newAfter)
				if lbAfter < 0 {
					lbAfter = -lbAfter
				}
				if lbBefore+lbAfter >= minDistance {
					currentPos = start + 1
					continue
				}

				distance := levenshteinDistance(ctx.before, newBefore) + levenshteinDistance(ctx.after, newAfter)

				if distance < minDistance {
					minDistance = distance
					bestStart = start
					bestEnd = end
				}

				currentPos = start + 1
			}
		}

		if bestStart != -1 {
			replacements = append(replacements, replacement{
				start:     bestStart,
				end:       bestEnd,
				ref:       ref,
				selection: selection,
			})
		} else {
			log.Warn().
				Str("ref", ref).
				Str("selection_preview", truncateSelection(selection, 50)).
				Msg("inline comment dropped: selected text not found in new body; comment will be lost")
		}
	}

	// Sort replacements from back to front to avoid offset issues.
	// Use a stable sort with ref as a tie-breaker so the ordering is
	// deterministic when two markers resolve to the same start offset.
	slices.SortStableFunc(replacements, func(a, b replacement) int {
		if a.start != b.start {
			return b.start - a.start
		}
		if a.ref < b.ref {
			return -1
		}
		if a.ref > b.ref {
			return 1
		}
		return 0
	})

	// Apply replacements back-to-front. Track the minimum start of any
	// applied replacement so that overlapping candidates (whose end exceeds
	// that boundary) are dropped rather than producing nested or malformed
	// <ac:inline-comment-marker> tags.
	minAppliedStart := len(newBody)
	for _, r := range replacements {
		if r.end > minAppliedStart {
			// This replacement overlaps with an already-applied one.
			// Drop it and warn so the user knows the comment was skipped.
			log.Warn().
				Str("ref", r.ref).
				Str("selection_preview", truncateSelection(r.selection, 50)).
				Int("start", r.start).
				Int("end", r.end).
				Int("conflicting_start", minAppliedStart).
				Msg("inline comment marker dropped: selection overlaps an already-placed marker")
			continue
		}
		minAppliedStart = r.start
		selection := newBody[r.start:r.end]
		withComment := fmt.Sprintf(
			`<ac:inline-comment-marker ac:ref="%s">%s</ac:inline-comment-marker>`,
			stdhtml.EscapeString(r.ref),
			selection,
		)
		newBody = newBody[:r.start] + withComment + newBody[r.end:]
	}

	return newBody, nil
}

// Cleanup closes any shared resources (such as headless Chrome sessions).
func Cleanup() {
	d2.Cleanup()
	mermaid.Cleanup()
}

// hasDrifted reports whether a page has been changed by somebody other than
// mark since mark last published it, along with the version mark wrote.
//
// The comparison is against the version number rather than the page's content:
// Confluence rewrites storage markup on save often enough that comparing bodies
// would report a difference on pages nobody had touched.
//
// An entry with no recorded version cannot answer the question -- it was
// written before versions were tracked, or by a run without --no-overwrite --
// and a page is given the benefit of the doubt rather than frozen.
func hasDrifted(
	tracker *manifest.Store,
	spaceKey, file string,
	target *confluence.PageInfo,
) (bool, int64, error) {
	entry, ok, err := tracker.Lookup(spaceKey, file)
	if err != nil {
		return false, 0, fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
	}

	if !ok || entry.Version == 0 || entry.PageID != target.ID {
		return false, 0, nil
	}

	return target.Version.Number != entry.Version, entry.Version, nil
}

// reportBrokenLinks says what failed a link check, and decides whether it ends
// the file.
//
// Every one is named, not just the first: a page with six broken links should
// take one run to find out, not six. Whether that is fatal is the caller's
// choice, because adopting --check-links on a repository that has been
// publishing for years wants to see the list before the build starts failing
// over it.
func reportBrokenLinks(broken []string, file string, warnOnly bool) error {
	if len(broken) == 0 {
		return nil
	}

	if warnOnly {
		for _, item := range broken {
			log.Warn().Msgf("%s: %s", file, item)
		}

		return nil
	}

	summary := fmt.Sprintf("%d links do not resolve", len(broken))
	if len(broken) == 1 {
		summary = "1 link does not resolve"
	}

	return fmt.Errorf("%s:\n  %s", summary, strings.Join(broken, "\n  "))
}

// includeSearchDirs reports the directories of the files a document includes.
//
// Only the directories: what is wanted is somewhere to look for a link written
// inside a fragment, and the fragment's neighbours are what such a link names.
// Duplicates are dropped so that a document including several files from one
// directory does not have it looked in repeatedly.
func includeSearchDirs(base, includePath string, markdown []byte) []string {
	var dirs []string
	seen := map[string]bool{}

	add := func(dir string) {
		if dir == "" || dir == base || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	for _, target := range includes.DirectiveTargets(markdown) {
		add(filepath.Dir(filepath.Join(base, target)))

		// The same fallback LoadTemplate uses when the file is not beside the
		// document, so a link inside a shared fragment resolves the same way
		// the fragment itself was found.
		if includePath != "" {
			add(filepath.Dir(filepath.Join(includePath, target)))
		}
	}

	return dirs
}

// pluraliseLinks names a count of links in a sentence that reads.
func pluraliseLinks(n int) string {
	if n == 1 {
		return "1 link does not resolve"
	}

	return fmt.Sprintf("%d links do not resolve", n)
}

func spaceOf(meta *metadata.Meta) string {
	if meta == nil {
		return ""
	}

	return meta.Space
}

func titleOf(meta *metadata.Meta) string {
	if meta == nil {
		return ""
	}

	return meta.Title
}

// handleOrphans deals with the pages whose source files were not seen.
//
// Reporting, acting and forgetting are one thing because they have to agree
// about which pages are being talked about. When they were separate, a scope
// narrowed what was archived or deleted while leaving reporting to name
// everything and forgetting to drop everything -- so a page deliberately put
// out of scope was announced and then lost track of anyway.
//
// A page outside the scope is not mark's business on this run: not reported,
// not touched, and still remembered, so that a later run with a wider scope
// still knows about it.
func handleOrphans(
	tracker *manifest.Store,
	api *confluence.API,
	config Config,
	action string,
	hadErrors bool,
	results *report.Report,
) error {
	for _, space := range tracker.Spaces() {
		orphans := tracker.OrphanEntries(space)
		if len(orphans) == 0 {
			continue
		}

		if hadErrors {
			// Some files did not process, which is indistinguishable from those
			// files being gone. Saying anything now would be actively
			// misleading, and acting on it worse.
			log.Debug().Msgf(
				"space %q has %d unpublished tracked pages, not reported because this run had errors",
				space, len(orphans),
			)

			continue
		}

		scopeID, err := page.ResolveScope(api, space, config.OrphanUnder)
		if err != nil {
			return err
		}

		candidates := make([]page.Orphan, 0, len(orphans))
		for _, orphan := range orphans {
			candidate := page.Orphan{
				Path:   orphan.Path,
				PageID: orphan.Entry.PageID,
				Title:  orphan.Entry.Title,
			}

			inScope, err := page.InScope(api, scopeID, candidate)
			if err != nil {
				return err
			}

			if inScope {
				candidates = append(candidates, candidate)
			}
		}

		if len(candidates) == 0 {
			continue
		}

		paths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.Path)
		}

		log.Info().Msgf(
			"space %q: %d tracked page(s) had no matching source file in this run: %s",
			space, len(paths), strings.Join(paths, ", "),
		)

		handled := paths
		if action != page.OnOrphanReport {
			// A run that published nothing in a space says nothing about that
			// space: a failed checkout and every document having been deleted
			// look exactly alike.
			//
			// A second line of defence rather than the first -- a pattern
			// matching no files stops the run well before this -- and kept
			// because the cost of it being needed once is a space emptied by a
			// bad checkout.
			if tracker.Published(space) == 0 {
				log.Warn().Msgf(
					"space %q: nothing was published in this run, so its %d orphaned page(s) are left alone",
					space, len(candidates),
				)

				continue
			}

			handled, err = page.HandleOrphans(api, action, scopeID, candidates, config.DryRun)
			if err != nil {
				return err
			}
		}

		if config.DryRun {
			continue
		}

		// Only what was actually dealt with is forgotten. A page left alone --
		// holding children, or out of scope -- stays in the manifest so a later
		// run finds it again instead of losing sight of it.
		for _, path := range handled {
			if action != page.OnOrphanReport {
				results.AddOrphan(report.Orphan{File: path, Action: action})
			}

			if err := tracker.Forget(space, path); err != nil {
				return fmt.Errorf("unable to update page manifest for %q: %w", path, err)
			}
		}
	}

	return nil
}
