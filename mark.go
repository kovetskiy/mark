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
	"github.com/kovetskiy/mark/v16/manifest"
	markmd "github.com/kovetskiy/mark/v16/markdown"
	"github.com/kovetskiy/mark/v16/mermaid"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/page"
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
	MinorEdit        bool
	VersionMessage   string
	EditLock         bool
	ChangesOnly      bool
	PreserveComments bool
	TrackPages       bool

	// Rendering
	DropH1          bool
	StripLinebreaks bool
	MermaidScale    float64
	D2Scale         float64
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

	// A nil *manifest.Store put into a non-nil interface is still a non-nil
	// interface, so page would see tracking as enabled and call through a nil
	// receiver. Build the interface value only when there is a store behind it.
	var folders page.FolderTracker
	if tracker != nil {
		folders = tracker
		// The whole file set is known before any of it is processed, which is
		// what lets a recorded path missing from the run be read as a rename
		// rather than a guess made one document at a time.
		tracker.SetRunFiles(config.Files, files)
	}

	var hasErrors bool
	for _, file := range files {
		log.Info().Msgf("processing %s", file)

		target, err := processFile(file, api, config, std, tracker, folders)
		if err != nil {
			if config.ContinueOnError {
				log.Error().Err(err).Msgf("processing %s", file)
				hasErrors = true
				continue
			}
			return err
		}

		if target != nil {
			log.Info().Msgf("page successfully updated: %s", api.BaseURL+target.Links.Full)
			if _, err := fmt.Fprintln(config.output(), api.BaseURL+target.Links.Full); err != nil {
				return err
			}
		}
	}

	// The manifest is saved before the run's own outcome is decided. Returning
	// early on hasErrors used to skip it entirely, so a single bad file threw
	// away the mapping for every page that had published perfectly well --
	// worst under --continue-on-error, which exists precisely to keep going.
	// What was recorded actually happened, and is worth keeping either way.
	var saveErr error
	if tracker != nil {
		reportOrphans(tracker, hasErrors)
		pruneOrphans(tracker, hasErrors, config.DryRun)
		if saveErr = tracker.Save(); saveErr != nil {
			saveErr = fmt.Errorf("unable to save page manifest: %w", saveErr)
		}
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

// reportOrphans logs pages the manifest knows about that this run did not
// publish to. It only reports; nothing here deletes anything.
//
// A path can be missing for two very different reasons -- the file was deleted,
// or this run simply did not cover it -- and mark cannot tell them apart. A run
// narrowed by --files leaves everything outside the glob unseen, and those files
// are present and fine. The wording says candidates for that reason, and acting
// on them is left to a human until there is a mechanism that can distinguish the
// two cases.
func reportOrphans(tracker *manifest.Store, hadErrors bool) {
	for _, space := range tracker.Spaces() {
		orphans := tracker.Orphans(space)
		if len(orphans) == 0 {
			continue
		}
		if hadErrors {
			// Some files did not process, which is indistinguishable from those
			// files being gone. Reporting them now would be actively misleading.
			log.Debug().Msgf(
				"space %q has %d unpublished tracked pages, not reported because this run had errors",
				space, len(orphans),
			)
			continue
		}
		log.Info().Msgf(
			"space %q: %d tracked page(s) had no matching source file in this run: %s",
			space, len(orphans), strings.Join(orphans, ", "),
		)
	}
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

	return processFile(file, api, config, std, nil, nil)
}

func processFile(file string, api *confluence.API, config Config, std *stdlib.Lib, tracker *manifest.Store, folders page.FolderTracker) (*confluence.PageInfo, error) {
	markdown, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("unable to read file %q: %w", file, err)
	}

	markdown = bytes.ReplaceAll(markdown, []byte("\r\n"), []byte("\n"))

	// Fingerprint the source as read, before metadata is stripped and links are
	// substituted. Both of those depend on state outside the file -- what is
	// already published, what other files resolve to -- and a fingerprint that
	// moves with the remote would not survive the round trip it exists for.
	sourceHash := sha1Hash(string(markdown))

	frontMatterEnabled := slices.Contains(config.Features, "frontmatter")

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
		return nil, fmt.Errorf("unable to extract metadata from file %q: %w", file, err)
	}

	if config.PageID != "" && meta != nil {
		log.Warn().Msg(
			`specified file contains metadata, ` +
				`but it will be ignored due specified command line URL`,
		)
		meta = nil
	}

	if config.PageID == "" && meta == nil {
		return nil, fmt.Errorf(
			"specified file doesn't contain metadata and URL is not specified " +
				"via command line or doesn't contain pageId GET-parameter",
		)
	}

	if meta != nil {
		if meta.Space == "" {
			return nil, fmt.Errorf(
				"space is not set ('Space' header is not set and '--space' option is not set)",
			)
		}
		if meta.Title == "" {
			return nil, fmt.Errorf(
				"page title is not set: use the 'Title' header, " +
					"or the --title-from-h1 / --title-from-filename flags",
			)
		}
	}

	links, err := page.ResolveRelativeLinks(
		api,
		meta,
		markdown,
		filepath.Dir(file),
		config.Space,
		config.TitleFromH1,
		config.TitleFromFilename,
		config.Parents,
		config.TitleAppendGeneratedHash,
		frontMatterEnabled,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve relative links: %w", err)
	}

	markdown = page.SubstituteLinks(markdown, links)

	if config.DryRun {
		if meta != nil {
			if _, pg, err := page.ResolvePage(true, api, meta, folders); err != nil {
				return nil, fmt.Errorf("unable to resolve page location: %w", err)
			} else if pg == nil {
				// The title found nothing, which is where a real run consults
				// the manifest. Saying so is the whole point of a dry run:
				// otherwise it reports a new page for every rename and retitle
				// the run would actually have handled in place.
				previewTrackedResolution(tracker, api, meta, file, sourceHash)
			}
		} else if config.PageID != "" {
			if _, err := api.GetPageByID(config.PageID); err != nil {
				return nil, fmt.Errorf("unable to resolve page by ID: %w", err)
			}
		}
	}

	if config.CompileOnly || config.DryRun {
		if config.DropH1 {
			log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
		}

		imageAlign, err := getImageAlign(config.ImageAlign, meta)
		if err != nil {
			return nil, fmt.Errorf("unable to determine image-align: %w", err)
		}

		cfg := types.MarkConfig{
			MermaidScale:  config.MermaidScale,
			D2Scale:       config.D2Scale,
			DropFirstH1:   config.DropH1,
			StripNewlines: config.StripLinebreaks,
			Features:      config.Features,
			ImageAlign:    imageAlign,
			IncludePath:   config.IncludePath,
		}
		html, _, err := markmd.CompileMarkdown(markdown, std, file, cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to compile markdown: %w", err)
		}
		if _, err := fmt.Fprintln(config.output(), html); err != nil {
			return nil, err
		}
		return nil, nil
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
			return nil, err
		}

		parent, pg, err := page.ResolvePage(false, api, meta, folders)
		if err != nil {
			return nil, fmt.Errorf("error resolving page %q: %w", meta.Title, err)
		}

		// The title lookup found nothing, which is how both "this page is new"
		// and "this page was renamed" look. Ask the manifest which of the two
		// this is before creating a second page beside the first.
		if pg == nil {
			pg, err = resolveTrackedPage(tracker, api, meta, file)
			if err != nil {
				return nil, err
			}
			if pg == nil {
				// Not published under this path before. It may have been
				// published under another: a renamed file is a new key holding
				// an old document.
				pg, err = resolveRenamedFile(tracker, api, meta, file, sourceHash)
				if err != nil {
					return nil, err
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
				return nil, fmt.Errorf("can't create %s %q: %w", meta.Type, meta.Title, err)
			}
			// A delay between the create and update call helps mitigate a 409
			// conflict that can occur when attempting to update a page just
			// after it was created. See issues/139.
			time.Sleep(1 * time.Second)
			pageCreated = true
		} else if parent != nil && parent.Type == "folder-parent" {
			if err := page.EnsurePageUnderFolderParent(api, pg, parent.ID); err != nil {
				return nil, fmt.Errorf("error relocating page %q: %w", meta.Title, err)
			}
		}

		target = pg
	} else {
		pg, err := api.GetPageByID(config.PageID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		if pg == nil {
			return nil, fmt.Errorf("page with id %q not found", config.PageID)
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
		return nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	// The page's remote attachment list is fetched once and threaded through
	// both resolve passes. Attachments are resolved twice per page -- declared
	// attachments here, then diagrams discovered while rendering -- and each
	// pass previously issued its own paginated fetch of the same list.
	remoteAttachments, err := api.GetAttachments(target.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	attaches, remoteAttachments, err := attachment.ResolveAttachmentsWithRemotes(
		api, target, localAttachments, remoteAttachments,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create/update attachments: %w", err)
	}

	markdown = attachment.CompileAttachmentLinks(markdown, attaches)

	if config.DropH1 {
		log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
	}

	imageAlign, err := getImageAlign(config.ImageAlign, meta)
	if err != nil {
		return nil, fmt.Errorf("unable to determine image-align: %w", err)
	}

	cfg := types.MarkConfig{
		MermaidScale:  config.MermaidScale,
		D2Scale:       config.D2Scale,
		DropFirstH1:   config.DropH1,
		StripNewlines: config.StripLinebreaks,
		Features:      config.Features,
		ImageAlign:    imageAlign,
		IncludePath:   config.IncludePath,
	}

	html, inlineAttachments, err := markmd.CompileMarkdown(markdown, std, file, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to compile markdown: %w", err)
	}

	if _, _, err = attachment.ResolveAttachmentsWithRemotes(
		api, target, inlineAttachments, remoteAttachments,
	); err != nil {
		return nil, fmt.Errorf("unable to create/update attachments: %w", err)
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
			return nil, fmt.Errorf("unable to execute layout template: %w", err)
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
			return nil, fmt.Errorf("unable to retrieve page body for comments: %w", err)
		}
		target = pg

		comments, err := api.GetInlineComments(target.ID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve inline comments: %w", err)
		}

		html, err = mergeComments(html, target.Body.Storage.Value, comments)
		if err != nil {
			return nil, fmt.Errorf("unable to merge inline comments: %w", err)
		}
	}

	if tracker != nil && meta != nil {
		if err := tracker.Record(meta.Space, file, target.ID, meta.Title, sourceHash); err != nil {
			return nil, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
		}
	}

	if shouldUpdatePage {
		err = api.UpdatePage(
			target,
			html,
			config.MinorEdit,
			finalVersionMessage,
			contentAppearance,
			emoji,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to update page: %w", err)
		}
	}

	if meta != nil {
		if err := updateLabels(api, target, labels); err != nil {
			return nil, err
		}
	}

	if config.EditLock {
		log.Info().Msgf(
			`edit locked on page %q by user %q to prevent manual edits`,
			target.Title,
			config.Username,
		)
		if err := api.RestrictPageUpdates(target, config.Username); err != nil {
			return nil, fmt.Errorf("unable to restrict page updates: %w", err)
		}
	}

	return target, nil
}

// pruneOrphans forgets the entries reportOrphans just named.
//
// Left in place the mapping only ever grows, and a file deleted once is
// reported as missing on every run from then on -- which teaches people to
// ignore the one message worth reading. Reported once, then forgotten.
//
// Nothing is forgotten on a run that had errors, where a file that failed to
// process is indistinguishable from one that is gone, nor on a dry run, which
// does not get to change what the next run believes.
func pruneOrphans(tracker *manifest.Store, hadErrors, dryRun bool) {
	if hadErrors || dryRun {
		return
	}

	for _, space := range tracker.Spaces() {
		if pruned := tracker.PruneOrphans(space); len(pruned) > 0 {
			log.Debug().Msgf(
				"space %q: stopped tracking %d page(s) whose source files are gone",
				space, len(pruned),
			)
		}
	}
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
// Only titles mark itself published are followed, and only when exactly one
// page was published under the title. Anything ambiguous, or any title mark has
// never seen, is left exactly as written -- a parent that is genuinely meant to
// be created still is.
func refreshStaleParents(tracker *manifest.Store, api *confluence.API, meta *metadata.Meta) error {
	if tracker == nil {
		return nil
	}

	for i, title := range meta.Parents {
		// FindPage is memoised, and ancestry resolution is about to ask the
		// same question, so this costs nothing on the common path where the
		// parent is exactly where it says it is.
		existing, err := api.FindPage(meta.Space, title, "page")
		if err != nil || existing != nil {
			continue
		}

		pageID, ok, err := tracker.ResolveStaleTitle(meta.Space, title)
		if err != nil {
			return fmt.Errorf("unable to check whether parent %q was renamed: %w", title, err)
		}
		if !ok {
			continue
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

func updateLabels(api *confluence.API, target *confluence.PageInfo, metaLabels []string) error {
	labelInfo, err := api.GetPageLabels(target, "global")
	if err != nil {
		return err
	}

	log.Debug().Msg("Page Labels:")
	log.Debug().Interface("labels", labelInfo.Labels).Send()
	log.Debug().Msg("Meta Labels:")
	log.Debug().Interface("labels", metaLabels).Send()

	delLabels := determineLabelsToRemove(labelInfo, metaLabels)
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
