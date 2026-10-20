package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	mark "github.com/kovetskiy/mark/v16"
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
)

// newCommand builds mark's command line: the global flags on the root, and a
// command for each thing mark does. It is run through run, which reads a
// command line naming no command the way mark always has; see Run.
func newCommand(version string) *cli.Command {
	// Where the configuration file is. The "config" flag fills in its path,
	// and every other flag reads the file through it.
	config := newConfigFile(new(string), true)

	return &cli.Command{
		Name:                  "mark",
		Usage:                 usage,
		Description:           description,
		Version:               version,
		Flags:                 globalFlags(config),
		EnableShellCompletion: true,
		// urfave/cli runs every Before in the chain only once the whole
		// command line has been parsed, so this sees the configuration file
		// the invocation named, whichever command it was given to.
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			// urfave/cli runs this for its own help and completion commands
			// too, and for a shell asking for completions. None of them reads
			// the configuration, so a broken file must not stop them: it is
			// reported by the command that would have used it.
			if answersItself(ctx, cmd) {
				return ctx, nil
			}
			if err := config.check(); err != nil {
				return ctx, err
			}
			if err := config.settle(cmd); err != nil {
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
				Flags: publishFlags(config),
				Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
					if answersItself(ctx, cmd) {
						return ctx, nil
					}
					return checkPublishFlags(ctx, cmd)
				},
				Action: RunPublish,
			},
		},
	}
}

// completing marks, in the context, a run a shell started to ask for
// completions.
type completing struct{}

// completionFlag is the flag a shell's completion script ends the command
// line with, to ask for completions rather than a run.
var completionFlag = "--" + cli.GenerateShellCompletionFlag.Names()[0]

// builtInCommands are the names of the commands urfave/cli adds for itself:
// help, under every command that has a help flag, and the root's completion
// command. They are not in a command's Commands until it runs.
//
// The help command is named as the help flag is -- both come from the same two
// constants in urfave/cli -- so its names are taken from the flag rather than
// spelled out again here.
func builtInCommands(root *cli.Command) map[string]bool {
	names := map[string]bool{}
	for _, name := range cli.HelpFlag.Names() {
		names[name] = true
	}

	completion := root.ShellCompletionCommandName
	if completion == "" {
		completion = "completion"
	}
	names[completion] = true

	return names
}

// answersItself says whether the command line, as cmd's Before sees it, runs
// something that prints about mark rather than doing anything: a help or
// completion command urfave/cli adds, at any depth -- "mark help", "mark
// publish help" -- or a shell's completion request.
func answersItself(ctx context.Context, cmd *cli.Command) bool {
	if asking, _ := ctx.Value(completing{}).(bool); asking {
		return true
	}

	root := cmd.Root()
	builtIn := builtInCommands(root)

	// Every command in the chain has been parsed by the time a Before runs,
	// so each one's arguments say which command it went on to.
	for current := root; current.Args().Present(); {
		name := current.Args().First()
		next := current.Command(name)
		if next == nil {
			return false
		}
		if builtIn[name] && !slices.ContainsFunc(current.Commands, func(c *cli.Command) bool {
			return c != next && c.HasName(name)
		}) {
			return true
		}
		current = next
	}

	return false
}

// Run runs mark's command line: args, as os.Args holds it, for a binary of the
// given version.
//
// Before there were commands, "mark <flags>" was the one thing mark did, and it
// is how mark runs in a great many CI pipelines. A command line that names no
// command therefore still publishes, exactly as "mark publish <flags>" would,
// and says on standard error that it is deprecated. "--help" and "--version"
// are the exception: on their own they are about mark as a whole.
//
// This is the only way in to the command line: reading the bare form takes
// looking at the arguments before urfave/cli parses them, which a command
// built and run on its own would not do.
func Run(ctx context.Context, version string, args []string) error {
	return run(ctx, newCommand(version), args)
}

// run is Run, for a command a test has built, and perhaps given writers.
func run(ctx context.Context, cmd *cli.Command, args []string) error {
	args, bare := defaultToPublish(cmd, args)
	if len(args) > 0 && args[len(args)-1] == completionFlag {
		ctx = context.WithValue(ctx, completing{}, true)
	} else if bare && !asksForHelp(args) {
		// Said here rather than logged, so that it is said whatever the log
		// level, and before anything can fail: a run that never gets as far
		// as publishing is one that still has to be migrated.
		errWriter := cmd.ErrWriter
		if errWriter == nil {
			errWriter = os.Stderr
		}
		_, _ = fmt.Fprintln(errWriter,
			`mark: running mark without a command is deprecated: run "mark publish" with the same flags instead`)
	}

	return cmd.Run(ctx, args)
}

// flagName is the name a command-line argument gives a flag, and the value
// written after its "=", if it is a flag at all.
func flagName(arg string) (name, value string, inline, isFlag bool) {
	if len(arg) < 2 || arg[0] != '-' {
		return "", "", false, false
	}

	name, value, inline = strings.Cut(strings.TrimLeft(arg, "-"), "=")

	return name, value, inline, true
}

