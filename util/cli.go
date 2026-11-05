package util

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	mark "github.com/kovetskiy/mark/v17"
	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/export"
	"github.com/kovetskiy/mark/v17/math"
	"github.com/kovetskiy/mark/v17/mermaid"
	"github.com/kovetskiy/mark/v17/metadata"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

const (
	usage       = "A tool for updating Atlassian Confluence pages from markdown."
	description = `Mark is a tool to update Atlassian Confluence pages from markdown. Documentation is available here: https://github.com/kovetskiy/mark`

	// PublishCommand is the name of the command that publishes markdown.
	PublishCommand = "publish"

	// ExportCommand is the name of the command that writes a page out as
	// markdown.
	ExportCommand = "export"
)

// Run runs mark's command line, args as os.Args holds it, for a binary of the
// given version.
func Run(ctx context.Context, version string, args []string) error {
	return newCommand(version).Run(ctx, args)
}

func newCommand(version string) *cli.Command {
	return buildCommand(version, RunPublish, RunExport)
}

// commandLine is what the actions of one run of mark's command line share.
type commandLine struct {
	config *configFile
	// publish and export do the work, once the flags are checked; tests
	// swap them out.
	publish, export cli.ActionFunc
}

// buildCommand is mark's command line: the global flags on the root, and a
// command for each thing mark does.
func buildCommand(version string, publish, export cli.ActionFunc) *cli.Command {
	config := newConfigFile(new(string))
	line := &commandLine{config: config, publish: publish, export: export}

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
		Flags:                 globalFlags(config),
		EnableShellCompletion: true,
		Action:                runRoot,
		OnUsageError:          publishFlagHint(publishCommand.Flags),
		Commands: []*cli.Command{publishCommand, {
			Name:  ExportCommand,
			Usage: "export a Confluence page, or a tree of them, to markdown (experimental)",
			Description: "Experimental: the Markdown it writes may change between releases. " +
				"Write the page --page-id, --url or --space and --title names as markdown, with the " +
				"metadata headers that publish it back to the same page, and download the attachments it shows or links to.",
			Flags:  exportFlags(config),
			Action: line.runExport,
		}, line.pageCommand(), line.doctorCommand()},
	}

	return root
}

// prepare readies cmd's action: it reads the configuration file, resolves the
// global flags from it, and sets up logging.
func (line *commandLine) prepare(cmd *cli.Command) error {
	if err := line.config.check(); err != nil {
		return err
	}
	if err := line.config.settle(cmd.Root()); err != nil {
		return err
	}

	return SetUpLogging(cmd)
}

func (line *commandLine) runPublish(ctx context.Context, cmd *cli.Command) error {
	if err := line.prepare(cmd); err != nil {
		return err
	}
	// An unquoted --files glob the shell expanded would publish only its
	// first match.
	if cmd.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: --files takes one pattern, so quote a glob", cmd.Args().First())
	}
	if _, err := checkPublishFlags(ctx, cmd); err != nil {
		return err
	}

	return line.publish(ctx, cmd)
}

func (line *commandLine) runExport(ctx context.Context, cmd *cli.Command) error {
	if err := line.prepare(cmd); err != nil {
		return err
	}
	if _, err := CheckExportFlags(ctx, cmd); err != nil {
		return err
	}

	return line.export(ctx, cmd)
}

// runRoot is "mark" with no command. It fails, so a pipeline that names no
// command does not pass.
func runRoot(_ context.Context, root *cli.Command) error {
	if root.NArg() == 0 {
		_ = cli.ShowRootCommandHelp(root)
		return fmt.Errorf("no command given: run %q to publish", "mark "+PublishCommand)
	}

	return unknownCommand(root, root.Args().First())
}

// unknownCommand refuses name, which is none of root's commands, naming the one
// it is a typo of if there is one.
func unknownCommand(root *cli.Command, name string) error {
	if command := closestCommand(root, name); command != "" {
		return fmt.Errorf("unknown command %q: did you mean %q?", name, command)
	}

	return fmt.Errorf("unknown command %q", name)
}

