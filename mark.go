package mark

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/comments"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/d2"
	"github.com/kovetskiy/mark/v16/fingerprint"
	"github.com/kovetskiy/mark/v16/header"
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
	ParentsFromPath          bool
	ParentsFromPathRoot      string
	ContentAppearance        string

	// Page updates
	MinorEdit          bool
	VersionMessage     string
	EditLock           bool
	ChangesOnly        bool
	PreserveComments   bool
	TrackPages         bool
	ManifestPage       string
	ManifestPrefix     string
	NoOverwrite        bool
	CheckLinks         []string
	CheckLinksWarnOnly bool
	AppendLabels       bool
	GlobalProperties   string
	OnOrphan           string
	OutputFormat       string
	OrphanUnder        string

	// Unwatch is read by Run, which unwatches as the run ends. ProcessFile
	// leaves it to the caller: set the API's Unwatch, and call its
	// UnwatchWritten after the last file, with a context that is not done yet,
	// or nothing is unwatched.
	Unwatch bool

	// Rendering
	DropH1           bool
	StripLinebreaks  bool
	MermaidEngine    string
	MermaidScale     float64
	MermaidOutput    string
	MermaidBundle    bool
	D2Output         string
	D2Scale          float64
	D2BundleRemote   bool
	MathFormat       string
	MathScale        float64
	Features         []string
	ImageAlign       string
	AttachReferenced bool
	IncludePath      string
	// PageHeader is a template placed at the top of every page: Markdown
	// when it has a Markdown extension, storage format otherwise.
	PageHeader string

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

// readOnly reports whether this run may leave Confluence untouched. A compile
// prints the body it would have sent and stops; a dry run reports what it would
// have done. Neither publishes anything, so neither may delete, archive or
// reorder either -- a guard that asks only about --dry-run is one --compile-only
// walks straight past.
func (c Config) readOnly() bool {
	return c.DryRun || c.CompileOnly
}

// isPositiveScale reports whether scale is a number a diagram can be multiplied
// by. Written as "> 0" rather than "not <= 0" on purpose: NaN fails every
// comparison it is given, so a check phrased the other way round lets it
// through, and an infinite scale passes one outright.
func isPositiveScale(scale float64) bool {
	return scale > 0 && !math.IsInf(scale, 0)
}

// isScaleOrDefault is isPositiveScale for a field whose zero value means the
// renderer's default.
func isScaleOrDefault(scale float64) bool {
	return scale == 0 || isPositiveScale(scale)
}

// Run processes all files matching Config.Files and publishes them to
// Confluence.
//
// Every Confluence request the run makes carries ctx, so cancelling it cuts
// short a read in flight and the backoff before retrying any request, and the
// file being published fails at its next request instead of running to the
// end. The run then stops, as it does for any file that fails, without
// starting the next; ctx is also checked between files and before the second
// pass. A write already sent is given a short grace to answer, so that what
// it did is recorded, and is not undone; a page can still be left part way
// through: created without its attachments, or updated without its labels.
// The next run completes it. A diagram being drawn is not interrupted, only
// bounded by its own render timeout. The unwatching --watch=false does as the
// run ends sees it too: pages not unwatched yet stay watched, and are named in
// a warning.
//
// Whatever the run did before it stopped stands. The page manifest and the run
// report are still written on the way out, as for any other ending; the
// manifest's save is made under a context of its own, detached from ctx, since
// a save that inherited the cancellation would be refused before it was sent
// and the mapping for every page already published would be lost with it.
func Run(ctx context.Context, config Config) (err error) {
	// The browser is shared for the life of the process and is started lazily
	// by the first diagram or formula. A library caller has no other way to
	// know it exists, so a run that started one shuts it down -- the CLI
	// included, which relies on this rather than calling Cleanup itself.
	defer Cleanup()

	return run(ctx, config)
}

// prepare checks the settings that decide how a document is rendered and what
// is done about its links, and sets up the mermaid engine they name. Run and
// ProcessFile both start here, so that a library caller publishing one file is
// held to the same settings, and told the same thing about them, as a run is:
// before this was shared, ProcessFile checked none of it, drew diagrams with
// whatever engine happened to be current rather than the one asked for, and
// published a PNG for a MermaidOutput it did not recognise.
func (c Config) prepare() (page.LinkChecks, error) {
	// The engine is chosen before anything is published, because it is built
	// lazily and shared: a diagram already drawn is not drawn again to match.
	if err := mermaid.UseEngine(c.MermaidEngine); err != nil {
		return page.LinkChecks{}, err
	}

	// Checked here as well as in the CLI, because Config is a public API and a
	// library caller reaches this without passing a flag at all. A value the
	// renderer does not know falls to its default branch, so an unrecognised
	// format publishes a PNG without a word -- taking a bundle asked for
	// alongside it down with it. An empty value is not that: it is a caller
	// that never set the field, and it means the PNG mark has always published.
	switch c.MermaidOutput {
	case "", "png", "svg":
		// ok
	default:
		return page.LinkChecks{}, fmt.Errorf(
			"invalid MermaidOutput %q: expected \"png\", \"svg\", or \"\" for the default",
			c.MermaidOutput,
		)
	}

	if c.MermaidBundle && c.MermaidOutput != "svg" {
		return page.LinkChecks{}, errors.New(
			"MermaidBundle needs MermaidOutput \"svg\": " +
				"there is nowhere in a PNG to keep the diagram's source",
		)
	}

	// The same for d2, and for the same reason: an unrecognised format falls to
	// the renderer's PNG branch, publishing one without a word about the SVG
	// that was asked for. An empty value is a caller that never set the field,
	// and means the PNG mark has always published.
	switch c.D2Output {
	case "", "png", "svg":
		// ok
	default:
		return page.LinkChecks{}, fmt.Errorf(
			"invalid D2Output %q: expected \"png\", \"svg\", or \"\" for the default",
			c.D2Output,
		)
	}

	// Zero is the field a caller never set, which for a scale is not a default
	// but a diagram rendered at nothing. Only checked where diagrams are drawn
	// at all, since a configuration that never turns d2 on carries the field
	// past every code path that reads it.
	// Not a plain "> 0": NaN fails every comparison, so NaN <= 0 is false and it
	// would pass a check written that way, and an infinite scale passes one
	// outright. Either reaches Chrome as a screenshot scale it cannot use, or
	// multiplies an SVG's size into something that is not a number of pixels.
	if slices.Contains(c.Features, "d2") && !isPositiveScale(c.D2Scale) {
		return page.LinkChecks{}, fmt.Errorf(
			"invalid D2Scale %v: expected a finite number greater than 0 with the d2 feature enabled",
			c.D2Scale,
		)
	}

	// Mermaid and math take the same numbers, with one difference: zero is
	// their default rather than an error. Mermaid is on by default, so refusing
	// zero would refuse every Config that never heard of the field; math has
	// always drawn a zero scale at its default of 2, and a caller relying on
	// that is not wrong. What neither has ever been able to draw is a negative,
	// NaN or infinite scale, and those are refused here rather than part way
	// through a run, when the first diagram reaches the browser.
	if slices.Contains(c.Features, "mermaid") && !isScaleOrDefault(c.MermaidScale) {
		return page.LinkChecks{}, fmt.Errorf(
			"invalid MermaidScale %v: expected a finite number greater than 0, or 0 for the default, with the mermaid feature enabled",
			c.MermaidScale,
		)
	}

	if slices.Contains(c.Features, "math") && !isScaleOrDefault(c.MathScale) {
		return page.LinkChecks{}, fmt.Errorf(
			"invalid MathScale %v: expected a finite number greater than 0, or 0 for the default, with the math feature enabled",
			c.MathScale,
		)
	}

	if c.CheckLinksWarnOnly && len(c.CheckLinks) == 0 {
		return page.LinkChecks{}, fmt.Errorf(
			"--check-links-warn-only requires --check-links: " +
				"on its own no links are checked, so the silence means nothing",
		)
	}

	return page.ParseLinkChecks(c.CheckLinks)
}

