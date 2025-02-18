package util

import (
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
		Usage:   "extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_TITLE_FROM_H1"), altsrctoml.TOML("title-from-h1", altsrc.NewStringPtrSourcer(&filename))),
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
		Name:    "mermaid-provider",
		Value:   "cloudscript",
		Usage:   "defines the mermaid provider to use. Supported options are: cloudscript, mermaid-go.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_MERMAID_PROVIDER"), altsrctoml.TOML("mermaid-provider", altsrc.NewStringPtrSourcer(&filename))),
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
	&cli.FloatFlag{
		Name:    "d2-scale",
		Value:   1.0,
		Usage:   "defines the scaling factor for d2 renderings.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_D2_SCALE"), altsrctoml.TOML("d2-scale", altsrc.NewStringPtrSourcer(&filename))),
	},

	&cli.StringSliceFlag{
		Name:    "features",
		Value:   []string{"mermaid"},
		Usage:   "Enables optional features. Current features: d2, mermaid, mkdocsadmonitions",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_FEATURES"), altsrctoml.TOML("features", altsrc.NewStringPtrSourcer(&filename))),
	},
}
