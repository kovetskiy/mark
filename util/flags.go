package util

import (
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var Flags = []cli.Flag{
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:      "files",
		Aliases:   []string{"f"},
		Value:     "",
		Usage:     "use specified markdown file(s) for converting to html. Supports file globbing patterns (needs to be quoted).",
		TakesFile: true,
		EnvVars:   []string{"MARK_FILES"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "continue-on-error",
		Value:   false,
		Usage:   "don't exit if an error occurs while processing a file, continue processing remaining files.",
		EnvVars: []string{"MARK_CONTINUE_ON_ERROR"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "compile-only",
		Value:   false,
		Usage:   "show resulting HTML and don't update Confluence page content.",
		EnvVars: []string{"MARK_COMPILE_ONLY"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "dry-run",
		Value:   false,
		Usage:   "resolve page and ancestry, show resulting HTML and exit.",
		EnvVars: []string{"MARK_DRY_RUN"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "edit-lock",
		Value:   false,
		Aliases: []string{"k"},
		Usage:   "lock page editing to current user only to prevent accidental manual edits over Confluence Web UI.",
		EnvVars: []string{"MARK_EDIT_LOCK"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "drop-h1",
		Value:   false,
		Aliases: []string{"h1_drop"},
		Usage:   "don't include the first H1 heading in Confluence output.",
		EnvVars: []string{"MARK_H1_DROP"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "strip-linebreaks",
		Value:   false,
		Aliases: []string{"L"},
		Usage:   "remove linebreaks inside of tags, to accomodate non-standard Confluence behavior",
		EnvVars: []string{"MARK_STRIP_LINEBREAKS"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "title-from-h1",
		Value:   false,
		Aliases: []string{"h1_title"},
		Usage:   "extract page title from a leading H1 heading. If no H1 heading on a page exists, then title must be set in the page metadata.",
		EnvVars: []string{"MARK_H1_TITLE"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "title-append-generated-hash",
		Value:   false,
		Usage:   "appends a short hash generated from the path of the page (space, parents, and title) to the title",
		EnvVars: []string{"MARK_TITLE_APPEND_GENERATED_HASH"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "minor-edit",
		Value:   false,
		Usage:   "don't send notifications while updating Confluence page.",
		EnvVars: []string{"MARK_MINOR_EDIT"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "version-message",
		Value:   "",
		Usage:   "add a message to the page version, to explain the edit (default: \"\")",
		EnvVars: []string{"MARK_VERSION_MESSAGE"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "color",
		Value:   "auto",
		Usage:   "display logs in color. Possible values: auto, never.",
		EnvVars: []string{"MARK_COLOR"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "log-level",
		Value:   "info",
		Usage:   "set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL.",
		EnvVars: []string{"MARK_LOG_LEVEL"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "username",
		Aliases: []string{"u"},
		Value:   "",
		Usage:   "use specified username for updating Confluence page.",
		EnvVars: []string{"MARK_USERNAME"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "password",
		Aliases: []string{"p"},
		Value:   "",
		Usage:   "use specified token for updating Confluence page. Specify - as password to read password from stdin, or your Personal access token. Username is not mandatory if personal access token is provided. For more info please see: https://developer.atlassian.com/server/confluence/confluence-server-rest-api/#authentication.",
		EnvVars: []string{"MARK_PASSWORD"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "target-url",
		Aliases: []string{"l"},
		Value:   "",
		Usage:   "edit specified Confluence page. If -l is not specified, file should contain metadata (see above).",
		EnvVars: []string{"MARK_TARGET_URL"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "base-url",
		Aliases: []string{"b", "base_url"},
		Value:   "",
		Usage:   "base URL for Confluence. Alternative option for base_url config field.",
		EnvVars: []string{"MARK_BASE_URL"},
	}),
	&cli.StringFlag{
		Name:      "config",
		Aliases:   []string{"c"},
		Value:     ConfigFilePath(),
		Usage:     "use the specified configuration file.",
		TakesFile: true,
		EnvVars:   []string{"MARK_CONFIG"},
	},
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "ci",
		Value:   false,
		Usage:   "run on CI mode. It won't fail if files are not found.",
		EnvVars: []string{"MARK_CI"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "space",
		Value:   "",
		Usage:   "use specified space key. If the space key is not specified, it must be set in the page metadata.",
		EnvVars: []string{"MARK_SPACE"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "parents",
		Value:   "",
		Usage:   "A list containing the parents of the document separated by parents-delimiter (default: '/'). These will be prepended to the ones defined in the document itself.",
		EnvVars: []string{"MARK_PARENTS"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "parents-delimiter",
		Value:   "/",
		Usage:   "The delimiter used for the parents list",
		EnvVars: []string{"MARK_PARENTS_DELIMITER"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:    "mermaid-provider",
		Value:   "cloudscript",
		Usage:   "defines the mermaid provider to use. Supported options are: cloudscript, mermaid-go.",
		EnvVars: []string{"MARK_MERMAID_PROVIDER"},
	}),
	altsrc.NewFloat64Flag(&cli.Float64Flag{
		Name:    "mermaid-scale",
		Value:   1.0,
		Usage:   "defines the scaling factor for mermaid renderings.",
		EnvVars: []string{"MARK_MERMAID_SCALE"},
	}),
	altsrc.NewStringFlag(&cli.StringFlag{
		Name:      "include-path",
		Value:     "",
		Usage:     "Path for shared includes, used as a fallback if the include doesn't exist in the current directory.",
		TakesFile: true,
		EnvVars:   []string{"MARK_INCLUDE_PATH"},
	}),
	altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:    "changes-only",
		Value:   false,
		Usage:   "Avoids re-uploading pages that haven't changed since the last run.",
		EnvVars: []string{"MARK_CHANGES_ONLY"},
	}),
}
