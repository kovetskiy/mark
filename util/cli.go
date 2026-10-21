package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mark "github.com/kovetskiy/mark/v16"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`

	// PublishCommand is the name of the command that publishes markdown.
	PublishCommand = "publish"
)

// Run runs mark's command line, args as os.Args holds it, for a binary of the
// given version.
func Run(ctx context.Context, version string, args []string) error {
	return newCommand(version).Run(ctx, args)
}

func newCommand(version string) *cli.Command {
	return buildCommand(version, RunPublish)
}

// commandLine is what the actions of one run of mark's command line share.
type commandLine struct {
	config *configFile
	// bare are publish's flags on the root; see bareFlags.
	bare []cli.Flag
	// publish does the publishing; tests swap it out.
	publish cli.ActionFunc
}

// buildCommand is mark's command line: the global flags on the root, and a
// command for each thing mark does. The root's own action is the deprecated
// "mark <publish flags>".
func buildCommand(version string, publish cli.ActionFunc) *cli.Command {
	config := newConfigFile(new(string))
	line := &commandLine{config: config, bare: bareFlags(config), publish: publish}

	publishCommand := &cli.Command{
		Name:  PublishCommand,
		Usage: "publish markdown files to Confluence",
		Description: "Compile each markdown file --files names into Confluence storage format and " +
			"create or update the page its metadata names.",
		Flags:  publishFlags(config),
		Action: line.runPublish,
	}

	root := &cli.Command{
		Name:                  "mark",
		Usage:                 usage,
		Description:           description,
		Version:               version,
		Flags:                 append(globalFlags(config), line.bare...),
		EnableShellCompletion: true,
		Action:                line.runBare,
		Commands:              []*cli.Command{publishCommand},
	}

	// "mark -f doc.md --help" is publish's help, as it would be with publish
	// named. ExtraInfo is the one hook urfave/cli calls while printing the
	// root's help, and the template prints what it returns instead.
	root.CustomRootCommandHelpTemplate = `{{with ExtraInfo}}{{index . "publish"}}{{else}}` +
		cli.RootCommandHelpTemplate + `{{end}}`
	root.ExtraInfo = func() map[string]string {
		// Once settled, the flags have read the environment and the file.
		if line.config.settled || line.bareFlagSet() == "" {
			return nil
		}
		var help strings.Builder
		writer := root.Writer
		root.Writer = &help
		defer func() { root.Writer = writer }()
		_ = cli.ShowCommandHelp(context.Background(), root, PublishCommand)
		return map[string]string{"publish": help.String()}
	}

	return root
}

// bareFlagSet is the name of a publish flag the root has, or "". Until
// settle, that is one the command line gave before any command.
func (line *commandLine) bareFlagSet() string {
	for _, flag := range line.bare {
		if flag.IsSet() {
			return flag.Names()[0]
		}
	}
	return ""
}

// prepare readies cmd's action: it reads the configuration file, resolves the
// global flags from it, and sets up logging.
func (line *commandLine) prepare(cmd *cli.Command) error {
	root := cmd.Root()
	if name := line.bareFlagSet(); name != "" && cmd != root {
		if cmd.Name == PublishCommand {
			return fmt.Errorf("--%s is a flag of %q: write it after %q", name, PublishCommand, cmd.Name)
		}
		return fmt.Errorf("--%s is a flag of %q, not of %q", name, PublishCommand, cmd.Name)
	}

	if err := line.config.check(); err != nil {
		return err
	}
	if err := line.config.settle(root); err != nil {
		return err
	}

	return SetUpLogging(cmd)
}

func (line *commandLine) runPublish(ctx context.Context, cmd *cli.Command) error {
	if err := line.prepare(cmd); err != nil {
		return err
	}
	if _, err := checkPublishFlags(ctx, cmd); err != nil {
		return err
	}

	return line.publish(ctx, cmd)
}

// runBare is "mark" with no command, which publishes as "mark publish" would.
// It is how mark ran before it had commands, in a great many CI pipelines.
func (line *commandLine) runBare(ctx context.Context, root *cli.Command) error {
	written := line.bareFlagSet() != ""

	if err := line.prepare(root); err != nil {
		return err
	}

	files := root.IsSet("files")
	if !files && !written && root.NArg() == 0 {
		// Nothing says what to publish: "mark" on its own. This still fails,
		// as it did, so a pipeline missing its settings does not pass.
		_ = cli.ShowRootCommandHelp(root)
		return fmt.Errorf("no command given: run %q to publish", "mark "+PublishCommand)
	}

	// Arguments that are not flags used to be ignored, which made a mistyped
	// command publish. A command it could be is refused; so is anything at
	// all when --files is not given. Anything else is still ignored: it is
	// usually a --files pattern the shell expanded.
	if args := root.Args().Slice(); len(args) > 0 {
		if command := closestCommand(root, args[0]); command != "" {
			return fmt.Errorf("unknown command %q: did you mean %q?", args[0], command)
		}
		if !files {
			return fmt.Errorf("unknown command %q", args[0])
		}
		log.Warn().Msgf("ignoring %s: --files takes one pattern, so quote a glob", strings.Join(args, " "))
	}

	if _, err := checkPublishFlags(ctx, root); err != nil {
		return err
	}

	log.Warn().Msg(`running mark without a command is deprecated: run "mark publish" with the same flags instead`)

	return line.publish(ctx, root)
}

// closestCommand is the name of root's command that name is a typo of, or "".
func closestCommand(root *cli.Command, name string) string {
	for _, command := range root.VisibleCommands() {
		for _, candidate := range command.Names() {
			if len(candidate) >= 3 && editDistance(name, candidate) <= max(1, len(candidate)/3) {
				return candidate
			}
		}
	}
	return ""
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
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

	log.Debug().Msg("config:")
	// Every command from this one up to the root, each flag once: the help
	// flag, for one, is on all of them.
	seen := map[string]bool{}
	for _, command := range cmd.Lineage() {
		for _, f := range command.Flags {
			flag := f.Names()
			if seen[flag[0]] {
				continue
			}
			seen[flag[0]] = true
			// A command can name a token inline, so it leaks the same way the password would.
			if flag[0] == "password" || flag[0] == "password-command" {
				log.Debug().Msgf("%20s: %v", flag[0], "******")
			} else {
				log.Debug().Msgf("%20s: %v", flag[0], cmd.Value(flag[0]))
			}
		}
	}

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
		Features:         EnabledFeatures(cmd),
		ImageAlign:       cmd.String("image-align"),
		AttachReferenced: cmd.Bool("attach-referenced"),
		IncludePath:      cmd.String("include-path"),
		PageHeader:       cmd.String("page-header"),

		Output: os.Stdout,
	}

	// RunContext shuts the shared browser down on the way out, however the run
	// ends -- including when ctx is cancelled by a signal.
	return mark.RunContext(ctx, config)
}

// ConfigFilePath is the default for --config, or "" when there is none.
//
// The flags are built before any of them can name a file, so failing here
// would fail every invocation, --help included. A minimal container with
// neither $XDG_CONFIG_HOME nor $HOME has no default file; --config and
// MARK_CONFIG still name one.
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
