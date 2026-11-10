package mark

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kovetskiy/mark/v17/attachment"
	"github.com/kovetskiy/mark/v17/chrome"
	"github.com/kovetskiy/mark/v17/comments"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/fingerprint"
	"github.com/kovetskiy/mark/v17/header"
	"github.com/kovetskiy/mark/v17/imageformat"
	"github.com/kovetskiy/mark/v17/includes"
	"github.com/kovetskiy/mark/v17/internal/fspath"
	"github.com/kovetskiy/mark/v17/internal/globpattern"
	"github.com/kovetskiy/mark/v17/internal/mdsource"
	"github.com/kovetskiy/mark/v17/internal/rendercheck"
	"github.com/kovetskiy/mark/v17/manifest"
	"github.com/kovetskiy/mark/v17/markdown"
	"github.com/kovetskiy/mark/v17/mermaid"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/rs/zerolog/log"
)

// Config is everything Run needs: the publish settings each document is
// published with, and the settings about the run as a whole, which only Run,
// seeing every file, can act on.
type Config struct {
	PublishConfig

	// Files is the glob naming the documents to publish.
	Files string

	// Paths names documents to publish exactly, with no glob meaning, instead
	// of or beside Files: a path holding *, ?, [ or { is that file. A path
	// that does not exist is not published but is still named, so with
	// Tracking.Partial a deleted file listed here is looked for like one Files
	// matches -- which is how a job gives the paths a change touched, as git
	// diff --no-renames --name-only lists them, a renamed file's old path
	// included.
	Paths []string

	// Documents are published in addition to the files Files matches, or
	// instead of them when Files is empty: documents handed over from memory,
	// each named by its Path and kept in the page manifest under its Key.
	// They are published after the files, in the order given. They take part
	// in everything a run does across its files, except ParentsFromPath,
	// which they are refused with: a run's whole file set lays out the
	// hierarchy, and a document's Path need not be anywhere on disk.
	//
	// A Files pattern that matches nothing still fails the run, documents or
	// not, unless CI is set: the pattern's pages would otherwise all read as
	// orphans, with the documents' pages keeping the space from looking
	// empty. Under CI such a pattern is only warned about, and its pages are
	// not looked at for orphans.
	Documents []Document

	// DocumentScope names the Documents for the page manifest the way Files
	// names the files: a page published from a document is recorded under
	// it, and a later run with the same scope that no longer publishes that
	// document's Key finds the page orphaned, and --on-orphan acts on it. A
	// run's documents are also only taken for renames, by their content, of
	// documents recorded under the same scope.
	//
	// Empty, documents take part in no orphan handling and no rename
	// detection: a page is found again by its Key alone, and never reported,
	// archived or deleted because a later run did not publish it. Files
	// published alongside keep their own scope, their pattern, either way.
	// It needs Tracking.TrackPages, and a scope is never mistaken for a
	// --files pattern.
	DocumentScope string

	Connection ConnectionConfig

	// ParentsFromPath places each page under pages standing for the
	// directories between ParentsFromPathRoot and its file, laid out from the
	// whole file set.
	ParentsFromPath     bool
	ParentsFromPathRoot string

	// ContinueOnError goes on to the next file after one fails, and fails the
	// run at the end with a *FilesFailedError holding a *FileError for each,
	// unless the run as a whole fails after its files -- an orphan that could
	// not be handled, or a link left unresolved -- which is then the error.
	// Without it, Run stops at the first file that fails, with a *FileError.
	// A cancelled run's error is the cancellation to errors.Is, and any files
	// that failed before it are only in the report.
	ContinueOnError bool
	// CI makes a glob that matches no files a warning rather than an error.
	CI bool

	// Unwatch stops the authenticated user watching the pages the run wrote,
	// once it ends, except those they watched before.
	Unwatch bool

	Tracking TrackingConfig
	Report   ReportConfig
}

// PublishConfig is how each document is published: what Run and a Publisher
// both act on, file by file.
type PublishConfig struct {
	Mode      ModeConfig
	Page      PageConfig
	Update    UpdateConfig
	Links     LinkConfig
	Rendering RenderingConfig

	// Output is where a compile-only or dry run prints the compiled body, and
	// where Run writes its page URLs and, unless ReportConfig.Output is set,
	// its report. If nil, output is discarded; the CLI sets this to os.Stdout.
	Output io.Writer
}

// ConnectionConfig is where Confluence is and who to be there.
type ConnectionConfig struct {
	BaseURL               string
	Username              string
	Password              string
	InsecureSkipTLSVerify bool
}

// api is a client for this Confluence, with the rest of its settings in opts.
func (c ConnectionConfig) api(opts confluence.Options) *confluence.API {
	opts.Username, opts.Password, opts.InsecureSkipVerify = c.Username, c.Password, c.InsecureSkipTLSVerify

	return confluence.NewAPI(c.BaseURL, opts)
}

// ModeConfig is how far publishing goes.
type ModeConfig struct {
	CompileOnly bool
	DryRun      bool

	// Offline says a compile has no Confluence to ask, and requires
	// CompileOnly. A relative link to another document is then checked against
	// the files alone and left as written, rather than rewritten to the page
	// found for it, and an ac: link fails the confluence check, which only
	// Confluence can answer. Without it a compile looks those pages up, so
	// that what it prints is what a real run would publish.
	Offline bool
}

// readOnly reports whether a run in this mode must leave Confluence untouched. A compile
// prints the body it would have sent and stops; a dry run reports what it would
// have done. Neither publishes anything, so neither may delete, archive or
// reorder either -- a guard that asks only about --dry-run is one --compile-only
// walks straight past.
func (m ModeConfig) readOnly() bool {
	return m.DryRun || m.CompileOnly
}

// PageConfig is which page a document publishes to: the one PageID names, or
// the one its space, parents and title find.
type PageConfig struct {
	PageID                   string
	Space                    string
	Parents                  []string
	TitleFromH1              bool
	TitleFromFilename        bool
	TitleAppendGeneratedHash bool
	ContentAppearance        metadata.Appearance
}

// UpdateConfig is how a page is written once it is found.
type UpdateConfig struct {
	MinorEdit        bool
	VersionMessage   string
	EditLock         bool
	ChangesOnly      bool
	PreserveComments bool
	AppendLabels     bool
	GlobalProperties string
}

// LinkConfig is which links are checked, whether one that does not resolve
// fails the run, and where relative links are looked for.
type LinkConfig struct {
	CheckLinks         []string
	CheckLinksWarnOnly bool

	// StagedRoot and SourceRoot are for a caller that publishes copies of its
	// documents from a staging directory: StagedRoot is the directory the
	// copies are under, SourceRoot the tree they were made from. A relative
	// link that names no file beside the staged document is then looked for
	// at the same place under SourceRoot, which is where the link was written
	// to make sense, both when it is checked and when it is rewritten to the
	// page it names. A file beside the staged document still comes first,
	// since that copy is the one that publishes.
	//
	// Both or neither: empty resolves links against the document's own
	// directory alone. Each must be an existing directory, and every file Run
	// is given, Sync: false or not, must be under StagedRoot. Relative paths are taken from the
	// working directory.
	StagedRoot string
	SourceRoot string

	// SourceRootCheckOnly looks under SourceRoot for the check alone: a link
	// to a file found only there passes the internal check and is left as
	// written, rather than rewritten to a page read from a file that is not
	// what publishes -- one without the headers a build would give it, or
	// titled from its name otherwise than the build titles it. Requires
	// SourceRoot.
	SourceRootCheckOnly bool
}

// LinkConfigError is a LinkConfig, alone or with the rest of the
// PublishConfig, that Run and NewPublisher refuse before anything is
// published, or a document the config cannot resolve links for, which fails
// that document. Ask for it with errors.As.
type LinkConfigError struct {
	// Field is the setting refused, as a path from PublishConfig, such as
	// "Links.CheckLinksWarnOnly".
	Field string
	// Flag is the mark publish flag that sets Field.
	Flag string
	// Reason says what is wrong with it, and names any other setting
	// involved.
	Reason string
	// Err is the cause, when there is one beyond the setting itself.
	Err error
}

func (e *LinkConfigError) Error() string {
	return fmt.Sprintf("%s (%s) %s", e.Field, e.Flag, e.Reason)
}

func (e *LinkConfigError) Unwrap() error { return e.Err }

// validate refuses link settings that cannot be acted on, given the checks
// they parse to.
func (l LinkConfig) validate(checks page.LinkChecks) error {
	// Tested on what was parsed rather than on the list, which can hold only
	// empty values and still check nothing.
	if l.CheckLinksWarnOnly && !checks.Any() {
		return &LinkConfigError{
			Field: "Links.CheckLinksWarnOnly", Flag: "--check-links-warn-only",
			Reason: "requires Links.CheckLinks (--check-links) to name a kind of link: " +
				"on its own no links are checked, so the silence means nothing",
		}
	}

	roots := []struct {
		field, flag, value string
	}{
		{"Links.StagedRoot", "--links-staged-root", l.StagedRoot},
		{"Links.SourceRoot", "--links-source-root", l.SourceRoot},
	}

	if (l.StagedRoot == "") != (l.SourceRoot == "") {
		set, unset := roots[0], roots[1]
		if l.StagedRoot == "" {
			set, unset = unset, set
		}

		return &LinkConfigError{
			Field: set.field, Flag: set.flag,
			Reason: fmt.Sprintf("requires %s (%s): one says where documents are read, "+
				"the other where their links were written, and neither means anything alone",
				unset.field, unset.flag),
		}
	}

	if l.SourceRootCheckOnly && l.SourceRoot == "" {
		return &LinkConfigError{
			Field: "Links.SourceRootCheckOnly", Flag: "--links-source-root-check-only",
			Reason: "requires Links.SourceRoot (--links-source-root): " +
				"it says how the source tree is used, and without one there is none",
		}
	}

	for _, root := range roots {
		if root.value == "" {
			continue
		}

		info, err := os.Stat(root.value)
		if err != nil {
			return &LinkConfigError{
				Field: root.field, Flag: root.flag,
				Reason: fmt.Sprintf("names no usable directory: %v", err), Err: err,
			}
		}

		if !info.IsDir() {
			return &LinkConfigError{
				Field: root.field, Flag: root.flag,
				Reason: fmt.Sprintf("names %s, which is not a directory", root.value),
			}
		}
	}

	return nil
}