// asksForHelp says whether args, before any "--", holds a help flag.
func asksForHelp(args []string) bool {
	help := cli.HelpFlag.Names()
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if name, _, _, isFlag := flagName(arg); isFlag && slices.Contains(help, name) {
			return true
		}
	}

	return false
}

// flagTable is what the flags of the commands in force at some point of a
// command line say about the arguments there: which names have a value after
// them, and which belong to the root.
type flagTable struct {
	takesValue map[string]bool
	global     map[string]bool
}

func (t flagTable) add(flags []cli.Flag, global bool) {
	for _, flag := range flags {
		valued, ok := flag.(interface{ TakesValue() bool })
		for _, name := range flag.Names() {
			t.takesValue[name] = ok && valued.TakesValue()
			if global {
				t.global[name] = true
			}
		}
	}
}

// defaultToPublish puts "publish" into a command line that names no command,
// and says whether it did.
//
// A command is named by the first argument that is neither a flag nor a flag's
// value. Telling a value from a command takes knowing which flags have one: the
// root's, and in a command line that names none, publish's, which is the
// command it then means. So "mark --version-message publish -f doc.md"
// publishes with the message "publish", as it always did.
//
// The version flag is answered by the root command, whatever else the command
// line holds, so it is all that is passed on: "mark -f doc.md --version" printed
// the version before there were commands, and "mark publish --version" prints
// it now. Not next to a help flag or a shell's completion request, though, which
// came first before there were commands and still do. A version flag that is
// false asks for nothing, and is dropped.
//
// The help flag and the one shell completion asks with, when they come with
// nothing but global flags: "mark --help" is about mark as a whole, and lists
// the commands and the global flags. Next to a flag only publish has, though,
// they are about publish -- "mark -f doc.md --help" is read as "mark publish -f
// doc.md --help", as it would be without the help flag, rather than failing at
// the root on a flag it does not define.
//
// A command urfave/cli adds for itself -- help, completion -- named after a flag
// only publish has is not one: before there were commands, "mark -f doc.md
// help" published and ignored the stray argument. It still does, and the
// argument is dropped, since publish would otherwise read it as its own help
// command.
func defaultToPublish(root *cli.Command, args []string) ([]string, bool) {
	if len(args) == 0 {
		return args, false
	}

	builtIn := builtInCommands(root)
	help := cli.HelpFlag.Names()
	version := cli.VersionFlag.Names()
	asksHelp := asksForHelp(args) || args[len(args)-1] == completionFlag

	table := flagTable{takesValue: map[string]bool{}, global: map[string]bool{}}
	table.add(root.Flags, true)
	if publish := root.Command(PublishCommand); publish != nil {
		table.add(publish.Flags, false)
	}

	// Whether the command line asks for the version, whether it has a flag the
	// root does not know, which makes it publish's, which command it names, if
	// any, and the arguments that ask for nothing.
	versionArg, commandFlag, named := "", false, false
	var drop []int

scan:
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}

		if name, value, inline, isFlag := flagName(arg); isFlag {
			switch {
			case slices.Contains(version, name):
				if inline && value == "false" {
					drop = append(drop, i)
				} else {
					versionArg = arg
				}
				continue
			case slices.Contains(help, name), arg == completionFlag:
				continue
			}
			if !named && !table.global[name] {
				commandFlag = true
			}
			if table.takesValue[name] && !inline {
				i++
			}
			continue
		}

		if named {
			// A positional argument of the command already named.
			continue
		}

		command := root.Command(arg)
		switch {
		case command != nil:
			// From here on the arguments are that command's, and so are
			// the flags among them.
			named = true
			table = flagTable{takesValue: map[string]bool{}, global: table.global}
			table.add(root.Flags, true)
			table.add(command.Flags, false)
		case builtIn[arg] && commandFlag:
			drop = append(drop, i)
			break scan
		case builtIn[arg]:
			// Its arguments are its own, and say nothing about the version.
			named = true
			break scan
		default:
			// A stray argument, which publish ignores.
			break scan
		}
	}

	if versionArg != "" && !asksHelp {
		return []string{args[0], versionArg}, false
	}

	if len(drop) > 0 {
		kept := make([]string, 0, len(args))
		for i, arg := range args {
			if !slices.Contains(drop, i) {
				kept = append(kept, arg)
			}
		}
		args = kept
	}

	if named || (asksHelp && !commandFlag) {
		return args, false
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

// ConfigFilePath is the default for --config, or "" when there is none.
//
// It is evaluated whenever the flags are built -- for every run, and for the
// deprecated Flags at package initialisation -- so failing here would fail
// every invocation, --version and --help included, and before any flag could
// say where the file is. That is exactly the environment
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
