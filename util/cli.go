package util

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	mark "github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/export"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`

	// PublishCommand is the name of the command that publishes markdown, and
	// the one a command line naming no command at all is taken to mean.
	PublishCommand = "publish"

	// ExportCommand is the name of the command that writes a page out as
	// markdown.
	ExportCommand = "export"
)

// NewCommand builds mark's command line: the global flags on the root, and a
// command for each thing mark does.
func NewCommand(version string) *cli.Command {
	// Where the configuration file is. The "config" flag fills it in, and every
	// other flag reads the file through it.
	var config string

	global := globalFlags(&config)

	var names []string
	for _, flag := range global {
		if name := flag.Names()[0]; name != "config" {
			names = append(names, name)
		}
	}

	return &cli.Command{
		Name:                  "mark",
		Usage:                 usage,
		Description:           description,
		Version:               version,
		Flags:                 global,
		EnableShellCompletion: true,
		// urfave/cli runs every Before in the chain only once the whole
		// command line has been parsed, so this sees the configuration file
		// the invocation named, whichever command it was given to.
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if err := CheckConfigFile(cmd); err != nil {
				return ctx, err
			}
			if err := ApplyConfigFile(cmd, names); err != nil {
				return ctx, err
			}
			return ctx, SetUpLogging(cmd)
		},
		Commands: []*cli.Command{
			{
				Name:  PublishCommand,
				Usage: "publish markdown files to Confluence",
				Description: "Compile each markdown file --files names into Confluence storage format and " +
					"create or update the page its metadata names.",
				Flags:  publishFlags(&config),
				Before: CheckFlags,
				Action: RunPublish,
			},
			{
				Name:  ExportCommand,
				Usage: "export a Confluence page to a markdown file",
				Description: "Write the page --page-id, --url or --space and --title names as markdown, with the " +
					"metadata headers that publish it back to the same page, and download the attachments it shows or links to.",
				Flags:  exportFlags(&config),
				Before: CheckExportFlags,
				Action: RunExport,
			},
		},
	}
}

// bareInvocation marks, in the context, a run whose command line named no
// command and was taken to mean "publish".
type bareInvocation struct{}

// Run runs mark's command line.
//
// Before there were commands, "mark <flags>" was the one thing mark did, and it
// is how mark runs in a great many CI pipelines. A command line that names no
// command therefore still publishes, exactly as "mark publish <flags>" would,
// and says that it is deprecated. "--help" and "--version" are the exception:
// on their own they are about mark as a whole.
func Run(ctx context.Context, cmd *cli.Command, args []string) error {
	args, bare := defaultToPublish(cmd, args)
	if bare {
		ctx = context.WithValue(ctx, bareInvocation{}, true)
	}

	return cmd.Run(ctx, args)
}

// defaultToPublish puts "publish" into a command line that names no command,
// and says whether it did.
//
// A command is named by the first argument that is neither a flag nor a flag's
// value. Telling a value from a command takes knowing which flags have one, so
// "mark --version-message publish -f doc.md" publishes with the message
// "publish", as it always did. The help and version flags, and the one shell
// completion asks with, leave the command line alone: they are answered by the
// root command, which lists the commands and the global flags.
func defaultToPublish(root *cli.Command, args []string) ([]string, bool) {
	if len(args) == 0 {
		return args, false
	}

	// Commands urfave/cli adds for itself, which are not in root.Commands
	// until it runs.
	commands := map[string]bool{"help": true, "h": true, "completion": true}
	takesValue := map[string]bool{}

	collect := func(flags []cli.Flag) {
		for _, flag := range flags {
			valued, ok := flag.(interface{ TakesValue() bool })
			for _, name := range flag.Names() {
				takesValue[name] = ok && valued.TakesValue()
			}
		}
	}

	collect(root.Flags)
	for _, command := range root.Commands {
		for _, name := range command.Names() {
			commands[name] = true
		}
		collect(command.Flags)
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}

		if len(arg) > 1 && arg[0] == '-' {
			name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch name {
			case "help", "h", "version", "v", "generate-shell-completion":
				return args, false
			}
			if takesValue[name] && !inline {
				i++
			}
			continue
		}

		if commands[arg] {
			return args, false
		}

		break
	}

	rewritten := make([]string, 0, len(args)+1)
	rewritten = append(rewritten, args[0], PublishCommand)
	rewritten = append(rewritten, args[1:]...)

	return rewritten, true
}

// SetUpLogging sets the log level and format the global flags ask for.
func SetUpLogging(cmd *cli.Command) error {
	if err := SetLogLevel(cmd); err != nil {
		return err
	}

	zerolog.TimeFieldFormat = "2006-01-02 15:04:05.000"

	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "2006-01-02 15:04:05.000",
		FormatLevel: func(i any) string {
			var l string
			if ll, ok := i.(string); ok {
				switch ll {
				case "trace":
					l = "TRACE"
				case "debug":
					l = "DEBUG"
				case "info":
					l = "INFO"
				case "warn":
					l = "WARNING"
				case "error":
					l = "ERROR"
				case "fatal":
					l = "FATAL"
				case "panic":
					l = "PANIC"
				default:
					l = strings.ToUpper(ll)
				}
			} else {
				l = strings.ToUpper(fmt.Sprintf("%s", i))
			}
			return l
		},
		FormatFieldName: func(i any) string {
			return ""
		},
		FormatFieldValue: func(i any) string {
			return fmt.Sprintf("%s", i)
		},
		FormatErrFieldName: func(i any) string {
			return ""
		},
		FormatErrFieldValue: func(i any) string {
			return fmt.Sprintf("%s", i)
		},
	}
	if cmd.String("color") == "never" {
		output.NoColor = true
	}
	log.Logger = zerolog.New(output).With().Timestamp().Logger()

	return nil
}

// RunPublish is the action of "mark publish".
func RunPublish(ctx context.Context, cmd *cli.Command) error {
	if bare, _ := ctx.Value(bareInvocation{}).(bool); bare {
		log.Warn().Msg(`running mark without a command is deprecated: run "mark publish" with the same flags instead`)
	}

	creds, err := GetCredentials(
		ctx,
		cmd.String("username"),
		cmd.String("password"),
		cmd.String("password-command"),
		cmd.String("target-url"),
		cmd.String("base-url"),
		cmd.Bool("compile-only"),
	)
	if err != nil {
		return err
	}

	logFlags(cmd)

	parents := splitParents(cmd.String("parents"), cmd.String("parents-delimiter"))

	config := mark.Config{
		BaseURL:               creds.BaseURL,
		Username:              creds.Username,
		Password:              creds.Password,
		PageID:                creds.PageID,
		InsecureSkipTLSVerify: cmd.Bool("insecure-skip-tls-verify"),

		Files: cmd.String("files"),

		CompileOnly:     cmd.Bool("compile-only"),
		DryRun:          cmd.Bool("dry-run"),
		ContinueOnError: cmd.Bool("continue-on-error"),
		CI:              cmd.Bool("ci"),

		Space:                    cmd.String("space"),
		Parents:                  parents,
		TitleFromH1:              cmd.Bool("title-from-h1"),
		TitleFromFilename:        cmd.Bool("title-from-filename"),
		TitleAppendGeneratedHash: cmd.Bool("title-append-generated-hash"),
		ParentsFromPath:          cmd.Bool("parents-from-path"),
		ParentsFromPathRoot:      cmd.String("parents-from-path-root"),
		ContentAppearance:        cmd.String("content-appearance"),

		MinorEdit:          cmd.Bool("minor-edit"),
		VersionMessage:     cmd.String("version-message"),
		EditLock:           cmd.Bool("edit-lock"),
		ChangesOnly:        cmd.Bool("changes-only"),
		TrackPages:         cmd.Bool("track-pages"),
		ManifestPage:       cmd.String("manifest-page"),
		ManifestPrefix:     cmd.String("manifest-prefix"),
		NoOverwrite:        cmd.Bool("no-overwrite"),
		CheckLinks:         cmd.StringSlice("check-links"),
		CheckLinksWarnOnly: cmd.Bool("check-links-warn-only"),
		AppendLabels:       cmd.Bool("append-labels"),
		GlobalProperties:   cmd.String("global-properties"),
		OnOrphan:           cmd.String("on-orphan"),
		OutputFormat:       cmd.String("output-format"),
		OrphanUnder:        cmd.String("orphan-under"),
		PreserveComments:   cmd.Bool("preserve-comments"),

		DropH1:           cmd.Bool("drop-h1"),
		StripLinebreaks:  cmd.Bool("strip-linebreaks"),
		MermaidEngine:    cmd.String("mermaid-engine"),
		MermaidScale:     cmd.Float("mermaid-scale"),
		MermaidOutput:    cmd.String("mermaid-output"),
		MermaidBundle:    cmd.Bool("mermaid-bundle"),
		D2Output:         cmd.String("d2-output"),
		D2BundleRemote:   cmd.Bool("d2-bundle-remote"),
		D2Scale:          cmd.Float("d2-scale"),
		MathFormat:       cmd.String("math-format"),
		MathScale:        cmd.Float("math-scale"),
		Features:         cmd.StringSlice("features"),
		ImageAlign:       cmd.String("image-align"),
		AttachReferenced: cmd.Bool("attach-referenced"),
		IncludePath:      cmd.String("include-path"),

		Output: os.Stdout,
	}

	// RunContext shuts the shared browser down on the way out, however the run
	// ends -- including when ctx is cancelled by a signal.
	return mark.RunContext(ctx, config)
}

// RunExport is the action of "mark export".
func RunExport(ctx context.Context, cmd *cli.Command) error {
	pageID := cmd.String("page-id")
	space := cmd.String("space")
	title := cmd.String("title")
	baseURL := cmd.String("base-url")

	if address := cmd.String("url"); address != "" {
		ref, err := export.ParsePageURL(address)
		if err != nil {
			return err
		}

		// A URL names its own page, in its own space, on its own instance --
		// unless --base-url says where the API is, which is not always where
		// the browser goes.
		pageID, title = ref.PageID, ref.Title
		if ref.Space != "" {
			space = ref.Space
		}
		if baseURL == "" {
			baseURL = ref.BaseURL
		}
	}

	if baseURL == "" {
		return errors.New("confluence base URL should be specified using --base-url, or come with --url")
	}

	creds, err := GetCredentials(
		ctx,
		cmd.String("username"),
		cmd.String("password"),
		cmd.String("password-command"),
		"",
		baseURL,
		false,
	)
	if err != nil {
		return err
	}

	logFlags(cmd)

	_, err = mark.Export(ctx, mark.ExportConfig{
		BaseURL:               creds.BaseURL,
		Username:              creds.Username,
		Password:              creds.Password,
		InsecureSkipTLSVerify: cmd.Bool("insecure-skip-tls-verify"),

		PageID: pageID,
		Space:  space,
		Title:  title,

		Output:         cmd.String("output"),
		AttachmentsDir: cmd.String("attachments-dir"),
		Overwrite:      cmd.Bool("overwrite"),
		NoAttachments:  cmd.Bool("no-attachments"),

		Stdout: os.Stdout,
	})

	return err
}

// logFlags logs every flag a command was run with, at debug level, with the
// credentials masked.
func logFlags(cmd *cli.Command) {
	log.Debug().Msg("config:")
	for _, f := range append(slices.Clone(cmd.Root().Flags), cmd.Flags...) {
		flag := f.Names()
		// A command can name a token inline, so it leaks the same way the password would.
		if flag[0] == "password" || flag[0] == "password-command" {
			log.Debug().Msgf("%20s: %v", flag[0], "******")
		} else {
			log.Debug().Msgf("%20s: %v", flag[0], cmd.Value(flag[0]))
		}
	}
}

// ConfigFilePath is the default for --config, or "" when there is none.
//
// It is evaluated while the flags are declared, at package initialisation, so
// failing here fails every invocation -- --version and --help included -- and
// before any flag could say where the file is. That is exactly the environment
// in which the user config directory is unknown: a minimal container or a
// systemd unit with neither $XDG_CONFIG_HOME nor $HOME. Having no default file
// is the honest answer there; --config and MARK_CONFIG still name one.
func ConfigFilePath() string {
	fp, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(fp, "mark.toml")
}

func SetLogLevel(cmd *cli.Command) error {
	logLevel := cmd.String("log-level")
	switch strings.ToUpper(logLevel) {
	case "TRACE":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "DEBUG":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "INFO":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "WARNING":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "ERROR":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "FATAL":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	default:
		return fmt.Errorf("unknown log level: %s", logLevel)
	}

	return nil
}

// splitParents reads the --parents value, dropping empty fields.
//
// A delimiter is a separator, not a parent. "/A/B" split to ["", "A", "B"] and
// the guard downstream checks only element zero, so a leading delimiter
// silently discarded the whole list and published the page under whatever
// ancestry the document itself named. "A//B" and "A/B/" went the other way and
// put a parent with no title into the chain, which ancestry resolution then
// looks up and, finding nothing, creates.
func splitParents(value, delimiter string) []string {
	var parents []string

	for _, parent := range strings.Split(value, delimiter) {
		if parent != "" {
			parents = append(parents, parent)
		}
	}

	return parents
}