// checkStaged is the directory under SourceRoot that stands for file's own, or
// "" when no roots are set, and refuses a document outside StagedRoot.
// Resolved against its own directory alone, its links would be answered by
// the staging copy, which is what setting the roots is meant to stop.
func (l LinkConfig) checkStaged(file string) (string, error) {
	if l.StagedRoot == "" {
		return "", nil
	}

	dir, ok := l.sourceDir(filepath.Dir(file))
	if !ok {
		return "", &LinkConfigError{
			Field: "Links.StagedRoot", Flag: "--links-staged-root",
			Reason: fmt.Sprintf("does not contain %s, so there is no telling where its links were written", file),
		}
	}

	return dir, nil
}

// sourceDir is the directory under SourceRoot that stands for dir, and false
// when no roots are set or dir is not under StagedRoot.
func (l LinkConfig) sourceDir(dir string) (string, bool) {
	if l.StagedRoot == "" {
		return "", false
	}

	rel, ok := fspath.Under(l.StagedRoot, dir)
	if !ok {
		return "", false
	}

	return filepath.Join(l.SourceRoot, rel), true
}

// TrackingConfig is the page manifest --track-pages keeps, and what is done
// about the pages it lists that a run no longer publishes.
type TrackingConfig struct {
	TrackPages   bool
	ManifestPage string

	// ManifestPageOptional publishes to a space where ManifestPage does not
	// exist untracked, rather than failing the run with a
	// *manifest.MissingPageError: as a first run of tracking would, with
	// nothing found through an earlier mapping. The mapping is saved only if
	// the page exists by the end of the run, so a run may publish the
	// manifest page itself after the documents under it. Each space that
	// ended untracked is in the report's Untracked, and logged as a warning.
	// OnOrphan finds no orphans in such a space. Requires ManifestPage.
	//
	// With NoOverwrite, every existing page in such a space is skipped, not
	// only on the first run but on every run for as long as the manifest page
	// is missing: nothing records what mark last wrote there, so an edit made
	// in Confluence cannot be told from none. Only pages the run creates are
	// published.
	ManifestPageOptional bool

	ManifestPrefix string
	OnOrphan       report.OnOrphan // report.OnOrphanReport (also ""), OnOrphanArchive or OnOrphanDelete
	OrphanUnder    string

	// Partial says Files and Paths name only some of the files of the pattern
	// that publishes them, such as the files a change touched. Orphans and
	// renamed files are then looked for among the tracked paths they name, not
	// among those their pattern recorded, so a tracked file they do not name
	// is left alone; and a page the run records keeps the pattern its entry,
	// or the entry of the file it was renamed from, was recorded under.
	Partial bool

	// NoOverwrite leaves alone a page edited in Confluence since mark last
	// published it, which only the manifest remembers.
	NoOverwrite bool
}

// RenderingConfig is how a document becomes storage format: diagrams,
// formulas, features, images and the page header.
type RenderingConfig struct {
	DropH1          bool
	StripLinebreaks bool
	MermaidEngine   mermaid.EngineKind
	MermaidScale    float64
	// MermaidOutput, D2Output and MathOutput are the image each kind of
	// picture is published as; the zero value is PNG.
	MermaidOutput    imageformat.Format
	MermaidBundle    bool
	D2Output         imageformat.Format
	D2Scale          float64
	D2BundleRemote   bool
	MathOutput       imageformat.Format
	MathScale        float64
	Features         []string
	ImageAlign       string
	AttachReferenced bool
	IncludePath      string
	// PageHeader is a template placed at the top of every page: Markdown
	// when it has a Markdown extension, storage format otherwise.
	PageHeader string
}

// ReportConfig is what a run says about itself. It is written to Output, or
// to PublishConfig.Output when Output is nil.
type ReportConfig struct {
	OutputFormat report.Format

	// Output, when set, is where Run writes its report, so that it can be read
	// apart from what PublishConfig.Output carries. PublishConfig.Output then
	// keeps the page URLs and the compiled bodies of a compile-only or dry
	// run in every format, rather than giving them up to a json or github
	// report that has to stay parseable. In the url format, whose report is
	// the list of URLs, each URL is written to both. The report is written on
	// the way out of every run that gets as far as publishing, whether or not
	// it then fails. Run does not close it.
	//
	// Nil writes the report to PublishConfig.Output with everything else.
	// Setting it to PublishConfig.Output itself mixes the two streams again:
	// give it a writer of its own.
	Output io.Writer

	// SourcePath, when set, names the file a document was made from, such as
	// the template generated Markdown came from, given the path mark read it
	// from. The json report gives it as each page's and orphan's source, and
	// the github report annotates it, so the annotation lands on a file in
	// the pull request. An orphan's path is its manifest key: slash-separated,
	// relative to the working directory for a file under it and absolute for
	// one outside it. An empty result, or file itself, means the document is
	// its own source. It is asked about each file once: for the report,
	// except in the url format, which names no files, and for the *FileError
	// of a file that fails, in every format.
	SourcePath func(file string) string
}

// output returns the configured writer, falling back to io.Discard so that
// library callers that do not set Output receive no implicit stdout writes.
func (c PublishConfig) output() io.Writer {
	if c.Output != nil {
		return c.Output
	}
	return io.Discard
}

// reportOutput is where Run writes its report.
func (c Config) reportOutput() io.Writer {
	if c.Report.Output != nil {
		return c.Report.Output
	}

	return c.output()
}

// isScaleOrDefault is rendercheck.IsScale for a field whose zero value means
// the renderer's default.
func isScaleOrDefault(scale float64) bool {
	return scale == 0 || rendercheck.IsScale(scale)
}

// resources are what one run starts and shuts down again: the browser and
// mermaid engine its pictures are drawn in, each launched by the first picture
// that needs it.
type resources struct {
	browser *chrome.Browser
	mermaid *mermaid.Engine
}

// newResources creates them for the mermaid engine kind names, starting
// nothing yet.
func newResources(kind mermaid.EngineKind) (*resources, error) {
	engine, err := mermaid.NewEngine(kind)
	if err != nil {
		return nil, err
	}

	return &resources{browser: chrome.NewBrowser(), mermaid: engine}, nil
}

func (r *resources) close() {
	r.browser.Close()
	r.mermaid.Close()
}

// prepare checks the settings that decide how a document is rendered and what
// is done about its links, for Run and NewPublisher alike.
func (c PublishConfig) prepare() (page.LinkChecks, error) {
	if err := c.Rendering.validate(); err != nil {
		return page.LinkChecks{}, err
	}

	// Every other mode writes to Confluence, or reads it to say what it would
	// write.
	if c.Mode.Offline && !c.Mode.CompileOnly {
		return page.LinkChecks{}, errors.New("Mode.Offline requires Mode.CompileOnly: only a compile can run without Confluence")
	}

	// A page with no Content-Appearance of its own takes this one, and one
	// that does not parse would fail every such page in turn.
	if _, err := metadata.ParseContentAppearance(string(c.Page.ContentAppearance)); err != nil {
		return page.LinkChecks{}, fmt.Errorf("invalid ContentAppearance: %w", err)
	}

	checks, err := page.ParseLinkChecks(c.Links.CheckLinks)
	if err != nil {
		return page.LinkChecks{}, &LinkConfigError{
			Field: "Links.CheckLinks", Flag: "--check-links", Reason: "is not usable: " + err.Error(), Err: err,
		}
	}

	if err := c.Links.validate(checks); err != nil {
		return page.LinkChecks{}, err
	}

	return checks, nil
}

// Validate checks the settings each document is published with, as Run and
// NewPublisher do before anything else, without a connection or any
// documents. It does look on disk for Links.StagedRoot and Links.SourceRoot,
// so call it once they exist. A link setting it refuses is a
// *LinkConfigError. Run checks the settings
// about the run as a whole as well, which this does not.
func (c PublishConfig) Validate() error {
	_, err := c.prepare()

	return err
}

