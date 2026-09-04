package util

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrctoml "github.com/urfave/cli-altsrc/v3/toml"
	"github.com/urfave/cli/v3"
)

var filename string

// KnownFeatures are the values --features accepts. Every one of them is read
// with a plain slices.Contains at the point it takes effect, so a name that is
// not on this list is indistinguishable from one deliberately left off: the
// feature is simply never switched on, and nothing is said about it.
//
// The usage text is built from this list, so the two cannot drift.
var KnownFeatures = []string{
	"d2",
	"date",
	"emoji",
	"frontmatter",
	"inline-link-card",
	"math",
	"mention",
	"mermaid",
	"mkdocsadmonitions",
	"plantuml",
}

// defaultFeatures are what --features replaces when it is given at all, which
// is worth saying in the usage: enabling one feature silently turns the others
// off.
var defaultFeatures = []string{"mermaid", "mention"}

var Flags = []cli.Flag{
	// First, and it has to stay first. Every other flag finds the
	// configuration file through the pointer below, which is filled in as the
	// flags are resolved -- in the order they are declared. Anything declared
	// above this looks for its TOML value while that pointer is still empty and
	// silently gets nothing.
	//
	// A path on the command line happened to survive being declared late. One
	// in MARK_CONFIG did not, so "MARK_CONFIG=/etc/mark.toml mark" ignored
	// files, username, password, target-url, base-url and log-level while
	// honouring space, parents and features -- reported, if at all, as
	// "confluence password should be specified using -p flag".
	&cli.StringFlag{
		Name:        "config",
		Aliases:     []string{"c"},
		Value:       ConfigFilePath(),
		Usage:       "use the specified configuration file.",
		TakesFile:   true,
		Sources:     cli.NewValueSourceChain(cli.EnvVar("MARK_CONFIG")),
		Destination: &filename,
	},
	&cli.StringFlag{
		Name:      "files",
		Aliases:   []string{"f"},
		Value:     "",
		Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
		TakesFile: true,
		Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_FILES"), altsrctoml.TOML("files", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "continue-on-error",
		Value:   false,
		Usage:   "don't exit if an error occurs while processing a file, continue processing remaining files.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CONTINUE_ON_ERROR"), altsrctoml.TOML("continue-on-error", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "compile-only",
		Value:   false,
		Usage:   "show resulting HTML and don't update Confluence page content.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_COMPILE_ONLY"), altsrctoml.TOML("compile-only", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "dry-run",
		Value:   false,
		Usage:   "resolve page and ancestry, show resulting HTML and exit.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_DRY_RUN"), altsrctoml.TOML("dry-run", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "edit-lock",
		Value:   false,
		Aliases: []string{"k"},
		Usage:   "lock page editing to current user only to prevent accidental manual edits over Confluence Web UI.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_EDIT_LOCK"), altsrctoml.TOML("edit-lock", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "drop-h1",
		Value:   false,
		Usage:   "don't include the first H1 heading in Confluence output.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_DROP_H1"), altsrctoml.TOML("drop-h1", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "strip-linebreaks",
		Value:   false,
		Aliases: []string{"L"},
		Usage:   "remove linebreaks inside of tags, to accommodate non-standard Confluence behavior",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_STRIP_LINEBREAKS"), altsrctoml.TOML("strip-linebreaks", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "title-from-h1",
		Value:   false,
		Usage:   "extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata. Mutually exclusive with --title-from-filename.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_FROM_H1"), altsrctoml.TOML("title-from-h1", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "title-from-filename",
		Value:   false,
		Usage:   "use the filename (without extension) as the Confluence page title if no explicit page title is set in the metadata. Mutually exclusive with --title-from-h1.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_FROM_FILENAME"), altsrctoml.TOML("title-from-filename", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "title-append-generated-hash",
		Value:   false,
		Usage:   "appends a short hash generated from the path of the page (space, parents, and title) to the title",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_APPEND_GENERATED_HASH"), altsrctoml.TOML("title-append-generated-hash", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "minor-edit",
		Value:   false,
		Usage:   "don't send notifications while updating Confluence page.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MINOR_EDIT"), altsrctoml.TOML("minor-edit", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "version-message",
		Value:   "",
		Usage:   "add a message to the page version, to explain the edit (default: \"\")",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_VERSION_MESSAGE"), altsrctoml.TOML("version-message", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:  "color",
		Value: "auto",
		Usage: "display logs in color. Possible values: auto, never.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_COLOR"),
			altsrctoml.TOML("color", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "log-level",
		Value:   "info",
		Usage:   "set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_LOG_LEVEL"), altsrctoml.TOML("log-level", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "username",
		Aliases: []string{"u"},
		Value:   "",
		Usage:   "use specified username for updating Confluence page.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_USERNAME"),
			altsrctoml.TOML("username", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "password",
		Aliases: []string{"p"},
		Value:   "",
		Usage:   "use specified token for updating Confluence page. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PASSWORD"), altsrctoml.TOML("password", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "password-command",
		Value:   "",
		Usage:   "run the specified command and use the first line of its stdout as the token for updating Confluence page. Runs without a shell. Mutually exclusive with password.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PASSWORD_COMMAND"), altsrctoml.TOML("password-command", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "target-url",
		Aliases: []string{"l"},
		Value:   "",
		Usage:   "edit specified Confluence page. If -l is not specified, file should contain metadata (see above).",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TARGET_URL"), altsrctoml.TOML("target-url", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "base-url",
		Aliases: []string{"b"},
		Value:   "",
		Usage:   "base URL for Confluence. Alternative option for base_url config field.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_BASE_URL"),
			altsrctoml.TOML("base-url", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "ci",
		Value:   false,
		Usage:   "run on CI mode. It won't fail if files are not found.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CI"), altsrctoml.TOML("ci", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "space",
		Value:   "",
		Usage:   "use specified space key. If the space key is not specified, it must be set in the page metadata.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_SPACE"), altsrctoml.TOML("space", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "parents",
		Value:   "",
		Usage:   "A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be prepended to the ones defined in the document itself.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS"), altsrctoml.TOML("parents", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "parents-delimiter",
		Value:   "/",
		Usage:   "The delimiter used for the parents list",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS_DELIMITER"), altsrctoml.TOML("parents-delimiter", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:  "content-appearance",
		Value: "",
		Usage: "default content appearance for pages without a Content-Appearance header. Possible values: full-width, fixed, default.",
		Sources: cli.NewValueSourceChain(
			cli.EnvVar("MARK_CONTENT_APPEARANCE"),
			altsrctoml.TOML("content-appearance", altsrc.NewStringPtrSourcer(&filename)),
		),
	},
	&cli.FloatFlag{
		Name:    "mermaid-scale",
		Value:   1.0,
		Usage:   "defines the scaling factor for mermaid PNG renderings; not accepted when mermaid-output is svg.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_SCALE"), altsrctoml.TOML("mermaid-scale", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "mermaid-output",
		Value:   "png",
		Usage:   "image a mermaid diagram is published as: png (rasterised, and scaled by --mermaid-scale) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment).",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_OUTPUT"), altsrctoml.TOML("mermaid-output", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "mermaid-bundle",
		Value:   false,
		Usage:   "keep the diagram's own source inside the SVG published for it, in its <desc> element, so the drawing can be edited again from the attachment. Needs --mermaid-output=svg.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_BUNDLE"), altsrctoml.TOML("mermaid-bundle", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "math-format",
		Value:   "png",
		Usage:   "image a formula is published as with --features=math: png (rasterised through the same headless Chrome mermaid uses) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment).",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MATH_FORMAT"), altsrctoml.TOML("math-format", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.FloatFlag{
		Name:    "math-scale",
		Value:   2.0,
		Usage:   "defines the scaling factor for PNG formula renderings; ignored when math-format is svg.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MATH_SCALE"), altsrctoml.TOML("math-scale", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:      "include-path",
		Value:     "",
		Usage:     "Path for shared includes, used as a fallback if the include doesn't exist in the current directory.",
		TakesFile: true,
		Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_INCLUDE_PATH"), altsrctoml.TOML("include-path", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "changes-only",
		Value:   false,
		Usage:   "Avoids re-uploading pages that haven't changed since the last run.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHANGES_ONLY"), altsrctoml.TOML("changes-only", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:  "output-format",
		Value: "url",
		Usage: "how to report what the run did: \"url\" prints the address of each published page (the default), \"json\" prints one object describing the whole run, \"github\" prints GitHub Actions workflow commands so that failures appear against the file that caused them.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_OUTPUT_FORMAT"),
			altsrctoml.TOML("output-format", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:  "on-orphan",
		Value: "report",
		Usage: "what to do about a page whose source file is gone: \"report\" says so and does nothing (the default), \"archive\" archives the page (Confluence Cloud only), \"delete\" moves it to the trash. Requires --track-pages.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_ON_ORPHAN"),
			altsrctoml.TOML("on-orphan", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:  "orphan-under",
		Value: "",
		Usage: "limit --on-orphan, and the reporting it does, to pages below this page or folder, given by title or id. Without it, every tracked page the --files pattern would have published is in scope.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_ORPHAN_UNDER"),
			altsrctoml.TOML("orphan-under", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringSliceFlag{
		Name:  "check-links",
		Usage: "fail on links that do not resolve. Repeat or comma-separate any of: \"internal\" (relative links to other files in the repository), \"confluence\" (ac: links naming a page by title), \"external\" (requests each URL to see whether it answers), or \"all\".",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHECK_LINKS"),
			altsrctoml.TOML("check-links", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:      "global-properties",
		Value:     "",
		Usage:     "path to a YAML or JSON file of Confluence content properties to set on every page. A Property header or properties front matter in a document wins over the file for that page.",
		TakesFile: true,
		Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_GLOBAL_PROPERTIES"), altsrctoml.TOML("global-properties", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "append-labels",
		Value:   false,
		Usage:   "add the labels a document asks for without removing any others, so that labels applied in Confluence survive a publish. Without it, a page ends up with exactly the labels its Label headers name.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_APPEND_LABELS"), altsrctoml.TOML("append-labels", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "check-links-warn-only",
		Value:   false,
		Usage:   "report links that do not resolve without failing the run. Only meaningful together with --check-links.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHECK_LINKS_WARN_ONLY"), altsrctoml.TOML("check-links-warn-only", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "no-overwrite",
		Value:   false,
		Usage:   "Leave alone any page that has been edited in Confluence since mark last published it, instead of overwriting the edit. Requires --track-pages, which is where the last published version is remembered.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_NO_OVERWRITE"), altsrctoml.TOML("no-overwrite", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "track-pages",
		Value:   false,
		Usage:   "Remember which page each file publishes to, so renaming a file or changing its title updates the existing page instead of creating a second one. Stores the mapping in Confluence (a space property on Cloud, a homepage content property on Server/Data Center); nothing is written to the repository.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TRACK_PAGES"), altsrctoml.TOML("track-pages", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "preserve-comments",
		Value:   false,
		Usage:   "Fetch and preserve inline comments on existing Confluence pages.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PRESERVE_COMMENTS"), altsrctoml.TOML("preserve-comments", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "d2-output",
		Value:   "png",
		Usage:   "image a d2 diagram is published as: png (rasterised) or svg (vector and sharp at any zoom, with whatever the diagram references inlined into it, where the instance displays an SVG attachment).",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_OUTPUT"), altsrctoml.TOML("d2-output", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.FloatFlag{
		Name:    "d2-scale",
		Value:   1.0,
		Usage:   "defines the scaling factor for d2 renderings: the pixels of a png, and the size the page displays an svg at.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_SCALE"), altsrctoml.TOML("d2-scale", altsrc.NewStringPtrSourcer(&filename))),
	},

	&cli.StringSliceFlag{
		Name:  "features",
		Value: []string{"mermaid", "mention"},
		Usage: "Enables optional features, replacing the defaults (" +
			strings.Join(defaultFeatures, ", ") + ") rather than adding to them. " +
			"Current features: " + strings.Join(KnownFeatures, ", "),
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_FEATURES"), altsrctoml.TOML("features", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.BoolFlag{
		Name:    "insecure-skip-tls-verify",
		Value:   false,
		Usage:   "skip TLS certificate verification (useful for self-signed certificates)",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_INSECURE_SKIP_TLS_VERIFY"), altsrctoml.TOML("insecure-skip-tls-verify", altsrc.NewStringPtrSourcer(&filename))),
	},
	&cli.StringFlag{
		Name:    "image-align",
		Value:   "",
		Usage:   "set image alignment (left, center, right). Can be overridden per-file via the Image-Align header.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_IMAGE_ALIGN"), altsrctoml.TOML("image-align", altsrc.NewStringPtrSourcer(&filename))),
	},
}

