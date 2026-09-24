package util

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kovetskiy/mark/v16/mermaid"

	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrctoml "github.com/urfave/cli-altsrc/v3/toml"
	"github.com/urfave/cli/v3"
)

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

// globalFlags are the flags every command shares: where the configuration file
// is, how to reach and authenticate against Confluence, and how to log. They
// sit on the root command and are persistent, so "mark --base-url B publish"
// and "mark publish --base-url B" mean the same thing.
//
// None of them reads the configuration file through its own Sources, only the
// environment. A persistent flag is resolved while the root command is
// parsed, and "mark publish --config X" names the file only once the
// subcommand is parsed, after that -- so a TOML source here would look in the
// default file instead of X. ApplyConfigFile fills them in from the file once
// every flag has been parsed and the file is known.
func globalFlags(config *string) []cli.Flag {
	return []cli.Flag{
		// First, so that anything reading the file through the pointer it
		// fills in finds it filled.
		&cli.StringFlag{
			Name:        "config",
			Aliases:     []string{"c"},
			Value:       ConfigFilePath(),
			Usage:       "use the specified configuration file.",
			TakesFile:   true,
			Sources:     cli.NewValueSourceChain(cli.EnvVar("MARK_CONFIG")),
			Destination: config,
		},
		&cli.StringFlag{
			Name:    "username",
			Aliases: []string{"u"},
			Value:   "",
			Usage:   "use specified username to authenticate with Confluence.",
			Sources: cli.EnvVars("MARK_USERNAME"),
		},
		&cli.StringFlag{
			Name:    "password",
			Aliases: []string{"p"},
			Value:   "",
			Usage:   "use specified token to authenticate with Confluence. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication.",
			Sources: cli.EnvVars("MARK_PASSWORD"),
		},
		&cli.StringFlag{
			Name:    "password-command",
			Value:   "",
			Usage:   "run the specified command and use the first line of its stdout as the token to authenticate with Confluence. Runs without a shell. Mutually exclusive with password.",
			Sources: cli.EnvVars("MARK_PASSWORD_COMMAND"),
		},
		&cli.StringFlag{
			Name:    "base-url",
			Aliases: []string{"b"},
			Value:   "",
			Usage:   "base URL for Confluence. Alternative to the base-url config file key.",
			Sources: cli.EnvVars("MARK_BASE_URL"),
		},
		&cli.BoolFlag{
			Name:    "insecure-skip-tls-verify",
			Value:   false,
			Usage:   "skip TLS certificate verification (useful for self-signed certificates)",
			Sources: cli.EnvVars("MARK_INSECURE_SKIP_TLS_VERIFY"),
		},
		&cli.StringFlag{
			Name:    "log-level",
			Value:   "info",
			Usage:   "set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL.",
			Sources: cli.EnvVars("MARK_LOG_LEVEL"),
		},
		&cli.StringFlag{
			Name:    "color",
			Value:   "auto",
			Usage:   "display logs in color. Possible values: auto, never.",
			Sources: cli.EnvVars("MARK_COLOR"),
		},
	}
}