// validate refuses rendering settings the renderers cannot act on.
func (r RenderingConfig) validate() error {
	// Checked whether or not mermaid is enabled: the engine is made for every
	// run, and refuses a name it does not know.
	if err := mermaid.CheckEngine(r.MermaidEngine); err != nil {
		return err
	}

	// Checked here as well as by the renderers, so that a value they do not
	// know fails the run before anything is published rather than at its
	// first picture. Compared as written: imageformat.Parse is for what a
	// person typed. Empty is a field nobody set, and means PNG.
	for _, output := range []struct {
		field string
		value imageformat.Format
	}{
		{"MermaidOutput", r.MermaidOutput},
		{"D2Output", r.D2Output},
		{"MathOutput", r.MathOutput},
	} {
		if output.value != "" && !output.value.Valid() {
			return fmt.Errorf(
				"invalid %s %q: expected \"png\", \"svg\", or \"\" for the default",
				output.field, output.value,
			)
		}
	}

	if r.MermaidBundle && r.MermaidOutput != imageformat.SVG {
		return errors.New(
			"MermaidBundle needs MermaidOutput \"svg\": " +
				"there is nowhere in a PNG to keep the diagram's source",
		)
	}

	// Zero is the field a caller never set, which for a scale is not a default
	// but a diagram rendered at nothing. Only checked where diagrams are drawn
	// at all, since a configuration that never turns d2 on carries the field
	// past every code path that reads it.
	if r.enabled("d2") && !rendercheck.IsScale(r.D2Scale) {
		return fmt.Errorf(
			"invalid D2Scale %v: expected a finite number greater than 0 with the d2 feature enabled",
			r.D2Scale,
		)
	}

	// Mermaid and math take the same numbers, with one difference: zero is
	// their default rather than an error. Mermaid is on by default, so refusing
	// zero would refuse every Config that never heard of the field; math has
	// always drawn a zero scale at its default of 2, and a caller relying on
	// that is not wrong. What neither has ever been able to draw is a negative,
	// NaN or infinite scale, and those are refused here rather than part way
	// through a run, when the first diagram reaches the browser.
	if r.enabled("mermaid") && !isScaleOrDefault(r.MermaidScale) {
		return fmt.Errorf(
			"invalid MermaidScale %v: expected a finite number greater than 0, or 0 for the default, with the mermaid feature enabled",
			r.MermaidScale,
		)
	}

	if r.enabled("math") && !isScaleOrDefault(r.MathScale) {
		return fmt.Errorf(
			"invalid MathScale %v: expected a finite number greater than 0, or 0 for the default, with the math feature enabled",
			r.MathScale,
		)
	}

	return nil
}

// enabled reports whether --features turns feature on.
func (r RenderingConfig) enabled(feature string) bool {
	return slices.Contains(r.Features, feature)
}

// ErrPartialPattern is returned, wrapped, by Run for a partial run that
// archives or deletes orphans (Tracking.Partial with Tracking.OnOrphan
// archive or delete) and is given a Files pattern holding glob syntax. Such a
// pattern reaches every tracked file it matches, those another job publishes
// included, and on a checkout holding only some files reads the rest as
// deleted; only the exact paths of Paths, or a Files naming one file, say
// which files the run is about.
var ErrPartialPattern = errors.New("a partial run that archives or deletes orphans takes exact paths, not a pattern")

// runFiles is the files a run publishes: those the glob matches, then each of
// paths that exists, as it is written, once.
func runFiles(glob string, paths []string) ([]string, error) {
	var files []string
	if glob != "" || len(paths) == 0 {
		matched, err := doublestar.FilepathGlob(glob)
		if err != nil {
			return nil, err
		}
		files = matched
	}

	listed := make(map[string]bool, len(files))
	for _, file := range files {
		listed[manifest.Key(file)] = true
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// Named all the same: a deleted file a partial run looks for.
			continue
		case err != nil:
			return nil, err
		case info.IsDir():
			return nil, fmt.Errorf("%s is a directory: Paths names files", path)
		}

		if key := manifest.Key(path); !listed[key] {
			listed[key] = true
			files = append(files, path)
		}
	}

	return files, nil
}