// publishFlagHint names the command a flag the root does not have belongs to,
// when it is one of publish's.
func publishFlagHint(flags []cli.Flag) cli.OnUsageErrorFunc {
	publish := map[string]bool{}
	for _, flag := range flags {
		for _, name := range flag.Names() {
			publish[name] = true
		}
	}

	return func(_ context.Context, cmd *cli.Command, err error, _ bool) error {
		// urfave/cli reads flags after an argument too, so "mark publsh -f
		// doc.md" fails on -f before the argument can be named.
		first := cmd.Args().First()
		if first != "" && cmd.Command(first) == nil {
			return unknownCommand(cmd, first)
		}

		name, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -")
		if ok && publish[name] {
			return fmt.Errorf("%w: it is a flag of %q, so write it after %q", err, PublishCommand, PublishCommand)
		}

		return err
	}
}

// closestCommand is the name of root's command that name is a typo of, or "".
func closestCommand(root *cli.Command, name string) string {
	var names []string
	for _, command := range root.VisibleCommands() {
		names = append(names, command.Names()...)
	}
	return closest(name, names)
}

// closest is the candidate that name is a typo of, or "": the nearest one
// within a third of its length, the first of those equally near.
func closest(name string, candidates []string) string {
	best, distance := "", 0
	for _, candidate := range candidates {
		if len(candidate) < 3 {
			continue
		}
		d := editDistance(name, candidate)
		if d <= max(1, len(candidate)/3) && (best == "" || d < distance) {
			best, distance = candidate, d
		}
	}
	return best
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

	// Only a false somebody gave: a command built without the flag reads as
	// false, and keeps watching.
	unwatch := cmd.IsSet("watch") && !cmd.Bool("watch")

	logFlags(cmd)

	parents := splitParents(cmd.String("parents"), cmd.String("parents-delimiter"))

	config := mark.Config{
		PublishConfig: mark.PublishConfig{
			Page: mark.PageConfig{
				PageID:                   creds.PageID,
				Space:                    cmd.String("space"),
				Parents:                  parents,
				TitleFromH1:              cmd.Bool("title-from-h1"),
				TitleFromFilename:        cmd.Bool("title-from-filename"),
				TitleAppendGeneratedHash: cmd.Bool("title-append-generated-hash"),
				ContentAppearance:        metadata.Appearance(cmd.String("content-appearance")),
			},
			Mode: mark.ModeConfig{
				CompileOnly: cmd.Bool("compile-only"),
				DryRun:      cmd.Bool("dry-run"),
			},
			Update: mark.UpdateConfig{
				MinorEdit:        cmd.Bool("minor-edit"),
				VersionMessage:   cmd.String("version-message"),
				EditLock:         cmd.Bool("edit-lock"),
				ChangesOnly:      cmd.Bool("changes-only"),
				AppendLabels:     cmd.Bool("append-labels"),
				GlobalProperties: cmd.String("global-properties"),
				PreserveComments: cmd.Bool("preserve-comments"),
			},
			Links: mark.LinkConfig{
				CheckLinks:         cmd.StringSlice("check-links"),
				CheckLinksWarnOnly: cmd.Bool("check-links-warn-only"),
			},
			Rendering: mark.RenderingConfig{
				DropH1:           cmd.Bool("drop-h1"),
				StripLinebreaks:  cmd.Bool("strip-linebreaks"),
				MermaidEngine:    mermaid.EngineKind(cmd.String("mermaid-engine")),
				MermaidScale:     cmd.Float("mermaid-scale"),
				MermaidOutput:    cmd.String("mermaid-output"),
				MermaidBundle:    cmd.Bool("mermaid-bundle"),
				D2Output:         cmd.String("d2-output"),
				D2BundleRemote:   cmd.Bool("d2-bundle-remote"),
				D2Scale:          cmd.Float("d2-scale"),
				MathFormat:       math.Format(mathFormat(cmd)),
				MathScale:        cmd.Float("math-scale"),
				Features:         EnabledFeatures(cmd),
				ImageAlign:       cmd.String("image-align"),
				AttachReferenced: cmd.Bool("attach-referenced"),
				IncludePath:      cmd.String("include-path"),
				PageHeader:       cmd.String("page-header"),
			},
			Output: os.Stdout,
		},
		Files: cmd.String("files"),
		Connection: mark.ConnectionConfig{
			BaseURL:               creds.BaseURL,
			Username:              creds.Username,
			Password:              creds.Password,
			InsecureSkipTLSVerify: cmd.Bool("insecure-skip-tls-verify"),
		},
		ParentsFromPath:     cmd.Bool("parents-from-path"),
		ParentsFromPathRoot: cmd.String("parents-from-path-root"),
		ContinueOnError:     cmd.Bool("continue-on-error"),
		CI:                  cmd.Bool("ci"),
		Unwatch:             unwatch,
		Tracking: mark.TrackingConfig{
			TrackPages:     cmd.Bool("track-pages"),
			ManifestPage:   cmd.String("manifest-page"),
			ManifestPrefix: cmd.String("manifest-prefix"),
			OnOrphan:       report.OnOrphan(cmd.String("on-orphan")),
			OrphanUnder:    cmd.String("orphan-under"),
			NoOverwrite:    cmd.Bool("no-overwrite"),
		},
		Report: mark.ReportConfig{
			OutputFormat: report.Format(cmd.String("output-format")),
		},
	}

	// Run shuts the browsers it started down on the way out, however the run
	// ends -- including when ctx is cancelled by a signal.
	return mark.Run(ctx, config)
}