// The error result is named so the deferred report write below can see, and
// replace, what the run is returning. Without it the closure assigned to a
// local err that no return statement read, and a failed write went unheard.
func run(ctx context.Context, config Config) (err error) {
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

	linkChecks, err := config.prepare()
	if err != nil {
		return err
	}

	// The rest is about the run as a whole -- the report written at the end of
	// it and the manifest kept across runs -- so ProcessFile, which has
	// neither, does not ask.
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

	if config.NoOverwrite && !config.TrackPages {
		return fmt.Errorf("--no-overwrite requires --track-pages: " +
			"the version mark last published is remembered in the page manifest")
	}

	if config.ManifestPage != "" && !config.TrackPages {
		return fmt.Errorf("--manifest-page requires --track-pages: " +
			"it says where the page manifest is kept, and nothing else keeps one")
	}

	if config.ManifestPrefix != "" && config.ManifestPrefix != manifest.PropertyKeyPrefix {
		if !config.TrackPages {
			return fmt.Errorf("--manifest-prefix requires --track-pages: " +
				"it names the page manifest's properties, and nothing else keeps one")
		}
		if err := manifest.ValidatePropertyKeyPrefix(config.ManifestPrefix); err != nil {
			return fmt.Errorf("--manifest-prefix: %w", err)
		}
	}

	api := confluence.NewAPI(config.BaseURL, config.Username, config.Password, config.InsecureSkipTLSVerify)
	// An update refused as a conflict is retried against the page's current
	// version, which overwrites whatever changed it. --no-overwrite exists to
	// stop exactly that, so under it the retry goes ahead only when nobody else
	// has written to the page.
	api.KeepConcurrentEdits = config.NoOverwrite
	api.Unwatch = config.Unwatch
	// Deferred, so that a run that fails partway still unwatches what it wrote.
	// An interrupted run does not: it warns which pages stay watched.
	defer api.UnwatchWritten(ctx)

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

	// Every file would be published over the one page in turn, and only the
	// last would be left on it.
	if config.PageID != "" && len(files) > 1 {
		named := files
		if len(named) > 3 {
			named = append(slices.Clone(named[:3]), "...")
		}
		return fmt.Errorf(
			"--target-url names one page, but --files matched %d files (%s): name the one file to publish there",
			len(files), strings.Join(named, ", "),
		)
	}

	// The standard library is a fixed set of templates that does not depend on
	// the file being processed, so it is built once for the whole run rather
	// than once per file. Building it cannot reach the network -- the "user"
	// template func captures the API, and ctx for its requests, but is only
	// invoked during rendering.
	std, err := stdlib.New(ctx, api)
	if err != nil {
		return fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	// Read once and before anything is published: a file that cannot be parsed
	// should not be discovered halfway through a repository.
	globalProperties, err := page.LoadGlobalProperties(config.GlobalProperties)
	if err != nil {
		return err
	}

	pageHeader, err := header.Load(config.PageHeader, std, config.markConfig())
	if err != nil {
		return err
	}

	if err := settlePlatform(ctx, api, config); err != nil {
		return err
	}

	checker := page.NewLinkChecker(linkChecks)

	// What the run did, for whatever is reading the output rather than the log.
	results := report.New()

	// Failures the report already carries, so that they are not said twice: a
	// document's own is recorded against that document, and a manifest save
	// that failed is recorded where it failed.
	var reported []error

	// Written on the way out, whatever the way out is. It used to be written at
	// the end of a successful run only, so --output-format json produced zero
	// bytes whenever the run failed after publishing -- an unresolved link, a
	// failed ordering pass, an orphan that could not be handled -- which is
	// exactly when an account of what was published is worth having. A failed
	// write still fails the run, but only when nothing worse already has.
	//
	// Deferred before the manifest's save below, so that it runs after it and
	// a save that fails on the way out is in what it writes.
	defer func() {
		// A failure of the run as a whole belongs to no document, and was
		// never recorded anywhere: the report of a run that stopped on an
		// unresolved link listed its pages as published and said nothing of
		// why the run had failed.
		if err != nil && !slices.ContainsFunc(reported, func(known error) bool { return errors.Is(err, known) }) {
			results.AddError(err.Error())
		}

		if writeErr := results.Write(config.output(), outputFormat); writeErr != nil {
			if err == nil {
				err = fmt.Errorf("unable to write the run report: %w", writeErr)

				return
			}

			log.Error().Err(writeErr).Msg("unable to write the run report")
		}
	}()

	// The manifest is only consulted when asked for. It changes how an existing
	// page is found, which is not something to switch on under anyone without
	// their say-so.
	var tracker *manifest.Store
	if config.TrackPages {
		// A run that publishes nothing resolves exactly as a real one does --
		// otherwise its preview is fiction -- and resolving records what it
		// finds. The store it gets cannot write, which is a guard that holds
		// however it is used.
		if config.readOnly() {
			tracker = manifest.NewReadOnlyStore(api)
		} else {
			tracker = manifest.NewStore(api)
		}
		tracker.SetManifestPage(config.ManifestPage)
		if err := tracker.SetPropertyKeyPrefix(config.ManifestPrefix); err != nil {
			return fmt.Errorf("--manifest-prefix: %w", err)
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
	var saveErr error
	defer func() {
		if tracker == nil {
			return
		}

		// A run stopped by ctx still saves: what the manifest records was
		// published, and is worth keeping however the run ends. Under ctx
		// itself the save would be refused before a byte was sent, or cut
		// short half-written by a signal arriving during it, so it goes out
		// under a context that keeps ctx's values and drops its cancellation,
		// whether or not ctx has ended yet. A second signal, which ends the
		// process outright, is what stops a save that hangs; the transport's
		// own timeouts bound it otherwise. When the run got as far as the
		// explicit save below, that one was made under the same detached
		// context, has already written everything unless it failed, and this
		// one then sends nothing.
		if err := tracker.Save(context.WithoutCancel(ctx)); err != nil {
			log.Error().Err(err).Msg("unable to save page manifest")

			// Only when the explicit save did not already fail: that one is
			// in the report, and this is the same failure a second time.
			if saveErr == nil {
				results.AddError(fmt.Sprintf("unable to save page manifest: %v", err))
			}
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

	// Where the files sit says where the pages go, when asked. Laid out once
	// from the run's own files, so that every document's parents, a directory's
	// title and the pages standing for directories all come from the one list.
	var hierarchy *page.Hierarchy
	if config.ParentsFromPath {
		directoryTitles := page.DirectoryTitleReader(func(file string) ([]byte, error) {
			_, markdown, err := readSource(file)
			return markdown, err
		}, config.TitleFromH1, slices.Contains(config.Features, "frontmatter"))

		hierarchy, err = page.BuildHierarchy(config.ParentsFromPathRoot, config.Files, files, directoryTitles)
		if err != nil {
			return err
		}
	}

	pub := &publishRun{
		api:              api,
		config:           config,
		std:              std,
		pageHeader:       pageHeader,
		tracker:          tracker,
		ancestryTracker:  ancestryTracker,
		checker:          checker,
		globalProperties: globalProperties,
		deferrals:        deferrals,
		results:          results,
		hierarchy:        hierarchy,
		// Read only once a parent is missing.
		titles: &runTitles{config: config, files: files, hierarchy: hierarchy},
	}

	// Pages that asked for a position among their siblings, collected as they
	// publish and applied once at the end -- the order of one page only means
	// anything alongside the others.
	var ordered []page.Ordered

	var hasErrors bool
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		log.Info().Msgf("processing %s", file)

		target, placement, err := pub.processFile(ctx, file)
		if placement != nil {
			ordered = append(ordered, *placement)
		}
		if err != nil {
			results.AddPage(report.Page{
				File: file, Status: report.StatusFailed, Reason: err.Error(),
			})

			if stepPast(ctx, config, file, err) {
				hasErrors = true
				continue
			}

			reported = append(reported, err)

			return err
		}

		if target != nil {
			url := api.BaseURL + target.Links.Full

			// What the report says rather than "there is a page here". A page
			// held back by --no-overwrite is one this run deliberately did not
			// write, and announcing it as updated contradicted the warning
			// printed a line earlier -- "leaving it alone" followed
			// immediately by "page successfully updated".
			status := results.StatusOf(file)

			switch status {
			case report.StatusSkipped:
				// Already said where the decision was made, with the reason.

			case report.StatusUnchanged:
				log.Info().Msgf("page is already up to date: %s", url)

			default:
				log.Info().Msgf("page successfully updated: %s", url)
			}

			// The other formats describe the whole run at the end, where a
			// page published twice can be reported once. A page left alone is
			// not one this run put there, so it is not named among the URLs of
			// the pages it did.
			if outputFormat == report.FormatURL && status != report.StatusSkipped {
				if _, err := fmt.Fprintln(config.output(), url); err != nil {
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

		// Nil deferrals: this is the last look, so a link that still does
		// not resolve is reported rather than waited on again.
		lastLook := *pub
		lastLook.deferrals = nil

		for _, file := range waiting {
			if err := ctx.Err(); err != nil {
				return err
			}

			log.Info().Msgf("processing %s again", file)

			if _, _, err := lastLook.processFile(ctx, file); err != nil {
				// Over what the first pass recorded: the document published
				// then, but the page now holds whatever this pass left it with.
				results.AddPage(report.Page{
					File: file, Status: report.StatusFailed, Reason: err.Error(),
				})

				if stepPast(ctx, config, file, err) {
					hasErrors = true

					continue
				}

				reported = append(reported, err)

				return err
			}
		}
	}

	// Once everything that was going to be published has been, an ac: link that
	// still names no page names none.
	missingPages, err := checker.MissingPages(ctx, api)
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
		if err := page.OrderChildren(ctx, api, config.readOnly(), ordered); err != nil {
			return fmt.Errorf("unable to order pages: %w", err)
		}
	}

	if tracker != nil {
		if err := handleOrphans(ctx, tracker, api, config, onOrphan, hasErrors, results); err != nil {
			return err
		}

		// Under the same detached context as the save on the way out, and for
		// the same reason. Under ctx, a run stopped while its last file
		// published (with --continue-on-error), or a signal arriving during
		// this save, would have it refused and reported as not saved -- while
		// the deferred save then writes the manifest after all, leaving the
		// report and the returned error saying otherwise. Nothing after this
		// point talks to Confluence but that deferred save, which this makes a
		// no-op whenever this one succeeds.
		if saveErr = tracker.Save(context.WithoutCancel(ctx)); saveErr != nil {
			saveErr = fmt.Errorf("unable to save page manifest: %w", saveErr)

			// Recorded here rather than left to the return: two of the three
			// ways out below return something else, and the manifest not
			// being saved is worth knowing whichever it is.
			results.AddError(saveErr.Error())
			reported = append(reported, saveErr)
		}
	}

	// After the manifest is written: an unresolved link says nothing about
	// whether the pages that did publish are recorded correctly, and throwing
	// the mapping away over it would be a far worse outcome than the link.
	if !config.CheckLinksWarnOnly {
		if err := unresolvedLinks(missingPages); err != nil {
			return err
		}
	}

	if hasErrors {
		// The files are the more useful complaint; a failed manifest write is
		// secondary and must not displace it.
		if saveErr != nil {
			log.Error().Err(saveErr).Msg("page manifest was not saved")
		}

		// Not a failure of its own: it sums up the ones recorded against
		// each document that failed.
		filesFailed := errors.New("one or more files failed to process")
		reported = append(reported, filesFailed)

		return filesFailed
	}

	return saveErr
}

// stepPast reports whether the run goes on to the next file after file
// failed with err, which it does only under --continue-on-error, logging the
// failure it steps past.
//
// A file that failed because the run was cancelled is not a file failure to
// step past: the run stops, so that it reports the cancellation.
func stepPast(ctx context.Context, config Config, file string, err error) bool {
	if !config.ContinueOnError || ctx.Err() != nil {
		return false
	}

	log.Error().Err(err).Msgf("processing %s", file)

	return true
}

// ProcessFile processes a single markdown file and publishes it to Confluence.
// Returns nil for the page info when compile-only or dry-run mode is active.
//
// Callers processing several files should prefer Run, which builds the standard
// library once instead of once per file.
//
// Every Confluence request made for the file carries ctx, so cancelling it
// stops the file at its next request, or during the one in flight, with an
// error errors.Is recognises as ctx.Err(); see Run for what that can leave
// behind. A caller looping over files gets the same effect as Run by stopping
// when one fails with it.
//
// Unlike Run this does not shut the shared browser down, since a caller
// publishing several files would pay to start it again for each. Call Cleanup
// when the last one is done.
func ProcessFile(ctx context.Context, file string, api *confluence.API, config Config) (*confluence.PageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	linkChecks, err := config.prepare()
	if err != nil {
		return nil, err
	}

	std, err := stdlib.New(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	globalProperties, err := page.LoadGlobalProperties(config.GlobalProperties)
	if err != nil {
		return nil, err
	}

	pageHeader, err := header.Load(config.PageHeader, std, config.markConfig())
	if err != nil {
		return nil, err
	}

	if err := settlePlatform(ctx, api, config); err != nil {
		return nil, err
	}

	checker := page.NewLinkChecker(linkChecks)

	pub := &publishRun{
		api:              api,
		config:           config,
		std:              std,
		pageHeader:       pageHeader,
		checker:          checker,
		globalProperties: globalProperties,
	}

	target, _, err := pub.processFile(ctx, file)
	if err != nil {
		return target, err
	}

	missing, err := checker.MissingPages(ctx, api)
	if err != nil {
		return target, err
	}

	for _, item := range missing {
		log.Warn().Msg(item)
	}

	if !config.CheckLinksWarnOnly {
		return target, unresolvedLinks(missing)
	}

	return target, nil
}

// readSource reads a document the way everything that looks at its headers
// has to see it. It returns the source, normalised but otherwise as written,
// which is what the file's fingerprint is taken from, and the markdown with its
// ignored regions removed, which is what the headers are read from.
//
// Publishing is not the only reader: --parents-from-path reads a directory's
// document for its title, and a title read from bytes publishing never sees --
// a byte-order mark in front of the header, a header inside an ignored region
// -- is a different title from the one the page is published under.
// settlePlatform identifies the Confluence platform once the run's local
// inputs have been checked and before anything is published, so that one
// failed request cannot send a Cloud tenant down Server's paths for the rest
// of the run. A compile-only run never contacts Confluence.
func settlePlatform(ctx context.Context, api *confluence.API, config Config) error {
	if config.CompileOnly {
		return nil
	}

	_, err := api.Cloud(ctx)

	return err
}

func readSource(file string) ([]byte, []byte, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read file %q: %w", file, err)
	}

	// A byte-order mark left in front of the first header comment would be
	// reported as "doesn't contain metadata", which is not where the author
	// would look.
	source = metadata.NormalizeSource(source)

	// Before the headers are read, so that the line numbers in any complaint
	// are the ones in the file the author is looking at rather than offsets
	// into what is left after the header block is taken off. It also means an
	// ignored region is ignored entirely, headers and all, which is what the
	// markers say on the tin.
	markdown, err := metadata.StripIgnoredBlocks(source)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to process %q: %w", file, err)
	}

	return source, markdown, nil
}

// declaredRestrictions is what a document's metadata restricts, for the
// Confluence client: only the operations it names.
func declaredRestrictions(r *metadata.Restrictions) []confluence.PageRestriction {
	var declared []confluence.PageRestriction
	for operation, subjects := range map[string]*metadata.RestrictionSubjects{
		confluence.RestrictRead:   r.Read,
		confluence.RestrictUpdate: r.Update,
	} {
		if subjects != nil {
			declared = append(declared, confluence.PageRestriction{
				Operation: operation, Users: subjects.Users, Groups: subjects.Groups,
			})
		}
	}
	return declared
}

// documentMeta reads a document's metadata and settles the parents and title
// it publishes under, which with a path hierarchy are partly the path's to say.
// The run's index of titles reads documents through it too, so that the title
// a child looks for and the title its parent publishes cannot disagree.
func documentMeta(config Config, file string, markdown []byte, hierarchy *page.Hierarchy) (*metadata.Meta, []byte, error) {
	meta, markdown, err := metadata.ExtractMeta(
		markdown,
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		file,
		config.Parents,
		// Deferred until the path has had its say about the parents, when it
		// has one; see below.
		config.TitleAppendGeneratedHash && hierarchy == nil,
		config.ContentAppearance,
		slices.Contains(config.Features, "frontmatter"),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to extract metadata from file %q: %w", file, err)
	}

	// Where the file sits says where the page goes, unless the document says
	// otherwise. An author who wrote a Parent header meant it.
	if hierarchy != nil && meta != nil {
		derived, err := hierarchy.Parents(file)
		if err != nil {
			return nil, nil, err
		}

		// A directory's own document is titled by the directory, whatever the
		// filename would have said -- "Readme" on every page that has one. The
		// title comes from the same place its children's parent does, so the
		// two cannot disagree.
		title, err := hierarchy.Title(file)
		if err != nil {
			return nil, nil, err
		}

		// The pages standing for directories are hashed like any other, and
		// both ends have to agree on it: the directory's own document in the
		// title it publishes, the documents beneath it in the parent they look
		// for. Hashing only the first left the README childless beside an
		// empty, unhashed page of the directory's name that took its children.
		if config.TitleAppendGeneratedHash {
			derived, title = page.HashDirectoryTitles(config.Parents, meta.Space, derived, title)
		}

		if !meta.DeclaredParents {
			meta.Parents = append(meta.Parents, derived...)
		}

		if title != "" {
			meta.Title = title
		} else if config.TitleAppendGeneratedHash {
			// Only now, with the parents final: hashed before they were, two
			// documents of one title in different directories hashed the same,
			// and the hash was the very thing meant to tell them apart.
			metadata.AppendGeneratedHash(meta)
		}
	}

	return meta, markdown, nil
}

// publishRun is what every document of a run shares: the connection, the
// settings, and the state built once and threaded through each file. A single
// ProcessFile leaves the run-wide parts nil, and each of them is nil-safe or
// checked where it is used.
type publishRun struct {
	api              *confluence.API
	config           Config
	std              *stdlib.Lib
	pageHeader       *header.Header
	tracker          *manifest.Store
	ancestryTracker  page.AncestryTracker
	checker          *page.LinkChecker
	globalProperties map[string]any
	deferrals        *page.Deferrals
	results          *report.Report
	hierarchy        *page.Hierarchy
	titles           *runTitles
}

// document is one source file as read for publishing.
type document struct {
	file string
	// sourceHash fingerprints the source as read; see readDocument.
	sourceHash string
	// meta is nil when publishing straight to a page id.
	meta     *metadata.Meta
	markdown []byte
	resolver *page.LinkResolver
	// restrictions are the document's declared restrictions, resolved to
	// users and groups; nil when it declares none.
	restrictions *confluence.ResolvedRestrictions
}

// destination is the page a document publishes to.
type destination struct {
	page    *confluence.PageInfo
	created bool
	// What this document's headers placed the page under, kept because a
	// folder parent is not visible anywhere else: it is not a page, so it
	// never appears among the page's ancestors.
	parent *confluence.PageInfo
	// A page whose title changed has to be written even when its content did
	// not, or --changes-only would leave it under the old title forever.
	titleChanged bool
}

// pagePreview is what a dry run found where the page would be.
type pagePreview struct {
	// page is nil when the page would be created; it is what --changes-only
	// compares against.
	page *confluence.PageInfo
	// Where a real run would put that page, and whether it failed the
	// ancestry check: what page.WouldMove needs to tell whether it would be
	// moved.
	parent    *confluence.PageInfo
	misplaced bool
}

func (r *publishRun) processFile(ctx context.Context, file string) (*confluence.PageInfo, *page.Ordered, error) {
	doc, err := r.readDocument(file)
	if err != nil {
		return nil, nil, err
	}

	// Before anything is asked of Confluence: a document that has opted out
	// should cost nothing, not a page lookup and an attachment upload it will
	// not use.
	if skipped, err := r.leaveUnsynchronized(ctx, doc); skipped || err != nil {
		return nil, nil, err
	}

	if err := r.validate(doc); err != nil {
		return nil, nil, err
	}

	doc.resolver = r.linkResolver(doc)

	if err := r.resolveRestrictions(ctx, doc); err != nil {
		return nil, nil, err
	}

	if r.config.CompileOnly || r.config.DryRun {
		return nil, nil, r.preview(ctx, doc)
	}

	dest, err := r.resolveDestination(ctx, doc)
	if err != nil {
		return nil, nil, err
	}

	// Checked here, before anything is written: there is no point uploading
	// attachments for a page that is about to be left alone.
	if skipped, err := r.leaveDrifted(ctx, doc, dest); err != nil {
		return nil, nil, err
	} else if skipped {
		return dest.page, nil, nil
	}

	restrictionPlan, err := r.tightenRestrictions(ctx, doc, dest)
	if err != nil {
		return nil, nil, err
	}

	html, brokenLinks, err := r.compile(ctx, doc, dest.page)
	if err != nil {
		return nil, nil, err
	}

	versionMessage, shouldUpdatePage := r.changeCheck(doc, dest, html)

	// Only fetch the old body and inline comments when we know the page will
	// actually be updated. This avoids unnecessary API round-trips for no-op
	// runs (e.g. when --changes-only determines the content is unchanged).
	if shouldUpdatePage && r.config.PreserveComments && !dest.created {
		html, err = r.preserveComments(ctx, doc, dest, html)
		if err != nil {
			return nil, nil, err
		}
	}

	if shouldUpdatePage {
		if err := r.writePage(ctx, doc, dest.page, html, versionMessage); err != nil {
			return nil, nil, err
		}
	}

	if restrictionPlan != nil {
		if err := r.api.ApplyFinal(ctx, restrictionPlan); err != nil {
			return nil, nil, fmt.Errorf("unable to set page restrictions: %w", err)
		}
	}

	if err := r.recordPublished(ctx, doc, dest.page); err != nil {
		return nil, nil, err
	}

	placement, err := r.applySettings(ctx, doc, dest)
	if err != nil {
		return nil, nil, err
	}

	// Recorded last, once everything asked of the page has been done, so that
	// a page whose update, labels or properties failed is not reported as
	// published.
	status := report.StatusPublished
	if !shouldUpdatePage && (restrictionPlan == nil || !restrictionPlan.Changes()) {
		status = report.StatusUnchanged
	}
	target := dest.page
	r.results.AddPage(report.Page{
		File: doc.file, Status: status,
		Space: spaceOf(doc.meta), Title: target.Title,
		PageID: target.ID, URL: r.api.BaseURL + target.Links.Full,
		Warnings: brokenLinks,
	})

	return target, placement, nil
}

// readDocument reads a file and settles the metadata it publishes under.
func (r *publishRun) readDocument(file string) (*document, error) {
	source, markdown, err := readSource(file)
	if err != nil {
		return nil, err
	}

	// Fingerprint the source as read, before metadata is stripped and links are
	// substituted. Both of those depend on state outside the file -- what is
	// already published, what other files resolve to -- and a fingerprint that
	// moves with the remote would not survive the round trip it exists for.
	sourceHash := fingerprint.SHA1(string(source))

	meta, markdown, err := documentMeta(r.config, file, markdown, r.hierarchy)
	if err != nil {
		return nil, err
	}

	if meta != nil {
		if previous, taken := r.hierarchy.Claim(meta.Space, meta.Title, file); taken {
			return nil, fmt.Errorf(
				"%s already publishes %q in space %q, and a space holds one page of a title: "+
					"rename one of them, or use --title-append-generated-hash",
				previous, meta.Title, meta.Space,
			)
		}
	}

	if r.config.PageID != "" && meta != nil {
		log.Warn().Msg(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)
		meta = nil
	}

	return &document{file: file, sourceHash: sourceHash, meta: meta, markdown: markdown}, nil
}

// leaveUnsynchronized reports, and returns true for, a document that has
// opted out of being published.
func (r *publishRun) leaveUnsynchronized(ctx context.Context, doc *document) (bool, error) {
	meta, file := doc.meta, doc.file
	if meta.Publish() {
		return false, nil
	}

	log.Info().Msgf("%s is not synchronized; leaving it alone", file)
	r.results.AddPage(report.Page{
		File: file, Status: report.StatusSkipped,
		Reason: "the document is not synchronized",
		Space:  spaceOf(meta), Title: titleOf(meta),
	})

	// Looked up purely to mark the path as seen. A page somebody stopped
	// synchronising is one they chose to keep, and a run that did not
	// account for the file would read it as gone: the page is reported as
	// having no source file and its mapping is dropped, so synchronising it
	// again later would no longer know which page was already its own.
	if r.tracker != nil && meta != nil && meta.Space != "" {
		if _, _, err := r.tracker.Lookup(ctx, meta.Space, file); err != nil {
			return true, fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
		}
	}

	return true, nil
}

// validate refuses a document that names no page to publish to.
func (r *publishRun) validate(doc *document) error {
	meta := doc.meta

	if r.config.PageID == "" && meta == nil {
		return fmt.Errorf(
			"specified file doesn't contain metadata and URL is not specified " +
				"via command line or names no page: neither a pageId parameter nor a " +
				"/spaces/KEY/pages/ID path",
		)
	}

	if meta == nil {
		return nil
	}

	if meta.Space == "" {
		return fmt.Errorf(
			"space is not set ('Space' header is not set and '--space' option is not set)",
		)
	}
	if meta.Title == "" {
		return fmt.Errorf(
			"page title is not set: use the 'Title' header, " +
				"or the --title-from-h1 / --title-from-filename flags",
		)
	}

	return checkTitleLengths(meta)
}

// linkResolver builds the resolver the document's links are rewritten
// through while it is rendered, by walking the parsed tree, so that a fenced
// block showing Markdown syntax keeps its example links as written.
func (r *publishRun) linkResolver(doc *document) *page.LinkResolver {
	config, file := r.config, doc.file

	// A fragment's links are written from where the fragment lives, so the
	// directory of everything this document includes is worth looking in too.
	// One level: a fragment that itself includes another is not followed here.
	searchDirs := includeSearchDirs(filepath.Dir(file), config.IncludePath, doc.markdown)

	resolver := page.NewLinkResolver(
		r.api,
		doc.meta,
		filepath.Dir(file),
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		config.Parents,
		config.TitleAppendGeneratedHash,
		slices.Contains(config.Features, "frontmatter"),
		r.checker,
		searchDirs,
	)
	resolver.SourceFile = file
	resolver.Deferrals = r.deferrals

	return resolver
}

// linksUnder is resolver's Resolve under ctx. The renderer asks for links
// without a context of its own, so they are resolved under the file's.
func linksUnder(ctx context.Context, resolver *page.LinkResolver) func(target, text string) (string, error) {
	return func(target, text string) (string, error) {
		return resolver.Resolve(ctx, target, text)
	}
}

// resolveRestrictions resolves the document's declared restrictions before
// anything is written, and on a dry run too, so that a user that resolves to
// nobody, or to more than one person, fails here rather than after a real run
// has created an empty, unrestricted page.
func (r *publishRun) resolveRestrictions(ctx context.Context, doc *document) error {
	if doc.meta == nil || doc.meta.Restrictions == nil || r.config.CompileOnly {
		return nil
	}

	resolved, err := r.api.ResolveRestrictions(ctx, declaredRestrictions(doc.meta.Restrictions))
	if err != nil {
		return fmt.Errorf("unable to resolve the restrictions of %q: %w", doc.file, err)
	}
	doc.restrictions = resolved

	return nil
}

// previewLocation finds, without changing anything, the page a dry run's
// document would publish to.
func (r *publishRun) previewLocation(ctx context.Context, doc *document) (pagePreview, error) {
	var found pagePreview
	meta, file := doc.meta, doc.file

	if meta == nil {
		if r.config.PageID != "" {
			pg, err := r.api.GetPageByID(ctx, r.config.PageID)
			if err != nil {
				return found, fmt.Errorf("unable to resolve page by ID: %w", err)
			}
			found.page = pg
		}

		return found, nil
	}

	// As a real run does before resolving, or a parent renamed in
	// Confluence is looked for under its old title and the page is
	// reported moved on every preview while the real run leaves it be.
	// It only reads, and a dry run's manifest store saves nothing.
	if err := refreshStaleParents(ctx, r.tracker, r.api, meta, r.titles); err != nil {
		return found, err
	}

	parent, pg, misplaced, err := page.PreviewPage(ctx, r.api, meta, r.ancestryTracker)
	if err != nil {
		return found, fmt.Errorf("unable to resolve page location: %w", err)
	}

	if pg == nil {
		// The title found nothing, which is where a real run consults
		// the manifest. Saying so is the whole point of a dry run:
		// otherwise it reports a new page for every rename and retitle
		// the run would actually have handled in place.
		found.page, err = previewTrackedResolution(ctx, r.tracker, r.api, meta, file, doc.sourceHash)
		if err != nil {
			return found, err
		}
		// Not found by title, so ResolvePage's own move does not apply.
		found.parent = parent

		return found, nil
	}

	found.page = pg
	found.parent, found.misplaced = parent, misplaced

	if r.tracker != nil {
		// Found by title. A real run records the path once it has
		// published, and that is what keeps the path from being read
		// as an orphan. A dry run returns before then, so it records
		// here; its store never saves.
		if err := r.tracker.Record(ctx, meta.Space, file, pg.ID, meta.Title, doc.sourceHash); err != nil {
			return found, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
		}
	}

	return found, nil
}

// preview is the whole of a compile-only or dry run for one document: it
// compiles the page, checks it as a real run would, and prints it or, under
// --changes-only, says whether the page would change.
func (r *publishRun) preview(ctx context.Context, doc *document) error {
	config, meta, file := r.config, doc.meta, doc.file

	var found pagePreview
	if config.DryRun {
		var err error
		found, err = r.previewLocation(ctx, doc)
		if err != nil {
			return err
		}
	}
	previewed := found.page

	cfg, err := config.documentConfig(meta, linksUnder(ctx, doc.resolver))
	if err != nil {
		return err
	}

	// Only a dry run asked to tell changed pages from unchanged ones.
	compare := config.DryRun && !config.CompileOnly && config.ChangesOnly

	status, reason := "", ""
	if compare && config.NoOverwrite && previewed != nil && r.tracker != nil && meta != nil {
		// Ahead of compiling, as on a real run: a page edited in Confluence
		// is left alone whatever the source says, and whatever it would fail on.
		drift, err := hasDrifted(ctx, r.api, r.tracker, meta.Space, file, previewed)
		if err != nil {
			return err
		}

		if drift != "" {
			status, reason = report.StatusSkipped, drift
			log.Warn().Msgf("%s: page %q would be left alone: %s", file, previewed.Title, reason)
		}
	}

	// A page the drift check above leaves alone is not compiled at all.
	var html string
	if status == "" {
		html, status, reason, err = r.previewBody(ctx, doc, found, cfg, compare)
		if err != nil {
			return err
		}
	}

	printHTML := status != report.StatusUnchanged && status != report.StatusSkipped
	if compare {
		// A json or github report goes to the same stream, for a CI step to
		// parse; HTML written ahead of it would make it unparseable.
		format, err := report.ParseFormat(config.OutputFormat)
		if err != nil {
			return err
		}
		printHTML = printHTML && format == report.FormatURL
	}

	if printHTML {
		if _, err := fmt.Fprintln(config.output(), html); err != nil {
			return err
		}
	}

	// Checked here as well as before an upload. Validating documents in CI
	// without Confluence credentials is the whole of what --compile-only is
	// for, and a body Confluence would reject outright should fail there,
	// not at publish time in front of the person who can least act on it.
	//
	// The layout wrap and the comment merge are missing from this output,
	// and neither introduces the errors this catches.
	if status != report.StatusSkipped {
		if err := markmd.CheckWellFormed(html); err != nil {
			return err
		}
	}

	// Said here too, and for the same reason the check above is: a run told to
	// check links should report them on the credential-free invocation
	// --compile-only exists for, and the one a pull request gate is most
	// likely to use.
	if err := reportBrokenLinks(doc.resolver.Broken(), file, config.CheckLinksWarnOnly); err != nil {
		return err
	}

	if status != "" {
		var target confluence.PageInfo
		var url string
		if previewed != nil {
			target = *previewed
			// Built as a real run builds it, so CI can link the page.
			url = r.api.BaseURL + target.Links.Full
		}
		title := target.Title
		if meta != nil {
			title = meta.Title
		}
		r.results.AddPage(report.Page{
			File: file, Status: status, Reason: reason,
			Space: spaceOf(meta), Title: title,
			PageID: target.ID, URL: url,
			Warnings: doc.resolver.Broken(),
		})
	}

	return nil
}

// previewBody compiles a previewed page and, when compare is set, says
// whether a real run would change it.
func (r *publishRun) previewBody(
	ctx context.Context,
	doc *document, found pagePreview, cfg types.MarkConfig, compare bool,
) (html, status, reason string, err error) {
	meta, file, previewed := doc.meta, doc.file, found.page

	// Read once, for the preview and for the clash check below.
	var declared []attachment.Attachment
	var attachments *attachmentPreview
	if compare {
		declared, err = declaredAttachments(file, meta)
		if err != nil {
			return "", "", "", err
		}

		attachments, err = previewAttachmentLinks(ctx, r.api, previewed, declared)
		if err != nil {
			return "", "", "", err
		}
		if attachments != nil {
			cfg.ResolveAttachment = attachments.links.Resolve
		}
	}

	html, inline, err := markmd.CompileMarkdown(doc.markdown, r.std, file, cfg)
	if err != nil {
		return "", "", "", fmt.Errorf("unable to compile markdown: %w", err)
	}

	var previewedTitle string
	if previewed != nil {
		previewedTitle = previewed.Title
	}

	headerHTML, headerAttachments, err := renderHeader(r.pageHeader, file, meta, previewedTitle, r.config.Space, cfg)
	if err != nil {
		return "", "", "", err
	}
	html = headerHTML + html

	// The clash a real run refuses, so that this one does not report
	// success for a page that run will fail on. Declared files are read
	// only when there is something to clash with them.
	if len(headerAttachments) > 0 || len(inline) > 0 {
		if !compare {
			declared, err = declaredAttachments(file, meta)
			if err != nil {
				return "", "", "", err
			}
		}

		if _, err := withHeaderAttachments(file, declared, inline, headerAttachments); err != nil {
			return "", "", "", err
		}
	}

	if !compare {
		return html, "", "", nil
	}

	var relinked bool
	html, relinked = attachments.settle(html)

	var others []string
	uploads, err := attachments.uploads(inline)
	if err != nil {
		return "", "", "", err
	}
	if uploads {
		others = append(others, "an attachment would be uploaded")
	}

	settings, err := r.previewSettingChanges(ctx, doc, found)
	if err != nil {
		return "", "", "", err
	}
	others = append(others, settings...)

	status, reason, err = previewChange(ctx, html, previewed, relinked, others, file, r.config, meta, r.std)
	if err != nil {
		return "", "", "", err
	}

	return html, status, reason, nil
}

// resolveDestination finds or creates the page a document publishes to.
func (r *publishRun) resolveDestination(ctx context.Context, doc *document) (*destination, error) {
	var dest *destination
	if doc.meta != nil {
		var err error
		dest, err = r.resolveDocumentPage(ctx, doc)
		if err != nil {
			return nil, err
		}
	} else {
		pg, err := r.api.GetPageByID(ctx, r.config.PageID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		if pg == nil {
			return nil, fmt.Errorf("page with id %q not found", r.config.PageID)
		}
		dest = &destination{page: pg}
	}

	if target := dest.page; target != nil && target.Type == "" {
		if doc.meta != nil && doc.meta.Type != "" {
			target.Type = doc.meta.Type
		} else {
			target.Type = "page"
		}
	}

	return dest, nil
}

// resolveDocumentPage finds the page a document's metadata names, following
// renames the manifest knows of, and creates or relocates it under the
// parents the document declares.
func (r *publishRun) resolveDocumentPage(ctx context.Context, doc *document) (*destination, error) {
	api, meta, file := r.api, doc.meta, doc.file
	dest := &destination{}

	// Parents are named by title, so a parent that has itself been renamed
	// leaves this document pointing at a name nothing carries any more.
	// Do this before resolution, so ancestry is walked against titles that
	// exist rather than creating an empty page under the old one.
	if err := refreshStaleParents(ctx, r.tracker, api, meta, r.titles); err != nil {
		return nil, err
	}

	parent, pg, err := page.ResolvePage(ctx, false, api, meta, r.ancestryTracker)
	if err != nil {
		return nil, fmt.Errorf("error resolving page %q: %w", meta.Title, err)
	}
	dest.parent = parent

	// The title lookup found nothing, which is how both "this page is new"
	// and "this page was renamed" look. Ask the manifest which of the two
	// this is before creating a second page beside the first.
	if pg == nil {
		pg, err = resolveTrackedPage(ctx, r.tracker, api, meta, file)
		if err != nil {
			return nil, err
		}
		if pg == nil {
			// Not published under this path before. It may have been
			// published under another: a renamed file is a new key holding
			// an old document.
			pg, err = resolveRenamedFile(ctx, r.tracker, api, meta, file, doc.sourceHash)
			if err != nil {
				return nil, err
			}
		}
		if pg != nil && pg.Title != meta.Title {
			pg.Title = meta.Title
			dest.titleChanged = true
		}
	}

	if pg == nil {
		if parent != nil && parent.Type == "folder-parent" {
			pg, err = api.CreatePageWithFolderParent(ctx, meta.Space, meta.Type, parent.ID, meta.Title, ``)
		} else {
			pg, err = api.CreatePage(ctx, meta.Space, meta.Type, parent, meta.Title, ``)
		}
		if err != nil {
			return nil, fmt.Errorf("can't create %s %q: %w", meta.Type, meta.Title, err)
		}
		dest.created = true
	} else if parent != nil && parent.Type == "folder-parent" {
		if err := page.EnsurePageUnderFolderParent(ctx, api, pg, parent.ID); err != nil {
			return nil, fmt.Errorf("error relocating page %q: %w", meta.Title, err)
		}
	} else if parent != nil && !page.UnderDeclaredParents(pg, meta.Parents) {
		// A page resolved through the manifest rather than by title never
		// passed the ancestry check, because that check starts from a title
		// lookup that just missed. So an edit changing the title and the
		// parent together would retitle the page and leave it where it was.
		if err := page.EnsurePageUnderParent(ctx, api, pg, parent.ID); err != nil {
			return nil, fmt.Errorf("error relocating page %q: %w", meta.Title, err)
		}
	}

	// Relocating refreshes the page from Confluence, which still carries the
	// old title -- the retitle above is staged and not published until the
	// update. Without this it is overwritten by the move and the page keeps
	// its old name, silently and only when both changed at once.
	if dest.titleChanged && pg != nil {
		pg.Title = meta.Title
	}

	dest.page = pg

	return dest, nil
}

// tightenRestrictions applies the interim half of the document's
// restrictions and returns the plan whose final half is applied once the
// page is written.
//
// Restrictions tighten before attachments and content are written and
// loosen after, so that new content is never shown to anyone it is to be
// kept from, and old content stays restricted until it has been replaced.
// A run that fails in between leaves the page no more open than either.
func (r *publishRun) tightenRestrictions(ctx context.Context, doc *document, dest *destination) (*confluence.RestrictionPlan, error) {
	if doc.restrictions == nil {
		return nil, nil
	}

	plan, err := r.api.PlanRestrictions(ctx, dest.page, doc.restrictions, r.config.EditLock, dest.created)
	if err != nil {
		return nil, err
	}
	if err := r.api.ApplyInterim(ctx, plan); err != nil {
		return nil, fmt.Errorf("unable to set page restrictions: %w", err)
	}

	return plan, nil
}

// compile uploads the document's attachments, compiles it, and returns the
// body to send, wrapped in its layout, with the broken links the run was told
// to warn about rather than fail on.
func (r *publishRun) compile(ctx context.Context, doc *document, target *confluence.PageInfo) (string, []string, error) {
	api, meta, file := r.api, doc.meta, doc.file

	// Collect attachments declared via <!-- Attachment: --> directives.
	var declared []string
	if meta != nil {
		declared = meta.Attachments
	}

	localAttachments, err := attachment.ResolveLocalAttachments(
		vfs.LocalOS,
		filepath.Dir(file),
		declared,
	)
	if err != nil {
		return "", nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	cfg, err := r.config.documentConfig(meta, linksUnder(ctx, doc.resolver))
	if err != nil {
		return "", nil, err
	}

	// With --page-id the file's metadata is discarded, so the page itself and
	// --space are what the header can name.
	var targetTitle string
	if target != nil {
		targetTitle = target.Title
	}

	headerHTML, headerAttachments, err := renderHeader(r.pageHeader, file, meta, targetTitle, r.config.Space, cfg)
	if err != nil {
		return "", nil, err
	}

	// The header's own files are known now, so a clash with a declared one is
	// refused before anything is uploaded. One with a file the document only
	// references can be seen once it is compiled, which needs these uploaded.
	if _, err := withHeaderAttachments(file, localAttachments, nil, headerAttachments); err != nil {
		return "", nil, err
	}

	// The page's remote attachment list is fetched once and threaded through
	// both resolve passes: declared attachments here, then diagrams discovered
	// while rendering.
	remoteAttachments, err := api.GetAttachments(ctx, target.ID)
	if err != nil {
		return "", nil, fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	attaches, remoteAttachments, err := attachment.ResolveAttachmentsWithRemotes(
		ctx,
		api, target, localAttachments, remoteAttachments,
	)
	if err != nil {
		return "", nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	attachmentLinks := attachment.NewResolver(attaches)

	cfg.ResolveAttachment = attachmentLinks.Resolve

	html, inlineAttachments, err := markmd.CompileMarkdown(doc.markdown, r.std, file, cfg)
	if err != nil {
		return "", nil, fmt.Errorf("unable to compile markdown: %w", err)
	}

	html = headerHTML + html
	inlineAttachments, err = withHeaderAttachments(file, attaches, inlineAttachments, headerAttachments)
	if err != nil {
		return "", nil, err
	}

	// Kept for the report as well as the log. Reaching this line with any of
	// them means the run was told to warn rather than fail, since otherwise
	// reportBrokenLinks has already ended it.
	brokenLinks := doc.resolver.Broken()
	if err := reportBrokenLinks(brokenLinks, file, r.config.CheckLinksWarnOnly); err != nil {
		return "", nil, err
	}

	// Only knowable once the document has been walked.
	for _, unused := range attachmentLinks.Unused(attaches) {
		log.Warn().Msgf("unused attachment: %s", unused)
	}

	if _, _, err = attachment.ResolveAttachmentsWithRemotes(
		ctx,
		api, target, inlineAttachments, remoteAttachments,
	); err != nil {
		return "", nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	var layout, sidebar string
	if meta != nil {
		layout = meta.Layout
		sidebar = meta.Sidebar
	}

	html, err = wrapLayout(r.std, layout, sidebar, html)
	if err != nil {
		return "", nil, err
	}

	return html, brokenLinks, nil
}

// preserveComments merges the inline comments on the published page into the
// body about to replace it, refreshing dest.page from Confluence on the way.
func (r *publishRun) preserveComments(ctx context.Context, doc *document, dest *destination, html string) (string, error) {
	pg, err := r.api.GetPageByIDExpanded(ctx, dest.page.ID, "ancestors,version,body.storage")
	if err != nil {
		return "", fmt.Errorf("unable to retrieve page body for comments: %w", err)
	}
	dest.page = pg

	// The refetch carries the title Confluence holds now, which is the old
	// one whenever this run is renaming the page: the new title is staged
	// on the object that was just replaced and is not published until the
	// update. Without this the rename is dropped silently, and the manifest
	// goes on to record a title the page does not carry -- which then
	// misresolves every parent that names it.
	if dest.titleChanged && doc.meta != nil {
		pg.Title = doc.meta.Title
	}

	inlineComments, err := r.api.GetInlineComments(ctx, pg.ID)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve inline comments: %w", err)
	}

	html, err = comments.Merge(html, pg.Body.Storage.Value, inlineComments)
	if err != nil {
		return "", fmt.Errorf("unable to merge inline comments: %w", err)
	}

	return html, nil
}

// writePage sends the page's new body.
func (r *publishRun) writePage(ctx context.Context, doc *document, target *confluence.PageInfo, html, versionMessage string) error {
	// Checked here rather than anywhere earlier because this is the body
	// that is actually sent: after the layout wrap, and after any inline
	// comments have been merged back into it.
	if err := markmd.CheckWellFormed(html); err != nil {
		return err
	}

	contentAppearance, emoji := appearanceOf(doc.meta)

	if err := r.api.UpdatePage(
		ctx,
		target,
		html,
		r.config.MinorEdit,
		versionMessage,
		contentAppearance,
		emoji,
	); err != nil {
		return fmt.Errorf("unable to update page: %w", err)
	}

	return nil
}

// applySettings syncs what the document asks of its page besides the body --
// labels, properties and the edit lock -- and returns where the page asked to
// sit among its siblings, applied once the whole run is known, since a
// position only means something next to the others.
func (r *publishRun) applySettings(ctx context.Context, doc *document, dest *destination) (*page.Ordered, error) {
	meta, target := doc.meta, dest.page

	if meta != nil {
		if err := r.api.UpdateLabels(ctx, target, meta.Labels, r.config.AppendLabels); err != nil {
			return nil, err
		}
	}

	var documentProperties map[string]any
	if meta != nil {
		documentProperties = meta.Properties
	}

	// No dry-run branch here: a dry run returns long before this, when the
	// compiled page would have been printed or, under --changes-only, compared.
	if err := page.ApplyProperties(
		ctx,
		r.api, target.ID,
		page.MergeProperties(r.globalProperties, documentProperties),
	); err != nil {
		return nil, err
	}

	var placement *page.Ordered
	if meta != nil && meta.Order != nil {
		placement = &page.Ordered{
			PageID:   target.ID,
			ParentID: page.ParentIDFor(target, dest.parent),
			Title:    target.Title,
			Order:    *meta.Order,
		}
	}

	if r.config.EditLock {
		log.Info().Msgf(
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			r.config.Username,
		)
		if err := r.api.RestrictPageUpdates(ctx, target, r.config.Username); err != nil {
			return nil, fmt.Errorf("unable to restrict page updates: %w", err)
		}
	}

	return placement, nil
}

// wrapLayout puts the compiled body inside the page's layout, which is what is
// sent to Confluence and what --changes-only fingerprints.
func wrapLayout(std *stdlib.Lib, layout, sidebar, body string) (string, error) {
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
			Body:    body,
		},
	)
	if err != nil {
		return "", fmt.Errorf("unable to execute layout template: %w", err)
	}

	return buffer.String(), nil
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

// Cleanup closes any shared resources (such as headless Chrome sessions).
func Cleanup() {
	d2.Cleanup()
	mermaid.Cleanup()
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

// appearanceOf is the content appearance and emoji the document's metadata
// asks of its page.
func appearanceOf(meta *metadata.Meta) (string, string) {
	if meta == nil {
		return "", ""
	}

	return meta.ContentAppearance, meta.Emoji
}

// renderHeader renders the page header for one document, naming it by its
// metadata where it has some and by fallbackTitle and fallbackSpace where not.
func renderHeader(
	pageHeader *header.Header, file string, meta *metadata.Meta, fallbackTitle, fallbackSpace string, cfg types.MarkConfig,
) (string, []attachment.Attachment, error) {
	return pageHeader.Render(file, cmp.Or(titleOf(meta), fallbackTitle), cmp.Or(spaceOf(meta), fallbackSpace), cfg)
}

// documentConfig is the compile configuration for one document: the run's,
// with what its metadata and link resolution add.
func (c Config) documentConfig(meta *metadata.Meta, resolveLink func(target, text string) (string, error)) (types.MarkConfig, error) {
	if c.DropH1 {
		log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
	}

	imageAlign, err := getImageAlign(c.ImageAlign, meta)
	if err != nil {
		return types.MarkConfig{}, fmt.Errorf("unable to determine image-align: %w", err)
	}

	cfg := c.markConfig()
	cfg.DropFirstH1 = c.DropH1
	cfg.ImageAlign = imageAlign
	cfg.ResolveLink = resolveLink

	return cfg, nil
}

// markConfig is the compile configuration the run's flags give every
// document; the per-document fields are left for the caller to fill in.
func (c Config) markConfig() types.MarkConfig {
	// An invalid value is reported by documentConfig, which every page passes.
	imageAlign, _ := getImageAlign(c.ImageAlign, nil)

	return types.MarkConfig{
		ImageAlign:       imageAlign,
		MermaidScale:     c.MermaidScale,
		MermaidOutput:    c.MermaidOutput,
		MermaidBundle:    c.MermaidBundle,
		D2Output:         c.D2Output,
		D2Scale:          c.D2Scale,
		D2BundleRemote:   c.D2BundleRemote,
		MathFormat:       c.MathFormat,
		MathScale:        c.MathScale,
		StripNewlines:    c.StripLinebreaks,
		Features:         c.Features,
		AttachReferenced: c.AttachReferenced,
		IncludePath:      c.IncludePath,
	}
}