// Run processes all files matching Config.Files or named in Config.Paths, and
// Config.Documents, and publishes them to Confluence.
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
//
// The browsers diagrams and formulas are drawn in belong to the run: started
// by the first picture that needs one, and shut down before this returns.
func Run(ctx context.Context, config Config) (err error) {
	// err is named so the deferred report write below can see, and replace,
	// what the run is returning.
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

	checks, err := config.prepare()
	if err != nil {
		return err
	}

	// The rest is about the run as a whole -- the report written at the end of
	// it and the manifest kept across runs -- so a Publisher, which has
	// neither, does not ask.
	outputFormat, err := report.ParseFormat(string(config.Report.OutputFormat))
	if err != nil {
		return err
	}

	onOrphan, err := report.ParseOnOrphan(string(config.Tracking.OnOrphan))
	if err != nil {
		return err
	}

	// A value no run could use is reported before what else it needs, as
	// OnOrphan is: adding --track-pages would only lead to this error next.
	manifestPrefix, err := manifest.NormalizePropertyKeyPrefix(config.Tracking.ManifestPrefix)
	if err != nil {
		return fmt.Errorf("invalid Tracking.ManifestPrefix (--manifest-prefix): %w", err)
	}

	// Removing a page needs to know mark put it there, and only the manifest
	// knows that. Without it the flag could only guess from titles, which is
	// not a guess worth making about deletion.
	if onOrphan != report.OnOrphanReport && !config.Tracking.TrackPages {
		return fmt.Errorf(
			"--on-orphan %s requires --track-pages: "+
				"only the page manifest knows which pages mark published",
			onOrphan,
		)
	}

	if config.ParentsFromPath && config.ParentsFromPathRoot == "" &&
		config.Files == "" && len(config.Paths) > 0 {
		return errors.New("--parents-from-path with only a list of paths needs --parents-from-path-root: " +
			"there is no --files pattern to take the root from")
	}

	if config.Tracking.Partial && !config.Tracking.TrackPages {
		return errors.New("--partial requires --track-pages: " +
			"it is about the page manifest, which only --track-pages keeps")
	}

	if config.Tracking.Partial && onOrphan != report.OnOrphanReport && globpattern.HasMeta(filepath.ToSlash(config.Files)) {
		return fmt.Errorf(
			"Tracking.OnOrphan (--on-orphan) %s with Tracking.Partial (--partial) and Files (--files) %q: %w; "+
				"name the files in Paths (--files-from), or give Files as one file",
			onOrphan, config.Files, ErrPartialPattern,
		)
	}

	if config.Tracking.NoOverwrite && !config.Tracking.TrackPages {
		return fmt.Errorf("--no-overwrite requires --track-pages: " +
			"the version mark last published is remembered in the page manifest")
	}

	if config.Tracking.ManifestPageOptional && strings.TrimSpace(config.Tracking.ManifestPage) == "" {
		return fmt.Errorf("--manifest-page-optional (Tracking.ManifestPageOptional) requires " +
			"--manifest-page (Tracking.ManifestPage): it says what to do when that page is missing")
	}

	if config.Tracking.ManifestPage != "" && !config.Tracking.TrackPages {
		return fmt.Errorf("--manifest-page requires --track-pages: " +
			"it says where the page manifest is kept, and nothing else keeps one")
	}

	if manifestPrefix != manifest.PropertyKeyPrefix && !config.Tracking.TrackPages {
		return fmt.Errorf("--manifest-prefix requires --track-pages: " +
			"it names the page manifest's properties, and nothing else keeps one")
	}

	if err := config.checkDocuments(onOrphan); err != nil {
		return err
	}

	api := config.Connection.api(confluence.Options{
		// An update refused as a conflict is retried against the page's
		// current version, which overwrites whatever changed it.
		// --no-overwrite exists to stop exactly that, so under it the retry
		// goes ahead only when nobody else has written to the page.
		KeepConcurrentEdits: config.Tracking.NoOverwrite,
		Unwatch:             config.Unwatch,
	})
	// Deferred, so that a run that fails partway still unwatches what it wrote.
	// An interrupted run does not: it warns which pages stay watched.
	defer api.UnwatchWritten(ctx)

	// Documents alone need no glob, and an empty one matches nothing.
	var files []string
	if config.Files != "" || len(config.Paths) > 0 || len(config.Documents) == 0 {
		files, err = runFiles(config.Files, config.Paths)
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
	}

	run, err := indexInputs(files, config.Documents)
	if err != nil {
		return err
	}
	inputs, documents := run.all, run.documents

	// A document the staged root does not contain is refused before any is
	// published, rather than part way through the run.
	for _, file := range files {
		if _, err := config.Links.checkStaged(file); err != nil {
			return err
		}
	}

	// Every document would be published over the one page in turn, and only
	// the last would be left on it.
	if config.Page.PageID != "" && len(inputs) > 1 {
		if len(documents) > 0 {
			return fmt.Errorf(
				"--url names one page, but the run has %d documents: give it the one document to publish there",
				len(inputs),
			)
		}

		named := files
		if len(named) > 3 {
			named = append(slices.Clone(named[:3]), "...")
		}
		return fmt.Errorf(
			"--url names one page, but --files matched %d files (%s): name the one file to publish there",
			len(files), strings.Join(named, ", "),
		)
	}

	publisher, err := newPublisher(ctx, api, config.PublishConfig, checks)
	if err != nil {
		return err
	}
	defer publisher.Close()

	pub := publisher.run

	// What the run did, for whatever is reading the output rather than the log.
	results := report.New()

	// Failures the report already carries, so that they are not said twice: a
	// document's own is recorded against that document, and a manifest save
	// that failed is recorded where it failed.
	var reported []error

	// Asked once a file: a failed file's error and the report both name its
	// source. A Document's own Source answers before SourcePath is asked.
	sourcePath := onceEach(sourcesOf(inputs, config.Report.SourcePath))

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

		pub.flushWarnings()

		if outputFormat != report.FormatURL {
			results.SetSources(sourcePath)
		}

		if writeErr := results.Write(config.reportOutput(), outputFormat); writeErr != nil {
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
	if config.Tracking.TrackPages {
		// A run that publishes nothing resolves exactly as a real one does --
		// otherwise its preview is fiction -- and resolving records what it
		// finds. The store it gets cannot write, which is a guard that holds
		// however it is used.
		if config.Mode.readOnly() {
			tracker = manifest.NewReadOnlyStore(api)
		} else {
			tracker = manifest.NewStore(api)
		}
		tracker.SetManifestPage(config.Tracking.ManifestPage)
		tracker.SetPartial(config.Tracking.Partial)
		tracker.SetManifestPageOptional(config.Tracking.ManifestPageOptional)
		// Normalised above, so this cannot fail.
		if err := tracker.SetPropertyKeyPrefix(manifestPrefix); err != nil {
			return err
		}
		if config.Page.PageID != "" {
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

		// After the save, which keeps the mapping of a space whose manifest
		// page this run published, so that space is not reported.
		for _, space := range tracker.UntrackedSpaces() {
			results.AddUntracked(report.Untracked{Space: space, ManifestPage: strings.TrimSpace(config.Tracking.ManifestPage)})
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
		// A pattern that matched nothing, let through by CI, is not the run's
		// scope when documents are published beside it: they would keep the
		// space from looking empty, and every page the pattern published would
		// read as an orphan.
		if len(files) > 0 || len(documents) == 0 {
			tracker.SetRunFiles(config.Files, files)
		}
		tracker.SetRunPaths(config.Paths)
		if len(documents) > 0 {
			keys := make([]string, 0, len(documents))
			for _, in := range documents {
				keys = append(keys, in.key)
			}
			tracker.SetRunDocuments(documentScopePattern(config.DocumentScope), keys)
		}
	}

	// A link is resolved by finding the page it points at, so a document linking
	// to another this run is about to create has nothing to find. Collected
	// while publishing and dealt with afterwards.
	//
	// Not on a dry run or a compile: nothing is published, so nothing a link is
	// waiting for will come to exist, and the wait is worth reporting instead.
	var deferrals *page.Deferrals
	if !config.Mode.readOnly() {
		deferrals = page.NewDeferrals()
	}

	// Where the files sit says where the pages go, when asked. Laid out once
	// from the run's own files, so that every document's parents, a directory's
	// title and the pages standing for directories all come from the one list.
	var hierarchy *page.Hierarchy
	if config.ParentsFromPath {
		directoryTitles := page.DirectoryTitleReader(func(file string) ([]byte, error) {
			_, markdown, _, err := readSource(file)
			return markdown, err
		}, config.Page.TitleFromH1, config.Rendering.enabled("frontmatter"))

		hierarchy, err = page.BuildHierarchy(config.ParentsFromPathRoot, config.Files, files, directoryTitles)
		if err != nil {
			return err
		}
	}

	pub.tracker = tracker
	pub.ancestryTracker = ancestryTracker
	pub.deferrals = deferrals
	pub.warnings = map[string]*report.Warnings{}
	if len(documents) > 0 {
		pub.inputs = run
	}

	if tracker != nil {
		// The manifest knows a document by its key, which is how it says
		// which document a warning of its is about.
		byKey := run.byKey
		tracker.SetWarningsFor(func(key string) *report.Warnings {
			if file, ok := byKey[key]; ok {
				return pub.warningsFor(file)
			}

			return nil
		})
	}

	if hierarchy != nil {
		for _, file := range hierarchy.Outside() {
			pub.warningsFor(file).Add(report.WarningPlacement,
				"it is outside --parents-from-path-root %q, so its path says nothing about its parents",
				hierarchy.Root())
		}
	}
	pub.results = results
	pub.hierarchy = hierarchy
	pub.noOverwrite = config.Tracking.NoOverwrite
	// A json or github report on PublishConfig.Output has to stand alone there
	// to be parsed, so the URLs and the compiled bodies give way to it.
	pub.reportShares = config.Report.Output == nil && outputFormat != report.FormatURL
	urlsAreReport := config.Report.Output != nil && outputFormat == report.FormatURL
	// Read only once a parent is missing.
	pub.titles = &runTitles{
		config: config.PublishConfig, inputs: inputs, hierarchy: hierarchy,
		warningsFor: pub.warningsFor,
	}

	// Pages that asked for a position among their siblings, collected as they
	// publish and applied once at the end -- the order of one page only means
	// anything alongside the others.
	var ordered []page.Ordered

	// The files that failed under --continue-on-error, each with its cause.
	failed := &FilesFailedError{}

	// fail records that file failed with err, and returns what the run stops
	// with, or nil when it goes on to the next file.
	// Both passes share pub's warnings, so its failedPage serves either.
	fail := func(file string, err error) error {
		results.AddPage(pub.failedPage(file, err))

		fileErr := &FileError{
			File: file, Source: report.SourceOf(file, sourcePath), Err: err,
		}
		if stepPast(ctx, config.ContinueOnError, file, err) {
			failed.Errors = append(failed.Errors, fileErr)

			return nil
		}

		reported = append(reported, fileErr)

		return fileErr
	}

	for _, in := range inputs {
		file := in.file

		if err := ctx.Err(); err != nil {
			return err
		}

		log.Info().Msgf("processing %s", file)

		target, placement, err := pub.processFile(ctx, in)
		if placement != nil {
			ordered = append(ordered, *placement)
		}
		if err != nil {
			if stop := fail(file, err); stop != nil {
				return stop
			}

			continue
		}

		if target != nil {
			url := api.BaseURL() + target.Links.Full

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
			// page published twice can be reported once, and the URLs give way
			// to them unless the report has a writer of its own. A page left
			// alone is not one this run put there, so it is not named among
			// the URLs of the pages it did.
			if status != report.StatusSkipped {
				if !pub.reportShares {
					if _, err := fmt.Fprintln(config.output(), url); err != nil {
						return err
					}
				}

				// In the url format the URLs are the report.
				if urlsAreReport {
					if _, err := fmt.Fprintln(config.reportOutput(), url); err != nil {
						return fmt.Errorf("unable to write the run report: %w", err)
					}
				}
			}
		}
	}

	// Everything exists now, so the documents that were waiting on a page this
	// run created can be published again with their links resolved.
	if waiting := deferrals.Files(); len(waiting) > 0 && !failed.any() {
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

			if _, _, err := lastLook.processFile(ctx, pub.inputs.named(file)); err != nil {
				// Over what the first pass recorded: the document published
				// then, but the page now holds whatever this pass left it with.
				if stop := fail(file, err); stop != nil {
					return stop
				}
			}
		}
	}

	// Once everything that was going to be published has been, an ac: link that
	// still names no page names none.
	missingLinks, err := pub.checker.MissingLinks(ctx, api)
	if err != nil {
		return err
	}

	var missingPages []string
	for _, link := range missingLinks {
		missingPages = append(missingPages, link.String())
	}

	if config.Links.CheckLinksWarnOnly {
		pub.warnMissingLinks(missingLinks)
	} else {
		for _, item := range missingPages {
			log.Warn().Msg(item)
		}
	}

	// The manifest is saved before the run's own outcome is decided: a single
	// bad file must not throw away the mapping for every page that published,
	// least of all under --continue-on-error, which exists to keep going.
	if !failed.any() && len(ordered) > 0 {
		// Not attempted when a file failed: the pages that did not publish are
		// missing from the sequence, and ordering the rest against each other
		// would arrange them as though the absent ones were gone for good.
		if err := page.OrderChildren(ctx, api, config.Mode.readOnly(), ordered); err != nil {
			return fmt.Errorf("unable to order pages: %w", err)
		}
	}

	if tracker != nil {
		if err := handleOrphans(ctx, tracker, api, config, onOrphan, failed.any(), results); err != nil {
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
	if !config.Links.CheckLinksWarnOnly {
		if err := unresolvedLinks(missingPages); err != nil {
			return err
		}
	}

	if failed.any() {
		// The files are the more useful complaint; a failed manifest write is
		// secondary and must not displace it.
		if saveErr != nil {
			log.Error().Err(saveErr).Msg("page manifest was not saved")
		}

		// Not a failure of its own: it sums up the ones recorded against
		// each document that failed.
		reported = append(reported, failed)

		return failed
	}

	return saveErr
}

// FileError is why one file failed to publish. Its message is the cause's,
// unchanged: the file is in File, and the run's log and report name it beside
// the message.
type FileError struct {
	// File is the path mark read the document from, or a Document's Path.
	File string
	// Source is the file the document was made from, as a Document's Source
	// or ReportConfig.SourcePath names it for the report; empty when it is
	// its own source, and from a Publisher unless a Document named one.
	Source string
	Err    error
}

func (e *FileError) Error() string { return e.Err.Error() }

func (e *FileError) Unwrap() error { return e.Err }

// FilesFailedError is what Run returns when files failed under
// ContinueOnError: one FileError for each, in the order the files were
// processed. errors.Is and errors.As see every file's cause, a timeout of
// its own included, so ctx.Err() is what says whether the run was cancelled.
type FilesFailedError struct {
	Errors []*FileError
}

func (e *FilesFailedError) Error() string { return "one or more files failed to process" }

func (e *FilesFailedError) Unwrap() []error {
	errs := make([]error, len(e.Errors))
	for i, fileErr := range e.Errors {
		errs[i] = fileErr
	}

	return errs
}

func (e *FilesFailedError) any() bool { return len(e.Errors) > 0 }

// onceEach is source asking source about each file only once, or nil when
// source is.
func onceEach(source func(file string) string) func(file string) string {
	if source == nil {
		return nil
	}

	named := map[string]string{}

	return func(file string) string {
		if name, ok := named[file]; ok {
			return name
		}

		name := source(file)
		named[file] = name

		return name
	}
}

// stepPast reports whether the run goes on to the next file after file
// failed with err, which it does only under --continue-on-error, logging the
// failure it steps past.
//
// A file that failed because the run was cancelled is not a file failure to
// step past: the run stops, so that it reports the cancellation.
func stepPast(ctx context.Context, continueOnError bool, file string, err error) bool {
	if !continueOnError || ctx.Err() != nil {
		return false
	}

	log.Error().Err(err).Msgf("processing %s", file)

	return true
}

// Publisher publishes files one at a time for a library caller, keeping
// between them the templates and page header, the folders resolved so far,
// what the link checks learned about external URLs, and the browser and
// mermaid engine pictures are drawn in, each started by the first picture that
// needs it. Close shuts those down. A Publisher is not safe for concurrent
// use.
//
// A Publisher is meant for one batch of files: the folders and link answers it
// keeps are never forgotten, and may go stale. A long-lived program makes a
// new one for each batch rather than holding one open.
//
// A Publisher sees one file at a time, never the run's whole list, so it does
// none of what Run does across files, and PublishConfig has no setting for any
// of it: a page's Position is not applied among its siblings, a document
// linking to a page published after it is not published again once that page
// exists, no page manifest is kept, and no run report is written. To unwatch
// the pages it wrote, build the API with confluence.Options.Unwatch, and call
// its UnwatchWritten after the last file, with a context that is not done yet.
//
// With Page.PageID set, every file would publish over the one page, so a
// Publisher publishes only the first file that reaches the page, as Run
// refuses more than one. A file that fails before then, one that cannot be
// read for instance, leaves the page to the next.
type Publisher struct {
	run *publishRun
}

// errNoPublisher is what Publish says once Close has been called, or on a
// Publisher NewPublisher did not make.
var errNoPublisher = errors.New("mark: Publisher is closed, or was not made by NewPublisher")

// NewPublisher checks config as Run does and prepares to publish with api.
// An edit lock leaves the page editable by the user api authenticates as.
func NewPublisher(ctx context.Context, api *confluence.API, config PublishConfig) (*Publisher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	checks, err := config.prepare()
	if err != nil {
		return nil, err
	}

	return newPublisher(ctx, api, config, checks)
}

// newPublisher builds what Run and NewPublisher both publish with, from
// a config prepare has passed, closing anything it started if that fails.
func newPublisher(
	ctx context.Context, api *confluence.API, config PublishConfig, checks page.LinkChecks,
) (*Publisher, error) {
	res, err := newResources(config.Rendering.MermaidEngine)
	if err != nil {
		return nil, err
	}

	pub, err := newPublishRun(ctx, api, config, res, checks)
	if err != nil {
		res.close()
		return nil, err
	}

	return &Publisher{run: pub}, nil
}

// Publish publishes file, and checks the ac: links it holds once it has.
// Returns nil for the page info when compile-only or dry-run mode is active,
// and a *FileError when the file fails to publish or its links do not resolve.
// A caller looping over files gets what Run does on cancellation by stopping
// when errors.Is(err, ctx.Err()).
func (p *Publisher) Publish(ctx context.Context, file string) (*confluence.PageInfo, error) {
	if p == nil || p.run == nil {
		return nil, errNoPublisher
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	target, err := p.publish(ctx, fileInput(file))
	if err != nil {
		return target, &FileError{File: file, Err: err}
	}

	return target, nil
}

// publish is Publish's work on file, its failures not yet tied to the file.
func (p *Publisher) publish(ctx context.Context, in input) (*confluence.PageInfo, error) {
	// Each file's ac: links are checked, and reported, on their own.
	p.run.checker.ForgetPages()

	target, _, err := p.run.processFile(ctx, in)
	if err != nil {
		return target, err
	}

	missing, err := p.run.checker.MissingPages(ctx, p.run.api)
	if err != nil {
		return target, err
	}

	for _, item := range missing {
		log.Warn().Msg(item)
	}

	if p.run.config.Links.CheckLinksWarnOnly {
		return target, nil
	}

	return target, unresolvedLinks(missing)
}

// Close shuts down the browser and mermaid engine, if a picture started them,
// and refuses every Publish after it. One still starting is cancelled rather
// than waited out. Closing twice, closing nil, or closing a Publisher
// NewPublisher did not make is harmless.
func (p *Publisher) Close() {
	if p == nil || p.run == nil {
		return
	}

	p.run.res.close()
	p.run = nil
}

// newPublishRun builds what every file of a run shares and does not depend on
// the files: the templates, the global properties and the page header. It also
// settles which platform api is, before anything is published.
func newPublishRun(
	ctx context.Context, api *confluence.API, config PublishConfig, res *resources, checks page.LinkChecks,
) (*publishRun, error) {
	// Building the templates cannot reach the network: the "user" template
	// func is only invoked during rendering, and then under the context of the
	// file being rendered, which processFile hands to users.
	users := &runUsers{api: api, ctx: ctx}

	std, err := stdlib.New(ctx, users)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve standard library: %w", err)
	}

	// Read once and before anything is published: a file that cannot be parsed
	// should not be discovered halfway through a repository.
	globalProperties, err := page.LoadGlobalProperties(config.Update.GlobalProperties)
	if err != nil {
		return nil, err
	}

	pageHeader, err := header.Load(ctx, config.Rendering.PageHeader, std, config.Rendering.markdownOptions(res))
	if err != nil {
		return nil, err
	}

	if err := settlePlatform(ctx, api, config.Mode); err != nil {
		return nil, err
	}

	return &publishRun{
		api:              api,
		config:           config,
		res:              res,
		folders:          page.NewFolderCache(),
		users:            users,
		std:              std,
		pageHeader:       pageHeader,
		checker:          page.NewLinkChecker(checks),
		globalProperties: globalProperties,
	}, nil
}

// settlePlatform identifies the Confluence platform once the run's local
// inputs have been checked and before anything is published, so that one
// failed request cannot send a Cloud tenant down Server's paths for the rest
// of the run. A compile-only run never contacts Confluence.
func settlePlatform(ctx context.Context, api *confluence.API, mode ModeConfig) error {
	if mode.CompileOnly {
		return nil
	}

	_, err := api.Cloud(ctx)

	return err
}

// readSource reads a document the way everything that looks at its headers
// has to see it. It returns the source, normalised but otherwise as written,
// which is what the file's fingerprint is taken from, and the markdown with its
// ignored regions removed, which is what the headers are read from, with
// lineOf mapping a line of the markdown to the line of the file, or nil when
// they are the same.
//
// Publishing is not the only reader: --parents-from-path reads a directory's
// document for its title, and a title read from bytes publishing never sees --
// a byte-order mark in front of the header, a header inside an ignored region
// -- is a different title from the one the page is published under.
func readSource(file string) (source, markdown []byte, lineOf func(int) int, err error) {
	if err := checkDocumentPath(file); err != nil {
		return nil, nil, nil, err
	}

	// The path that was checked: the OS would apply a ".." in file after
	// following the symlink in front of it, where the check took it away first.
	source, err = os.ReadFile(absPath(file))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to read file %q: %w", file, err)
	}

	return prepareSource(file, source)
}

// prepareSource is readSource for source as read from file.
func prepareSource(file string, source []byte) (_, markdown []byte, lineOf func(int) int, err error) {
	// A byte-order mark left in front of the first header comment would be
	// reported as "doesn't contain metadata", which is not where the author
	// would look.
	source = mdsource.Normalize(source)

	// Before the headers are read, so that the line numbers in any complaint
	// are the ones in the file the author is looking at rather than offsets
	// into what is left after the header block is taken off. It also means an
	// ignored region is ignored entirely, headers and all, which is what the
	// markers say on the tin.
	markdown, lineOf, err = mdsource.StripIgnoredBlocksLines(source)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to process %q: %w", file, err)
	}

	return source, markdown, lineOf, nil
}

// checkDocumentPath holds a document to the boundary every file it names is
// held to. The root is the working directory for a document named inside it,
// and otherwise the closest directory holding both, rather than the document's
// own, because a glob follows directory symlinks: docs/x/a.md under a link
// docs/x has its directory outside with it.
func checkDocumentPath(file string) error {
	abs := absPath(file)
	if err := attachment.CheckReadable(documentRoot(abs), abs); err != nil {
		return fmt.Errorf("unable to read file %q: %w", file, err)
	}

	return nil
}

// documentRoot is the directory a document is held to. The working directory
// is tried as it is spelled and as it resolves, since a checkout reached
// through a symlink can be named either way. A document sharing nothing with
// it short of the filesystem's root is held to its own directory.
func documentRoot(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Dir(abs)
	}

	root := ""
	for _, dir := range []string{cwd, fspath.ResolveDeepest(cwd)} {
		if _, ok := fspath.Within(dir, abs); ok {
			return dir
		}

		for ; filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
			if _, ok := fspath.Within(dir, abs); ok {
				if len(dir) > len(root) {
					root = dir
				}

				break
			}
		}
	}

	if root == "" {
		return filepath.Dir(abs)
	}

	return root
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
//
// What the metadata ignored is added to warnings; nil only logs it.
func documentMeta(
	config PublishConfig, file string, markdown []byte, hierarchy *page.Hierarchy, warnings *report.Warnings,
) (*metadata.Meta, []byte, error) {
	return readDocumentMeta(hierarchy, markdown, metadata.ExtractOptions{
		Filename:                 file,
		Warnings:                 warnings,
		Space:                    config.Page.Space,
		TitleFromH1:              config.Page.TitleFromH1,
		TitleFromFilename:        config.Page.TitleFromFilename,
		Parents:                  config.Page.Parents,
		TitleAppendGeneratedHash: config.Page.TitleAppendGeneratedHash,
		ContentAppearance:        config.Page.ContentAppearance,
		FrontMatter:              config.Rendering.enabled("frontmatter"),
	})
}

// readDocumentMeta is documentMeta from options already made, which is how a
// link reads the document it points at: its options name the linking
// document's space when the run names none.
func readDocumentMeta(hierarchy *page.Hierarchy, markdown []byte, opts metadata.ExtractOptions) (*metadata.Meta, []byte, error) {
	file, hash := opts.Filename, opts.TitleAppendGeneratedHash

	// Deferred until the path has had its say about the parents, when it has
	// one; see below.
	opts.TitleAppendGeneratedHash = hash && hierarchy == nil

	meta, markdown, err := metadata.ExtractMeta(markdown, opts)
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
		if hash {
			derived, title = page.HashDirectoryTitles(opts.Parents, meta.Space, derived, title)
		}

		if !meta.DeclaredParents {
			meta.Parents = append(meta.Parents, derived...)
		}

		if title != "" {
			meta.Title = title
		} else if hash {
			// Only now, with the parents final: hashed before they were, two
			// documents of one title in different directories hashed the same,
			// and the hash was the very thing meant to tell them apart.
			metadata.AppendGeneratedHash(meta)
		}
	}

	return meta, markdown, nil
}