// exportBaseURL is the instance "mark export --url" reads from: the URL's own,
// unless a base URL is configured. A page id names a page of one instance
// only, so a configured base URL on another host is refused rather than asked
// for an unrelated page -- except for the api.atlassian.com gateway, which is
// where a scoped token reaches a Cloud site.
func exportBaseURL(configured, fromURL string) (string, error) {
	if configured == "" {
		return fromURL, nil
	}

	want, err := url.Parse(fromURL)
	if err != nil {
		return "", fmt.Errorf("unable to read %q: %w", fromURL, err)
	}
	have, err := url.Parse(configured)
	if err != nil {
		return "", fmt.Errorf("unable to read base URL %q: %w", configured, err)
	}

	sameHost := strings.EqualFold(have.Hostname(), want.Hostname()) && portOf(have) == portOf(want)
	gateway := strings.EqualFold(have.Hostname(), "api.atlassian.com") && confluence.IsCloudHost(want.Hostname())
	if !sameHost && !gateway {
		return "", fmt.Errorf(
			"--url is a page on %s, but the base URL is %s, and a page id names a page of one instance only: "+
				"leave the base URL out to read from %s, or name the page with --page-id to read from %s",
			want.Host, configured, want.Host, have.Host,
		)
	}

	return configured, nil
}

// portOf is the port a URL connects to, its scheme's default when it names none.
func portOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
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

		pageID, title = ref.PageID, ref.Title
		if ref.Space != "" {
			space = ref.Space
		}
		baseURL, err = exportBaseURL(baseURL, ref.BaseURL)
		if err != nil {
			return err
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

	config := mark.ExportConfig{
		Connection: mark.ConnectionConfig{
			BaseURL:               creds.BaseURL,
			Username:              creds.Username,
			Password:              creds.Password,
			InsecureSkipTLSVerify: cmd.Bool("insecure-skip-tls-verify"),
		},

		PageID: pageID,
		Space:  space,
		Title:  title,

		Output:         cmd.String("output"),
		AttachmentsDir: cmd.String("attachments-dir"),
		Overwrite:      cmd.Bool("overwrite"),
		NoAttachments:  cmd.Bool("no-attachments"),
		Format:         export.Format(cmd.String("format")),
		Metadata:       export.Metadata(cmd.String("metadata")),
		Generator:      generator(cmd.Root().Version),

		Stdout: os.Stdout,
	}

	if cmd.Bool("recursive") {
		results, err := mark.ExportTree(ctx, config, export.TreeOptions{
			Depth:         cmd.Int("depth"),
			RelativeLinks: cmd.Bool("relative-links"),
		})
		if err == nil {
			log.Info().Msgf("exported %d page(s) to %s", len(results), config.Output)
		}
		return err
	}

	_, err = mark.Export(ctx, config)

	return err
}

// generator names mark and its version, as OKF's generated.by does, without
// the commit the binary's version carries after "@", so that two builds of a
// release write the same files. Without a version it is empty, which the
// export takes as "mark/unknown".
func generator(version string) string {
	version, _, _ = strings.Cut(version, "@")
	if version == "" {
		return ""
	}
	return "mark/" + version
}

// logFlags logs every flag a command was run with, at debug level, with the
// credentials masked.
func logFlags(cmd *cli.Command) {
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