// publishFlags are the flags of "mark publish". Each reads the configuration
// file through the pointer the "config" flag fills in; that flag belongs to the
// root command and is always resolved first, whichever side of "publish" it is
// written on, so every one of these sees the file it names.
func publishFlags(config *string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:      "files",
			Aliases:   []string{"f"},
			Value:     "",
			Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
			TakesFile: true,
			Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_FILES"), altsrctoml.TOML("files", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "continue-on-error",
			Value:   false,
			Usage:   "don't exit if an error occurs while processing a file, continue processing remaining files.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CONTINUE_ON_ERROR"), altsrctoml.TOML("continue-on-error", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "compile-only",
			Value:   false,
			Usage:   "show resulting HTML and don't update Confluence page content.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_COMPILE_ONLY"), altsrctoml.TOML("compile-only", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "dry-run",
			Value:   false,
			Usage:   "resolve page and ancestry, show resulting HTML and exit.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_DRY_RUN"), altsrctoml.TOML("dry-run", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "edit-lock",
			Value:   false,
			Aliases: []string{"k"},
			Usage:   "lock page editing to current user only to prevent accidental manual edits over Confluence Web UI.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_EDIT_LOCK"), altsrctoml.TOML("edit-lock", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "drop-h1",
			Value:   false,
			Usage:   "don't include the first H1 heading in Confluence output.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_DROP_H1"), altsrctoml.TOML("drop-h1", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "strip-linebreaks",
			Value:   false,
			Aliases: []string{"L"},
			Usage:   "remove linebreaks inside of tags, to accommodate non-standard Confluence behavior",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_STRIP_LINEBREAKS"), altsrctoml.TOML("strip-linebreaks", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "title-from-h1",
			Value:   false,
			Usage:   "extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata. Mutually exclusive with --title-from-filename.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_FROM_H1"), altsrctoml.TOML("title-from-h1", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "title-from-filename",
			Value:   false,
			Usage:   "use the filename (without extension) as the Confluence page title if no explicit page title is set in the metadata. Mutually exclusive with --title-from-h1.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_FROM_FILENAME"), altsrctoml.TOML("title-from-filename", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "parents-from-path",
			Value:   false,
			Usage:   "place each page under a page named after every directory between the file pattern's root and the file itself. An index.md or README.md is the page for its own directory rather than a page inside it. A document that names its own Parent is left alone.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS_FROM_PATH"), altsrctoml.TOML("parents-from-path", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "parents-from-path-root",
			Value:   "",
			Usage:   "the directory --parents-from-path measures from. Taken from the --files pattern when not given, which is everything before its first wildcard.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS_FROM_PATH_ROOT"), altsrctoml.TOML("parents-from-path-root", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "title-append-generated-hash",
			Value:   false,
			Usage:   "appends a short hash generated from the path of the page (space, parents, and title) to the title",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_APPEND_GENERATED_HASH"), altsrctoml.TOML("title-append-generated-hash", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "minor-edit",
			Value:   false,
			Usage:   "don't send notifications while updating Confluence page.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MINOR_EDIT"), altsrctoml.TOML("minor-edit", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "version-message",
			Value:   "",
			Usage:   "add a message to the page version, to explain the edit (default: \"\")",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_VERSION_MESSAGE"), altsrctoml.TOML("version-message", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "target-url",
			Aliases: []string{"l"},
			Value:   "",
			Usage:   "edit the Confluence page at this URL. Without it, each file must name its page with Space and Title metadata headers.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TARGET_URL"), altsrctoml.TOML("target-url", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "ci",
			Value:   false,
			Usage:   "run on CI mode. It won't fail if files are not found.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CI"), altsrctoml.TOML("ci", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "space",
			Value:   "",
			Usage:   "use specified space key. If the space key is not specified, it must be set in the page metadata.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_SPACE"), altsrctoml.TOML("space", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "parents",
			Value:   "",
			Usage:   "A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be prepended to the ones defined in the document itself.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS"), altsrctoml.TOML("parents", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "parents-delimiter",
			Value:   "/",
			Usage:   "The delimiter used for the parents list",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PARENTS_DELIMITER"), altsrctoml.TOML("parents-delimiter", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:  "content-appearance",
			Value: "",
			Usage: "default content appearance for pages without a Content-Appearance header. Possible values: full-width, fixed, default.",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("MARK_CONTENT_APPEARANCE"),
				altsrctoml.TOML("content-appearance", altsrc.NewStringPtrSourcer(config)),
			),
		},
		&cli.FloatFlag{
			Name:    "mermaid-scale",
			Value:   1.0,
			Usage:   "defines the scaling factor for mermaid renderings: the pixels of a png, and the size the page displays an svg at.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_SCALE"), altsrctoml.TOML("mermaid-scale", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:  "mermaid-engine",
			Value: "chrome",
			Usage: "what mermaid diagrams are drawn by: chrome (the default, a headless browser running mermaid.js) or merman (experimental, a native reimplementation that needs no browser and must be installed separately).",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_ENGINE"),
				altsrctoml.TOML("mermaid-engine", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "mermaid-output",
			Value:   "png",
			Usage:   "image a mermaid diagram is published as: png (rasterised, and scaled by --mermaid-scale) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment).",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_OUTPUT"), altsrctoml.TOML("mermaid-output", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "mermaid-bundle",
			Value:   false,
			Usage:   "keep the diagram's own source inside the SVG published for it, in its <desc> element, so the drawing can be edited again from the attachment. Needs --mermaid-output=svg.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_BUNDLE"), altsrctoml.TOML("mermaid-bundle", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "math-format",
			Value:   "png",
			Usage:   "image a formula is published as with --features=math: png (rasterised through the same headless Chrome mermaid uses) or svg (vector and sharp at any zoom, where the instance displays an SVG attachment).",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MATH_FORMAT"), altsrctoml.TOML("math-format", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.FloatFlag{
			Name:    "math-scale",
			Value:   2.0,
			Usage:   "defines the scaling factor for PNG formula renderings; ignored when math-format is svg.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MATH_SCALE"), altsrctoml.TOML("math-scale", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:      "include-path",
			Value:     "",
			Usage:     "Path for shared includes, used as a fallback if the include doesn't exist in the current directory.",
			TakesFile: true,
			Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_INCLUDE_PATH"), altsrctoml.TOML("include-path", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "changes-only",
			Value:   false,
			Usage:   "Avoids re-uploading pages that haven't changed since the last run.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHANGES_ONLY"), altsrctoml.TOML("changes-only", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:  "output-format",
			Value: "url",
			Usage: "how to report what the run did: \"url\" prints the address of each published page (the default), \"json\" prints one object describing the whole run, \"github\" prints GitHub Actions workflow commands so that failures appear against the file that caused them.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_OUTPUT_FORMAT"),
				altsrctoml.TOML("output-format", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:  "on-orphan",
			Value: "report",
			Usage: "what to do about a page whose source file is gone: \"report\" says so and does nothing (the default), \"archive\" archives the page (Confluence Cloud only), \"delete\" moves it to the trash. Requires --track-pages.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_ON_ORPHAN"),
				altsrctoml.TOML("on-orphan", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:  "orphan-under",
			Value: "",
			Usage: "limit --on-orphan, and the reporting it does, to pages below this page or folder, given by title or id. Without it, every tracked page the --files pattern would have published is in scope.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_ORPHAN_UNDER"),
				altsrctoml.TOML("orphan-under", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringSliceFlag{
			Name:  "check-links",
			Usage: "fail on links that do not resolve. Repeat or comma-separate any of: \"internal\" (relative links to other files in the repository), \"confluence\" (ac: links naming a page by title), \"external\" (requests each URL to see whether it answers), or \"all\".",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHECK_LINKS"),
				altsrctoml.TOML("check-links", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:      "global-properties",
			Value:     "",
			Usage:     "path to a YAML or JSON file of Confluence content properties to set on every page. A Property header or properties front matter in a document wins over the file for that page.",
			TakesFile: true,
			Sources:   cli.NewValueSourceChain(cli.EnvVar("MARK_GLOBAL_PROPERTIES"), altsrctoml.TOML("global-properties", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "append-labels",
			Value:   false,
			Usage:   "add the labels a document asks for without removing any others, so that labels applied in Confluence survive a publish. Without it, a page ends up with exactly the labels its Label headers name.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_APPEND_LABELS"), altsrctoml.TOML("append-labels", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "check-links-warn-only",
			Value:   false,
			Usage:   "report links that do not resolve without failing the run. Only meaningful together with --check-links.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_CHECK_LINKS_WARN_ONLY"), altsrctoml.TOML("check-links-warn-only", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "no-overwrite",
			Value:   false,
			Usage:   "Leave alone any page that has been edited in Confluence since mark last published it, instead of overwriting the edit. Requires --track-pages, which is where the last published version is remembered.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_NO_OVERWRITE"), altsrctoml.TOML("no-overwrite", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "track-pages",
			Value:   false,
			Usage:   "Remember which page each file publishes to, so renaming a file or changing its title updates the existing page instead of creating a second one. Stores the mapping in Confluence (a space property on Cloud, a homepage content property on Server/Data Center, or the page --manifest-page names); nothing is written to the repository.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TRACK_PAGES"), altsrctoml.TOML("track-pages", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "manifest-page",
			Value:   "",
			Usage:   "keep the --track-pages mapping as content properties of this page, given by title or id, instead of as space properties. Needs only the right to edit that page where a space property needs space administration, and works with a scoped API token. Requires --track-pages.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MANIFEST_PAGE"), altsrctoml.TOML("manifest-page", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "manifest-prefix",
			Value:   "mark.manifest",
			Usage:   "name the --track-pages mapping's properties under this prefix, so that two projects publishing into one space keep manifests of their own instead of sharing, and reporting each other's files as gone. Letters, digits, '_', '-' and dots. Requires --track-pages.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MANIFEST_PREFIX"), altsrctoml.TOML("manifest-prefix", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:    "preserve-comments",
			Value:   false,
			Usage:   "Fetch and preserve inline comments on existing Confluence pages.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_PRESERVE_COMMENTS"), altsrctoml.TOML("preserve-comments", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "d2-output",
			Value:   "png",
			Usage:   "image a d2 diagram is published as: png (rasterised) or svg (vector and sharp at any zoom, with whatever the diagram references inlined into it, where the instance displays an SVG attachment).",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_OUTPUT"), altsrctoml.TOML("d2-output", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:  "d2-bundle-remote",
			Value: false,
			Usage: "let a d2 diagram published as svg have mark fetch the URLs it names, and publish what comes back inside the drawing. Off by default, which refuses a diagram that names a URL: the request is made by the document rather than by you, to any address it likes.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_BUNDLE_REMOTE"),
				altsrctoml.TOML("d2-bundle-remote", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.FloatFlag{
			Name:    "d2-scale",
			Value:   1.0,
			Usage:   "defines the scaling factor for d2 renderings: the pixels of a png, and the size the page displays an svg at.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_SCALE"), altsrctoml.TOML("d2-scale", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringSliceFlag{
			Name:  "features",
			Value: []string{"mermaid", "mention"},
			Usage: "Enables optional features, replacing the defaults (" +
				strings.Join(defaultFeatures, ", ") + ") rather than adding to them. " +
				"Current features: " + strings.Join(KnownFeatures, ", "),
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_FEATURES"), altsrctoml.TOML("features", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.BoolFlag{
			Name:  "attach-referenced",
			Value: false,
			Usage: "upload a local file that a link points at, and link to the attachment. Without it the link is published as the path the document wrote, which means nothing once the page is on Confluence. Images are attached either way.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_ATTACH_REFERENCED"),
				altsrctoml.TOML("attach-referenced", altsrc.NewStringPtrSourcer(config))),
		},
		&cli.StringFlag{
			Name:    "image-align",
			Value:   "",
			Usage:   "set image alignment (left, center, right). Can be overridden per-file via the Image-Align header.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_IMAGE_ALIGN"), altsrctoml.TOML("image-align", altsrc.NewStringPtrSourcer(config))),
		},
	}
}

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

// ApplyConfigFile gives each of the named flags that neither the command line
// nor the environment has set the value the configuration file holds for it,
// under a key of the same name.
//
// It is how the global flags read the file (see globalFlags), and it has to run
// once every command in the chain has been parsed -- from a Before, which
// urfave/cli runs only after that -- so that it reads the file the invocation
// actually named. The value goes through the same TOML source and the same
// rules as a flag's own Sources would use.
func ApplyConfigFile(command *cli.Command, names []string) error {
	path := command.String("config")
	if path == "" {
		return nil
	}

	for _, name := range names {
		if command.IsSet(name) {
			continue
		}

		value, found := altsrctoml.TOML(name, altsrc.StringSourcer(path)).Lookup()
		if !found {
			continue
		}

		// What a flag's own PostParse does with a value from a source: an
		// empty value is still a value for a string, is false for a bool, and
		// is nothing at all for anything else.
		if value == "" {
			switch command.Value(name).(type) {
			case string:
			case bool:
				value = "false"
			default:
				continue
			}
		}

		if err := command.Set(name, value); err != nil {
			return fmt.Errorf(
				"could not parse %q from configuration file %q for flag %s: %w",
				value, path, name, err,
			)
		}
	}

	return nil
}

// CheckFlags validates combinations and values of the publish flags. The
// configuration file they may come from has been checked by then, by the root
// command's Before.
func CheckFlags(context context.Context, command *cli.Command) (context.Context, error) {
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

	// Both output formats are checked the same way, and for the same two
	// reasons.
	//
	// As written, without trimming, because what is checked has to be what is
	// used: the value goes into the configuration whole and the renderer
	// compares it literally, so a trimmed " svg " would pass here and publish a
	// PNG -- taking anything asked for alongside it down with it.
	//
	// And asked of IsSet as well as of the value, because a flag carrying a
	// default is never empty unless somebody emptied it: d2-output = "" in the
	// configuration file, or MARK_D2_OUTPUT exported with nothing in it, would
	// otherwise fall through and publish a PNG without a word. A command that
	// does not carry the flag at all has neither, and is left alone.
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
	//
	// The same for mermaid and math. Zero is refused on the command line even
	// though a library Config reads it as the default: a flag nobody set never
	// reaches here, so a zero is one somebody typed, and it is no scale.
	for _, name := range []string{"d2-scale", "mermaid-scale", "math-scale"} {
		if scale := command.Float(name); command.IsSet(name) &&
			(!(scale > 0) || math.IsInf(scale, 0)) {
			return context, fmt.Errorf(
				"invalid value for --%s: %v (expected: a finite number greater than 0)",
				name, scale,
			)
		}
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

	// A bundle that goes nowhere is worth saying out loud rather than dropping:
	// it was asked for on purpose, and it silently does not happen. Asked of its
	// value rather than of IsSet, because false is exactly what a bundle nobody
	// asked for looks like -- so mermaid-bundle = false contradicts a PNG in no
	// way at all.
	//
	// The scale is not checked against the format any more: it applies to both,
	// multiplying the pixels of a PNG and the size the page shows an SVG at.
	if mermaidOutput == "png" && command.Bool("mermaid-bundle") {
		return context, errors.New(
			"--mermaid-bundle needs --mermaid-output=svg: there is nowhere in a PNG to keep the diagram's source",
		)
	}

	// Checked as written, and asked of IsSet as well, so that a value somebody
	// emptied is not read as one nobody set.
	mermaidEngine := command.String("mermaid-engine")
	if mermaidEngine != "" || command.IsSet("mermaid-engine") {
		switch mermaidEngine {
		case mermaid.EngineChrome, mermaid.EngineMerman:
			// ok
		default:
			return context, fmt.Errorf(
				"invalid value for --mermaid-engine: %q (expected: %s or %s)",
				mermaidEngine, mermaid.EngineChrome, mermaid.EngineMerman,
			)
		}
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