// publishRun is what every document of a run shares: the connection, the
// settings, and the state built once and threaded through each file. A
// Publisher leaves the run-wide parts nil, and each of them is nil-safe or
// checked where it is used.
type publishRun struct {
	api              *confluence.API
	config           PublishConfig
	res              *resources
	folders          *page.FolderCache
	users            *runUsers
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

	// noOverwrite is Tracking.NoOverwrite, which needs tracker.
	noOverwrite bool
	// reportShares says a json or github report shares PublishConfig.Output,
	// so a compile or dry run prints none of the bodies it compiles.
	reportShares bool

	// pageFile is the file published to Page.PageID, once there is one.
	pageFile string

	// warnings holds each document's warnings for the run's report: a second
	// pass keeps what only the first found, such as a comment it dropped, and
	// a document that fails is reported with what was found before it did.
	// Nil, as a Publisher has it, starts each read afresh.
	warnings map[string]*report.Warnings

	// inputs indexes the run's files and Documents by name, for links
	// between them and for the second pass; nil when it has no Documents.
	inputs *runInputs
}

// runUsers is the "user" template func's lookup for a run's templates and
// page header. They are built once and kept across files, so a lookup is made
// under the context of the file being published rather than the one the
// templates were built under.
type runUsers struct {
	api *confluence.API
	ctx context.Context
}

