package util

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrctoml "github.com/urfave/cli-altsrc/v3/toml"
	"github.com/urfave/cli/v3"
)

var filename string

var Flags = []cli.Flag{
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
	&cli.StringFlag{
		Name:        "config",
		Aliases:     []string{"c"},
		Value:       ConfigFilePath(),
		Usage:       "use the specified configuration file.",
		TakesFile:   true,
		Sources:     cli.NewValueSourceChain(cli.EnvVar("MARK_CONFIG")),
		Destination: &filename,
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
		Usage:   "defines the scaling factor for mermaid renderings.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_SCALE"), altsrctoml.TOML("mermaid-scale", altsrc.NewStringPtrSourcer(&filename))),
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
	&cli.FloatFlag{
		Name:    "d2-scale",
		Value:   1.0,
		Usage:   "defines the scaling factor for d2 renderings.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_SCALE"), altsrctoml.TOML("d2-scale", altsrc.NewStringPtrSourcer(&filename))),
	},

	&cli.StringSliceFlag{
		Name:    "features",
		Value:   []string{"mermaid", "mention"},
		Usage:   "Enables optional features. Current features: d2, date, details, emoji, footnotes, frontmatter, html-img-tag, inline-link-card, math, mention, mermaid, mkdocsadmonitions, plantuml",
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

	return context, nil
}