// CheckFlags validates combinations and values of global flags.
// CheckConfigFile reports a configuration file that cannot be used.
//
// Settings are read from the file lazily, one flag at a time, and a file that
// cannot be parsed simply yields nothing. A stray syntax error therefore
// removes every setting at once and says nothing, and the first sign of trouble
// is whichever required value went missing with it -- so "confluence password
// should be specified" is what an unquoted list three lines further down looks
// like, which is a long way from where the problem is.
func CheckConfigFile(command *cli.Command) error {
	path := command.String("config")
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing at the default location is the ordinary case: most
			// people pass flags. A path somebody named themselves is
			// different, because there silence makes a typo indistinguishable
			// from a setting they forgot.
			//
			// Compared against the default rather than asked of IsSet, which
			// answers true for a flag carrying a default value and would
			// therefore fail every run that has no configuration file at all.
			if path != ConfigFilePath() {
				return fmt.Errorf("configuration file %q does not exist", path)
			}
			return nil
		}
		return fmt.Errorf("unable to read configuration file %q: %w", path, err)
	}

	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("unable to parse configuration file %q: %w", path, err)
	}

	return nil
}

func CheckFlags(context context.Context, command *cli.Command) (context.Context, error) {
	if err := CheckConfigFile(command); err != nil {
		return context, err
	}

	if command.Bool("title-from-h1") && command.Bool("title-from-filename") {
		return context, errors.New("flags --title-from-h1 and --title-from-filename are mutually exclusive. Please specify only one")
	}

	contentAppearance := strings.TrimSpace(command.String("content-appearance"))
	if contentAppearance != "" {
		switch contentAppearance {
		case "full-width", "fixed", "default":
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --content-appearance: %q (expected: full-width, fixed, or default)",
				contentAppearance,
			)
		}
	}

	for _, feature := range command.StringSlice("features") {
		// Compared as written rather than trimmed, because that is how it is
		// read where it takes effect: --features "math, mermaid" hands the
		// second one over with its leading space still on, and it would go
		// unrecognised there just as surely.
		if !slices.Contains(KnownFeatures, feature) {
			return context, fmt.Errorf(
				"invalid value for --features: %q (expected any of: %s)",
				feature, strings.Join(KnownFeatures, ", "),
			)
		}
	}

	imageAlign := strings.TrimSpace(command.String("image-align"))
	if imageAlign != "" {
		switch strings.ToLower(imageAlign) {
		case "left", "center", "right":
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --image-align: %q (expected: left, center, or right)",
				imageAlign,
			)
		}
	}

	// Checked as written, without trimming, because what is checked has to be
	// what is used: the value goes into the configuration whole, and the
	// renderer compares it literally, so a trimmed " svg " would pass here and
	// publish a PNG -- taking a bundle asked for alongside it down with it.
	//
	// Asked of IsSet as well as of the value, because a flag carrying a default
	// is never empty unless somebody emptied it: mermaid-output = "" in the
	// configuration file, or MARK_MERMAID_OUTPUT exported with nothing in it,
	// would otherwise skip the check below and quietly publish a PNG. A command
	// that does not carry the flag at all has neither, and is left alone.
	// Checked as written, without trimming, because what is checked has to be
	// what is used: the value goes into the configuration whole and the
	// renderer compares it literally. Asked of IsSet as well as of the value,
	// because a flag carrying a default is never empty unless somebody emptied
	// it, and an emptied one would otherwise fall through and publish a PNG
	// without a word.
	d2Output := command.String("d2-output")
	if d2Output != "" || command.IsSet("d2-output") {
		switch d2Output {
		case "png", "svg":
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --d2-output: %q (expected: png or svg)",
				d2Output,
			)
		}
	}

	// A scale of zero renders nothing and a negative one renders nonsense, and
	// both reach Chrome as a screenshot request that fails somewhere further in
	// with nothing to point at the setting. Asked of IsSet as well, so that a
	// command built without the flag -- which is what the tests around this one
	// do -- is left alone.
	if command.IsSet("d2-scale") && command.Float("d2-scale") <= 0 {
		return context, fmt.Errorf(
			"invalid value for --d2-scale: %v (expected: greater than 0)",
			command.Float("d2-scale"),
		)
	}

	mermaidOutput := command.String("mermaid-output")
	if mermaidOutput != "" || command.IsSet("mermaid-output") {
		switch mermaidOutput {
		case "png", "svg":
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --mermaid-output: %q (expected: png or svg)",
				mermaidOutput,
			)
		}
	}

	// A scale that does nothing and a bundle that goes nowhere are both worth
	// saying out loud rather than dropping: each was asked for on purpose, and
	// each silently does not happen.
	//
	// The scale is asked of IsSet, since 1.0 is a scale like any other and
	// there is nothing else to tell a default from a value somebody chose. The
	// bundle is asked of its value instead, because false is exactly what a
	// bundle nobody asked for looks like -- so mermaid-bundle = false, or
	// --mermaid-bundle=false, contradicts a PNG in no way at all.
	if mermaidOutput == "svg" && command.IsSet("mermaid-scale") {
		return context, errors.New(
			"--mermaid-scale does not apply to --mermaid-output=svg: an SVG is the same drawing at every size",
		)
	}

	if mermaidOutput == "png" && command.Bool("mermaid-bundle") {
		return context, errors.New(
			"--mermaid-bundle needs --mermaid-output=svg: there is nowhere in a PNG to keep the diagram's source",
		)
	}

	mathFormat := strings.TrimSpace(command.String("math-format"))
	if mathFormat != "" {
		switch mathFormat {
		case "svg", "png":
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --math-format: %q (expected: svg or png)",
				mathFormat,
			)
		}
	}

	return context, nil
}