func (u *runUsers) LookupUser(_ context.Context, name string) (*confluence.User, error) {
	return u.api.LookupUser(u.ctx, name)
}

// document is one source file, or one Document, as read for publishing.
type document struct {
	// input is where it came from: its name, manifest key and root.
	input
	// sourceHash fingerprints the source as read; see readDocument.
	sourceHash string
	// meta is nil when publishing straight to a page id.
	meta     *metadata.Meta
	markdown []byte
	resolver *page.LinkResolver
	// restrictions are the document's declared restrictions, resolved to
	// users and groups; nil when it declares none.
	restrictions *confluence.ResolvedRestrictions
	// warnings are what was wrong with the document but did not stop it,
	// for its line of the report.
	warnings *report.Warnings
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

func (r *publishRun) processFile(ctx context.Context, in input) (*confluence.PageInfo, *page.Ordered, error) {
	file := in.file

	if r.pageFile != "" && r.pageFile != absPath(file) {
		return nil, nil, fmt.Errorf(
			"Page.PageID names one page, which %s publishes to: refusing to publish %s over it",
			r.pageFile, file,
		)
	}

	r.users.ctx = ctx

	doc, err := r.readDocument(in)
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

	doc.resolver, err = r.linkResolver(doc)
	if err != nil {
		return nil, nil, err
	}

	if err := r.resolveRestrictions(ctx, doc); err != nil {
		return nil, nil, err
	}

	if r.config.Mode.readOnly() {
		return nil, nil, r.preview(ctx, doc)
	}

	dest, err := r.resolveDestination(ctx, doc)
	if err != nil {
		return nil, nil, err
	}

	// From here on the page may be written to, so a file failing later has
	// still put something of itself on it.
	r.claimPage(doc.file)

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

	html, err := r.compile(ctx, doc, dest.page)
	if err != nil {
		return nil, nil, err
	}

	versionMessage, shouldUpdatePage := r.changeCheck(doc, dest, html)

	// Only fetch the old body and inline comments when we know the page will
	// actually be updated. This avoids unnecessary API round-trips for no-op
	// runs (e.g. when --changes-only determines the content is unchanged).
	if shouldUpdatePage && r.config.Update.PreserveComments && !dest.created {
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
	r.addPage(doc, report.Page{
		File: doc.file, Status: status,
		Space: spaceOf(doc.meta), Title: target.Title,
		PageID: target.ID, URL: r.api.BaseURL() + target.Links.Full,
	})

	return target, placement, nil
}

// claimPage records file as the one publishing to Page.PageID, once it has
// reached the page. A file that failed before then leaves the page to the next
// one; a dry run claims at the same point a real run does.
func (r *publishRun) claimPage(file string) {
	if r.config.Page.PageID != "" && r.pageFile == "" {
		r.pageFile = absPath(file)
	}
}

// absPath is file as an absolute path, so that one file is one name however
// it was spelled; a name that cannot be made absolute is only cleaned.
func absPath(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return filepath.Clean(file)
	}

	return abs
}

// readDocument reads a file and settles the metadata it publishes under.
func (r *publishRun) readDocument(in input) (*document, error) {
	file := in.file

	source, markdown, lineOf, err := in.read()
	if err != nil {
		return nil, err
	}

	// Fingerprint the source as read, before metadata is stripped and links are
	// substituted. Both of those depend on state outside the file -- what is
	// already published, what other files resolve to -- and a fingerprint that
	// moves with the remote would not survive the round trip it exists for.
	sourceHash := fingerprint.SHA1(string(source))

	warnings := r.warningsFor(file)
	warnings.Reread()
	warnings.MapLines(lineOf)

	meta, markdown, err := documentMeta(r.config, file, markdown, r.hierarchy, warnings)
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

	if r.config.Page.PageID != "" && meta != nil {
		warnings.Add(report.WarningMetadataIgnored,
			"the document's metadata is ignored: the page to publish to is named by id")
		meta = nil
	}

	return &document{
		input: in, sourceHash: sourceHash, meta: meta, markdown: markdown, warnings: warnings,
	}, nil
}

// addPage reports what became of doc, with its warnings both as the messages
// Page.Warnings holds and in full.
func (r *publishRun) addPage(doc *document, page report.Page) {
	r.results.AddPage(withWarnings(page, doc.warnings))
}

// warnMissingLinks adds each ac: link that still names no page, now that the
// run has published everything it will, to the warnings of every document
// holding it, and puts those into the documents' lines of the report.
func (r *publishRun) warnMissingLinks(links []page.MissingLink) {
	for _, link := range links {
		for _, source := range link.Sources {
			warnings, known := r.warnings[source]
			if !known {
				// Not a document this run read; logged against it all the same.
				report.NewWarnings(source).Add(report.WarningUnresolvedLink, "%s", link.Message())

				continue
			}

			warnings.Add(report.WarningUnresolvedLink, "%s", link.Message())
		}
	}
}

// warningsFor is the collector of file's warnings: the run's own when it keeps
// one, which a Publisher does not, or else a new one.
func (r *publishRun) warningsFor(file string) *report.Warnings {
	if warnings, ok := r.warnings[file]; ok {
		return warnings
	}

	warnings := report.NewWarnings(file)
	if r.warnings != nil {
		r.warnings[file] = warnings
	}

	return warnings
}

// flushWarnings puts each document's warnings as they stand into its line of
// the report. Some are found after that line is written -- an ac: link checked
// at the end, a page that could not be ordered -- so the report is brought up
// to date once, on the way out, however the run ends.
func (r *publishRun) flushWarnings() {
	for file, warnings := range r.warnings {
		r.results.SetWarnings(file, warnings.List())
	}
}

// failedPage is the report of a file that failed with err, with what was
// found wrong with it before it did.
func (r *publishRun) failedPage(file string, err error) report.Page {
	return withWarnings(report.Page{
		File: file, Status: report.StatusFailed, Reason: err.Error(),
	}, r.warnings[file])
}

// withWarnings is page with warnings both as the messages Page.Warnings
// holds and in full.
func withWarnings(page report.Page, warnings *report.Warnings) report.Page {
	page.WarningDetails = warnings.List()
	for _, warning := range page.WarningDetails {
		page.Warnings = append(page.Warnings, warning.Message)
	}

	return page
}

// leaveUnsynchronized reports, and returns true for, a document that has
// opted out of being published.
func (r *publishRun) leaveUnsynchronized(ctx context.Context, doc *document) (bool, error) {
	meta, file := doc.meta, doc.file
	if meta.Publish() {
		return false, nil
	}

	log.Info().Msgf("%s is not synchronized; leaving it alone", file)
	r.addPage(doc, report.Page{
		File: file, Status: report.StatusSkipped,
		Reason: report.ReasonNotSynchronized,
		Space:  spaceOf(meta), Title: titleOf(meta),
	})

	// Looked up purely to mark the path as seen. A page somebody stopped
	// synchronising is one they chose to keep, and a run that did not
	// account for the file would read it as gone: the page is reported as
	// having no source file and its mapping is dropped, so synchronising it
	// again later would no longer know which page was already its own.
	if r.tracker != nil && meta != nil && meta.Space != "" {
		if _, _, err := r.tracker.Lookup(ctx, meta.Space, doc.key); err != nil {
			return true, fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
		}
	}

	return true, nil
}

// validate refuses a document that names no page to publish to.
func (r *publishRun) validate(doc *document) error {
	meta := doc.meta

	if r.config.Page.PageID == "" && meta == nil {
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
func (r *publishRun) linkResolver(doc *document) (*page.LinkResolver, error) {
	config, file := r.config, doc.file

	sourceDir, err := config.Links.checkStaged(file)
	if err != nil {
		return nil, err
	}

	// A fragment's links are written from where the fragment lives, so the
	// directory of everything this document includes is worth looking in too.
	// One level: a fragment that itself includes another is not followed here.
	base := filepath.Dir(file)
	searchDirs := includeSearchDirs(base, config.Rendering.IncludePath, doc.markdown)

	// A staged document's or fragment's links were written beside its source.
	// Looked for there only after every staged place, so that setting the
	// roots cannot change what a link that already resolved names.
	var sourceDirs []string
	if sourceDir != "" {
		sourceDirs = append(sourceDirs, sourceDir)
	}
	for _, dir := range searchDirs {
		if mapped, ok := config.Links.sourceDir(dir); ok {
			sourceDirs = append(sourceDirs, mapped)
		}
	}
	var checkOnlyDirs []string
	if config.Links.SourceRootCheckOnly {
		checkOnlyDirs = sourceDirs
	} else {
		searchDirs = append(searchDirs, sourceDirs...)
	}

	resolver := page.NewLinkResolver(
		r.api,
		doc.meta,
		base,
		metadata.ExtractOptions{
			Space:                    config.Page.Space,
			TitleFromH1:              config.Page.TitleFromH1,
			TitleFromFilename:        config.Page.TitleFromFilename,
			Parents:                  config.Page.Parents,
			TitleAppendGeneratedHash: config.Page.TitleAppendGeneratedHash,
			FrontMatter:              config.Rendering.enabled("frontmatter"),
		},
		r.checker,
		searchDirs,
	)
	resolver.CheckOnlyDirs = checkOnlyDirs
	resolver.Offline = config.Mode.Offline
	resolver.SourceFile = file
	resolver.Deferrals = r.deferrals
	resolver.Files = r.linkFiles(doc)
	resolver.Warnings = doc.warnings
	resolver.ReadMeta = func(markdown []byte, opts metadata.ExtractOptions) (*metadata.Meta, []byte, error) {
		// The hierarchy places only the run's own files. Another is published
		// by a run that lays out its own, so its headers are all it says here.
		hierarchy := r.hierarchy
		if !hierarchy.Has(opts.Filename) {
			hierarchy = nil
		}

		return readDocumentMeta(hierarchy, markdown, opts)
	}

	return resolver, nil
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
	if doc.meta == nil || doc.meta.Restrictions == nil || r.config.Mode.CompileOnly {
		return nil
	}

	resolved, err := r.api.ResolveRestrictions(ctx, declaredRestrictions(doc.meta.Restrictions))
	if err != nil {
		return fmt.Errorf("unable to resolve the restrictions of %q: %w", doc.file, err)
	}
	doc.restrictions = resolved

	return nil
}

// ancestryOptions are the run's settings for resolving doc's page's
// ancestry. DryRun stays unset: PreviewPage is a dry run whatever it is given.
func (r *publishRun) ancestryOptions(doc *document) page.AncestryOptions {
	return page.AncestryOptions{
		FolderCache: r.folders,
		Parents:     r.ancestryTracker,
		Folders:     r.ancestryTracker,
		Warnings:    doc.warnings,
	}
}

// previewLocation finds, without changing anything, the page a dry run's
// document would publish to.
func (r *publishRun) previewLocation(ctx context.Context, doc *document) (pagePreview, error) {
	var found pagePreview
	meta, file := doc.meta, doc.file

	if meta == nil {
		if r.config.Page.PageID != "" {
			pg, err := r.api.GetPageByID(ctx, r.config.Page.PageID)
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
	if err := refreshStaleParents(ctx, r.tracker, r.api, meta, r.titles, doc.warnings); err != nil {
		return found, err
	}

	parent, pg, misplaced, err := page.PreviewPage(ctx, r.api, meta, r.ancestryOptions(doc))
	if err != nil {
		return found, fmt.Errorf("unable to resolve page location: %w", err)
	}

	if pg == nil {
		// The title found nothing, which is where a real run consults
		// the manifest. Saying so is the whole point of a dry run:
		// otherwise it reports a new page for every rename and retitle
		// the run would actually have handled in place.
		found.page, err = previewTrackedResolution(ctx, r.tracker, r.api, doc)
		if err != nil {
			return found, err
		}
		// Not found by title, so ResolvePage's own move does not apply.
		found.parent = parent
	} else {
		found.page = pg
		found.parent, found.misplaced = parent, misplaced
	}

	if r.tracker != nil && found.page != nil {
		// A real run records the path once it has published, and that is
		// what keeps the path from being read as an orphan, and what hands
		// a renamed file's old entry, --no-overwrite baseline included, to
		// the new path. A dry run returns before then, so it records here;
		// its store never saves.
		if err := r.tracker.Record(ctx, meta.Space, doc.key, found.page.ID, meta.Title, doc.sourceHash); err != nil {
			return found, fmt.Errorf("unable to record page mapping for %q: %w", file, err)
		}
	}

	return found, nil
}

// notePublishes counts the pages a real run would have created for meta by
// the time ac: links are looked up -- its own and every parent, which a real
// run creates before it compiles the document, so whether or not the document
// then fails -- as found. A run that previews never creates them, and an ac:
// link to one is not missing.
func (r *publishRun) notePublishes(meta *metadata.Meta) {
	if meta == nil {
		return
	}

	for _, parent := range meta.Parents {
		r.checker.NotePublishes(meta.Space, parent)
	}

	if meta.Type == string(confluence.ContentPage) {
		r.checker.NotePublishes(meta.Space, meta.Title)
	}
}

// preview is the whole of a compile-only or dry run for one document: it
// compiles the page, checks it as a real run would, and prints it or, under
// --changes-only, says whether the page would change. Either way the document
// is recorded in the report, with its warnings.
func (r *publishRun) preview(ctx context.Context, doc *document) error {
	config, meta, file := r.config, doc.meta, doc.file

	var found pagePreview
	if config.Mode.DryRun {
		var err error
		found, err = r.previewLocation(ctx, doc)
		if err != nil {
			return err
		}
	}
	previewed := found.page
	r.claimPage(file)
	r.notePublishes(meta)

	cfg, err := config.Rendering.documentConfig(ctx, r.res, doc)
	if err != nil {
		return err
	}

	// A dry run says what a real run would do; a compile, given --dry-run or
	// not, only how far the document got. Only a dry run asked to tell
	// changed pages from unchanged ones compares.
	dryRun := config.Mode.DryRun && !config.Mode.CompileOnly
	compare := dryRun && config.Update.ChangesOnly
	uncompared := report.StatusCompiled
	if dryRun {
		uncompared = report.StatusWouldPublish
	}

	var status report.Status
	var reason string
	if dryRun && r.noOverwrite && previewed != nil && r.tracker != nil && meta != nil {
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
		if !compare {
			status = uncompared
		}
	}

	// A json or github report going to the same stream is for a CI step to
	// read, so the HTML gives way to it: ahead of json it would not parse,
	// and ahead of github a line of it could be a workflow command of the
	// document's own, such as a forged ::error or a ::stop-commands that
	// hides the report's annotations.
	if status != report.StatusUnchanged && status != report.StatusSkipped && !r.reportShares {
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
		if err := markdown.CheckWellFormed(html); err != nil {
			return err
		}
	}

	// Said here too, and for the same reason the check above is: a run told to
	// check links should report them on the credential-free invocation
	// --compile-only exists for, and the one a pull request gate is most
	// likely to use.
	if err := reportBrokenLinks(doc.resolver.Broken(), doc.warnings, config.Links.CheckLinksWarnOnly); err != nil {
		return err
	}

	var target confluence.PageInfo
	var url string
	if previewed != nil {
		target = *previewed
		// Built as a real run builds it, so CI can link the page.
		url = r.api.BaseURL() + target.Links.Full
	}
	title := target.Title
	if meta != nil {
		title = meta.Title
	}
	r.addPage(doc, report.Page{
		File: file, Status: status, Reason: reason,
		Space: spaceOf(meta), Title: title,
		PageID: target.ID, URL: url,
	})

	return nil
}

// previewBody compiles a previewed page and, when compare is set, says
// whether a real run would change it.
func (r *publishRun) previewBody(
	ctx context.Context,
	doc *document, found pagePreview, cfg markdown.Options, compare bool,
) (html string, status report.Status, reason string, err error) {
	meta, file, previewed := doc.meta, doc.file, found.page

	// Read once, for the preview and for the clash check below.
	var declared []attachment.Attachment
	var attachments *attachmentPreview
	if compare {
		declared, err = declaredAttachments(doc.root, file, meta)
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

	html, inline, err := markdown.CompileMarkdown(ctx, doc.markdown, r.std, file, cfg)
	if err != nil {
		return "", "", "", fmt.Errorf("unable to compile markdown: %w", err)
	}

	var previewedTitle string
	if previewed != nil {
		previewedTitle = previewed.Title
	}

	headerHTML, headerAttachments, err := renderHeader(ctx, r.pageHeader, file, meta, previewedTitle, r.config.Page.Space, cfg)
	if err != nil {
		return "", "", "", err
	}
	html = headerHTML + html

	// The clash a real run refuses, so that this one does not report
	// success for a page that run will fail on. Declared files are read
	// only when there is something to clash with them.
	if len(headerAttachments) > 0 || len(inline) > 0 {
		if !compare {
			declared, err = declaredAttachments(doc.root, file, meta)
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
		pg, err := r.api.GetPageByID(ctx, r.config.Page.PageID)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve page by id: %w", err)
		}
		if pg == nil {
			return nil, fmt.Errorf("page with id %q not found", r.config.Page.PageID)
		}
		dest = &destination{page: pg}
	}

	if target := dest.page; target != nil && target.Type == "" {
		if doc.meta != nil && doc.meta.Type != "" {
			target.Type = confluence.ContentType(doc.meta.Type)
		} else {
			target.Type = confluence.ContentPage
		}
	}

	return dest, nil
}

// resolveDocumentPage finds the page a document's metadata names, following
// renames the manifest knows of, and creates or relocates it under the
// parents the document declares.
func (r *publishRun) resolveDocumentPage(ctx context.Context, doc *document) (*destination, error) {
	api, meta := r.api, doc.meta
	dest := &destination{}

	// Parents are named by title, so a parent that has itself been renamed
	// leaves this document pointing at a name nothing carries any more.
	// Do this before resolution, so ancestry is walked against titles that
	// exist rather than creating an empty page under the old one.
	if err := refreshStaleParents(ctx, r.tracker, api, meta, r.titles, doc.warnings); err != nil {
		return nil, err
	}

	parent, pg, err := page.ResolvePage(ctx, api, meta, r.ancestryOptions(doc))
	if err != nil {
		return nil, fmt.Errorf("error resolving page %q: %w", meta.Title, err)
	}
	dest.parent = parent

	// The title lookup found nothing, which is how both "this page is new"
	// and "this page was renamed" look. Ask the manifest which of the two
	// this is before creating a second page beside the first.
	if pg == nil {
		pg, err = resolveTrackedPage(ctx, r.tracker, api, doc)
		if err != nil {
			return nil, err
		}
		if pg == nil {
			// Not published under this path before. It may have been
			// published under another: a renamed file is a new key holding
			// an old document.
			pg, err = resolveRenamedFile(ctx, r.tracker, api, doc)
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
			pg, err = api.CreatePageWithFolderParent(ctx, meta.Space, confluence.ContentType(meta.Type), parent.ID, meta.Title, ``)
		} else {
			pg, err = api.CreatePage(ctx, meta.Space, confluence.ContentType(meta.Type), parent, meta.Title, ``)
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

	plan, err := r.api.PlanRestrictions(ctx, dest.page, doc.restrictions, r.config.Update.EditLock, dest.created)
	if err != nil {
		return nil, err
	}
	if err := r.api.ApplyInterim(ctx, plan); err != nil {
		return nil, fmt.Errorf("unable to set page restrictions: %w", err)
	}

	return plan, nil
}

// compile uploads the document's attachments, compiles it, and returns the
// body to send, wrapped in its layout. The broken links the run was told to
// warn about rather than fail on go to the document's warnings.
func (r *publishRun) compile(ctx context.Context, doc *document, target *confluence.PageInfo) (string, error) {
	api, meta, file := r.api, doc.meta, doc.file

	// Collect attachments declared via <!-- Attachment: --> directives.
	var declared []string
	if meta != nil {
		declared = meta.Attachments
	}

	localAttachments, err := attachment.ResolveLocalAttachments(
		doc.root,
		filepath.Dir(file),
		declared,
	)
	if err != nil {
		return "", fmt.Errorf("unable to locate attachments: %w", err)
	}

	cfg, err := r.config.Rendering.documentConfig(ctx, r.res, doc)
	if err != nil {
		return "", err
	}

	// With --page-id the file's metadata is discarded, so the page itself and
	// --space are what the header can name.
	var targetTitle string
	if target != nil {
		targetTitle = target.Title
	}

	headerHTML, headerAttachments, err := renderHeader(ctx, r.pageHeader, file, meta, targetTitle, r.config.Page.Space, cfg)
	if err != nil {
		return "", err
	}

	// The header's own files are known now, so a clash with a declared one is
	// refused before anything is uploaded. One with a file the document only
	// references can be seen once it is compiled, which needs these uploaded.
	if _, err := withHeaderAttachments(file, localAttachments, nil, headerAttachments); err != nil {
		return "", err
	}

	// The page's remote attachment list is fetched once and threaded through
	// both resolve passes: declared attachments here, then diagrams discovered
	// while rendering.
	remoteAttachments, err := api.GetAttachments(ctx, target.ID)
	if err != nil {
		return "", fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	attaches, remoteAttachments, err := uploadAttachments(
		ctx,
		api, target, localAttachments, remoteAttachments,
	)
	if err != nil {
		return "", fmt.Errorf("unable to create/update attachments: %w", err)
	}

	attachmentLinks := attachment.NewResolver(attaches)

	cfg.ResolveAttachment = attachmentLinks.Resolve

	html, inlineAttachments, err := markdown.CompileMarkdown(ctx, doc.markdown, r.std, file, cfg)
	if err != nil {
		return "", fmt.Errorf("unable to compile markdown: %w", err)
	}

	html = headerHTML + html
	inlineAttachments, err = withHeaderAttachments(file, attaches, inlineAttachments, headerAttachments)
	if err != nil {
		return "", err
	}

	if err := reportBrokenLinks(doc.resolver.Broken(), doc.warnings, r.config.Links.CheckLinksWarnOnly); err != nil {
		return "", err
	}

	// Only knowable once the document has been walked.
	for _, unused := range attachmentLinks.Unused(attaches) {
		doc.warnings.Add(report.WarningAttachment, "unused attachment: %s", unused)
	}

	if _, _, err = uploadAttachments(
		ctx,
		api, target, inlineAttachments, remoteAttachments,
	); err != nil {
		return "", fmt.Errorf("unable to create/update attachments: %w", err)
	}

	var layout, sidebar string
	if meta != nil {
		layout = meta.Layout
		sidebar = meta.Sidebar
	}

	html, err = wrapLayout(r.std, layout, sidebar, html)
	if err != nil {
		return "", err
	}

	return html, nil
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

	html, err = comments.MergeWithWarnings(html, pg.Body.Storage.Value, inlineComments, doc.warnings)
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
	if err := markdown.CheckWellFormed(html); err != nil {
		return err
	}

	contentAppearance, emoji := appearanceOf(doc.meta)

	if err := r.api.UpdatePage(
		ctx,
		target,
		html,
		confluence.UpdateOptions{
			MinorEdit:      r.config.Update.MinorEdit,
			VersionMessage: versionMessage,
			Appearance:     contentAppearance,
			Emoji:          emoji,
		},
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
		if err := r.api.UpdateLabels(ctx, target, meta.Labels, r.config.Update.AppendLabels); err != nil {
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
			Warnings: doc.warnings,
		}
	}

	if r.config.Update.EditLock {
		log.Info().Msgf(
			`edit locked on page %q by the authenticated user to prevent manual edits`,
			target.Title,
		)
		// The user r.api logs in as, which is not necessarily the one
		// Connection names: a Publisher is handed its api.
		if err := r.api.RestrictPageUpdates(ctx, target, ""); err != nil {
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
		normalised, ok := rendercheck.ImageAlign(align)
		if !ok {
			return "", fmt.Errorf(
				`unknown image-align %q, expected one of: left, center, right`,
				normalised,
			)
		}
		return normalised, nil
	}

	return "", nil
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
		// the fragment itself was found. Spelled like base, absolute or
		// relative, so that a file found there is recognised as the run's.
		if includePath != "" {
			add(spelledLike(filepath.Dir(filepath.Join(includePath, target)), base))
		}
	}

	return dirs
}

// spelledLike writes path the way like is written: absolute, or relative to
// the working directory. A path it cannot rewrite is returned as it is.
func spelledLike(path, like string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	if filepath.IsAbs(like) {
		return absolute
	}

	cwd, err := os.Getwd()
	if err != nil {
		return path
	}

	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		return path
	}

	return relative
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

	return string(meta.ContentAppearance), meta.Emoji
}

// renderHeader renders the page header for one document, naming it by its
// metadata where it has some and by fallbackTitle and fallbackSpace where not.
func renderHeader(
	ctx context.Context, pageHeader *header.Header, file string, meta *metadata.Meta, fallbackTitle, fallbackSpace string, cfg markdown.Options,
) (string, []attachment.Attachment, error) {
	return pageHeader.Render(ctx, file, cmp.Or(titleOf(meta), fallbackTitle), cmp.Or(spaceOf(meta), fallbackSpace), cfg)
}

// documentConfig is the compile configuration for one document: the run's,
// with what its metadata, link resolution and warnings add.
func (c RenderingConfig) documentConfig(ctx context.Context, res *resources, doc *document) (markdown.Options, error) {
	meta := doc.meta

	if c.DropH1 {
		log.Info().Msg("the leading H1 heading will be excluded from the Confluence output")
	}

	imageAlign, err := getImageAlign(c.ImageAlign, meta)
	if err != nil {
		return markdown.Options{}, fmt.Errorf("unable to determine image-align: %w", err)
	}

	cfg := c.markdownOptions(res)
	cfg.DropH1 = c.DropH1
	cfg.ImageAlign = imageAlign
	cfg.ResolveLink = linksUnder(ctx, doc.resolver)
	cfg.Warnings = doc.warnings
	cfg.Root = doc.root

	return cfg, nil
}

// markdownOptions is the compile configuration the run's flags give every
// document, drawing in the run's browsers; the per-document fields are left
// for the caller to fill in.
func (c RenderingConfig) markdownOptions(res *resources) markdown.Options {
	// An invalid value is reported by documentConfig, which every page passes.
	imageAlign, _ := getImageAlign(c.ImageAlign, nil)

	return markdown.Options{
		Browser:          res.browser,
		Mermaid:          res.mermaid,
		ImageAlign:       imageAlign,
		MermaidScale:     c.MermaidScale,
		MermaidOutput:    c.MermaidOutput,
		MermaidBundle:    c.MermaidBundle,
		D2Output:         c.D2Output,
		D2Scale:          c.D2Scale,
		D2BundleRemote:   c.D2BundleRemote,
		MathOutput:       c.MathOutput,
		MathScale:        c.MathScale,
		StripLinebreaks:  c.StripLinebreaks,
		Features:         c.Features,
		AttachReferenced: c.AttachReferenced,
		IncludePath:      c.IncludePath,
	}
}
