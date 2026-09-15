package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/export"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/urfave/cli/v3"
)

// PageCommand is the name of the command that acts on one Confluence page.
const PageCommand = "page"

// pageArgument is how the page commands' help names a page.
const pageArgument = "A page is named by its id, by its URL, or by its title together with --space."

// pageCommand is "mark page" and its subcommands. Each works on pages in
// Confluence alone: none reads a markdown file, and none updates what
// --track-pages recorded.
func (line *commandLine) pageCommand() *cli.Command {
	return &cli.Command{
		Name:  PageCommand,
		Usage: "list, move, copy, rename, archive, delete or watch Confluence pages (experimental)",
		Description: "Experimental: the commands and their flags may change between releases. " +
			"These act on Confluence directly, not on markdown: a page publish manages is put " +
			"back the way its file says the next time publish runs. " + pageArgument,
		Commands: []*cli.Command{
			{
				Name:      "tree",
				Usage:     "list a page, or a folder by id, and everything beneath it, with their ids",
				ArgsUsage: "<page>",
				Flags: append(pageFlags(line.config),
					&cli.IntFlag{
						Name:  "depth",
						Usage: "list this many levels, counting the page itself; 0 is every level.",
					},
				),
				Action: line.pageAction(1, checkPageTree, runPageTree),
			},
			{
				Name:      "move",
				Usage:     "move a page, and the pages beneath it, under another page or folder, or beside a sibling",
				ArgsUsage: "<page>",
				Flags: append(pageFlags(line.config),
					&cli.StringFlag{Name: "parent", Usage: "the page, or folder id, to move the page under."},
					&cli.StringFlag{Name: "before", Usage: "place the page directly before this sibling, under the sibling's parent."},
					&cli.StringFlag{Name: "after", Usage: "place the page directly after this sibling, under the sibling's parent."},
				),
				Action: line.pageAction(1, checkPageMove, runPageMove),
			},
			{
				Name:      "copy",
				Usage:     "copy a page under another, with its attachments, labels and content properties but not its restrictions (Confluence Cloud)",
				ArgsUsage: "<page>",
				Flags: append(pageFlags(line.config),
					&cli.StringFlag{Name: "parent", Usage: "the page to copy it under."},
					&cli.StringFlag{Name: "title", Usage: "the title of the copy. Defaults to --prefix and the page's own title."},
					&cli.StringFlag{Name: "prefix", Usage: "put this before the title of every copy --title does not name, such as \"Copy of \". A copy cannot take a title its space already has."},
					&cli.BoolFlag{Name: "recursive", Usage: "copy every page beneath it too, each under the copy of its parent."},
				),
				Action: line.pageAction(1, checkPageCopy, runPageCopy),
			},
			{
				Name:      "rename",
				Usage:     "change a page's title, leaving its content as it is",
				ArgsUsage: "<page> <new title>",
				Flags:     pageFlags(line.config),
				Action:    line.pageAction(2, nil, runPageRename),
			},
			{
				Name:      "archive",
				Usage:     "archive a page that has no children (Confluence Cloud only)",
				ArgsUsage: "<page>",
				Flags:     pageFlags(line.config),
				Action:    line.pageAction(1, nil, runPageArchive),
			},
			{
				Name:      "delete",
				Usage:     "move a page to the trash",
				ArgsUsage: "<page>",
				// No configuration file or environment variable sets
				// --recursive: it is typed for the one run that means it.
				Flags: append(pageFlags(line.config),
					&cli.BoolFlag{
						Name:  "recursive",
						Usage: "trash the pages beneath it too. Without it, a page with children is refused.",
					},
				),
				Action: line.pageAction(1, nil, runPageDelete),
			},
			{
				Name:      "watch",
				Usage:     "be notified of a page's changes, or have others be",
				ArgsUsage: "<page>",
				Flags:     append(pageFlags(line.config), watcherFlag("watch"), recursiveWatchFlag("watch")),
				Action:    line.pageAction(1, nil, runPageWatch),
			},
			{
				Name:      "unwatch",
				Usage:     "stop being notified of a page's changes, or have others stop",
				ArgsUsage: "<page>",
				Flags:     append(pageFlags(line.config), watcherFlag("unwatch"), recursiveWatchFlag("unwatch")),
				Action:    line.pageAction(1, nil, runPageUnwatch),
			},
		},
	}
}

// watcherFlag is --user, typed for the run like --recursive: watching for
// someone else takes space administration, and is not a standing setting.
func watcherFlag(verb string) cli.Flag {
	return &cli.StringSliceFlag{
		Name: "user",
		Usage: verb + " the page for this user rather than for yourself. Repeat or comma-separate for several. " +
			"On Cloud a user is named by full name, which has to match exactly one user, or as accountid:<id>; " +
			"on Server and Data Center by username. Needs space administration for anyone but yourself.",
	}
}

// recursiveWatchFlag is --recursive for watch and unwatch, typed for the run
// like delete's.
func recursiveWatchFlag(verb string) cli.Flag {
	return &cli.BoolFlag{
		Name:  "recursive",
		Usage: verb + " every page beneath it too. A page with a folder beneath it is refused, since the folder's pages are out of reach.",
	}
}

// pageFlags are the flags every page command has. "space" is publish's and
// export's setting, and only ever used to look a title up: an id or a URL
// names its page without it.
func pageFlags(config *configFile) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "space",
			Value:   "",
			Usage:   "the space key a page named by its title is looked up in.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_SPACE"), config.value("space")),
		},
		&cli.BoolFlag{
			Name:    "dry-run",
			Value:   false,
			Usage:   "say what would be done, and change nothing.",
			Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_DRY_RUN"), config.value("dry-run")),
		},
	}
}

// pageRun is what a page command's action gets: the API to work through, and
// a resolver for the pages its arguments and flags name on the same instance.
type pageRun struct {
	api  *confluence.API
	refs map[string]page.Ref
	out  io.Writer
}

type pageRunner func(run pageRun, cmd *cli.Command) error

// pageAction checks that a page command was given its args arguments and
// whatever check asks of its flags, reads every page they name, and works out
// the instance from --base-url or from the URLs among them.
func (line *commandLine) pageAction(args int, check func(*cli.Command) error, run pageRunner) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		if err := line.prepare(cmd); err != nil {
			return err
		}
		if cmd.NArg() != args {
			return fmt.Errorf("%q takes %d argument(s) and got %d. %s",
				"mark page "+cmd.Name+" "+cmd.ArgsUsage, args, cmd.NArg(), pageArgument)
		}
		if check != nil {
			if err := check(cmd); err != nil {
				return err
			}
		}

		values := cmd.Args().Slice()[:1]
		for _, name := range []string{"parent", "before", "after"} {
			if value := cmd.String(name); value != "" {
				values = append(values, value)
			}
		}

		baseURL := cmd.String("base-url")
		refs := map[string]page.Ref{}
		for _, value := range values {
			ref, base, err := parsePageRef(value, cmd.String("space"))
			if err != nil {
				return err
			}
			if base != "" && cmd.String("base-url") == "" {
				if baseURL != "" && baseURL != base {
					return fmt.Errorf("%s is on %s, not on %s: the pages of one command are on one Confluence", value, base, baseURL)
				}
				baseURL = base
			}
			refs[value] = ref
		}
		if baseURL == "" {
			return errors.New("confluence base URL should be specified using --base-url, or come with a page URL")
		}

		creds, err := resolveCredentials(ctx, cmd, "", baseURL, false)
		if err != nil {
			return err
		}

		logFlags(cmd)

		return run(pageRun{
			api: confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password, cmd.Bool("insecure-skip-tls-verify"),
				confluence.WithCookies(creds.Cookies)),
			refs: refs,
			out:  os.Stdout,
		}, cmd)
	}
}

// parsePageRef reads a page argument: a URL, an id, or a title in space. A
// title made only of digits is read as an id, and is named by URL instead.
func parsePageRef(value, space string) (page.Ref, string, error) {
	if strings.Contains(value, "://") {
		ref, err := export.ParsePageURL(value)
		if err != nil {
			return page.Ref{}, "", err
		}
		return page.Ref{ID: ref.PageID, Space: ref.Space, Title: ref.Title}, ref.BaseURL, nil
	}

	if strings.Trim(value, "0123456789") == "" {
		return page.Ref{ID: value}, "", nil
	}

	if space == "" {
		return page.Ref{}, "", fmt.Errorf("%q is neither a page id nor a URL: give --space to look it up by title", value)
	}

	return page.Ref{Space: space, Title: value}, "", nil
}

func (run pageRun) ref(cmd *cli.Command, flag string) *page.Ref {
	value := cmd.String(flag)
	if value == "" {
		return nil
	}
	ref := run.refs[value]
	return &ref
}

func checkPageMove(cmd *cli.Command) error {
	if cmd.String("before") != "" && cmd.String("after") != "" {
		return errors.New("--before and --after each place the page; give only one of them")
	}
	if cmd.String("parent") == "" && cmd.String("before") == "" && cmd.String("after") == "" {
		return errors.New("say where to move the page with --parent, --before or --after")
	}
	return nil
}

func checkPageTree(cmd *cli.Command) error {
	if cmd.Int("depth") < 0 {
		return errors.New("--depth counts levels, and cannot be negative")
	}
	return nil
}

func runPageTree(run pageRun, cmd *cli.Command) error {
	return page.Tree(run.api, run.refs[cmd.Args().First()], cmd.Int("depth"), run.out)
}

func checkPageCopy(cmd *cli.Command) error {
	if cmd.String("parent") == "" {
		return errors.New("say where to copy the page with --parent")
	}
	return nil
}

func runPageCopy(run pageRun, cmd *cli.Command) error {
	return page.Copy(run.api, run.refs[cmd.Args().First()], page.CopyOptions{
		Parent:    *run.ref(cmd, "parent"),
		Title:     cmd.String("title"),
		Prefix:    cmd.String("prefix"),
		Recursive: cmd.Bool("recursive"),
		DryRun:    cmd.Bool("dry-run"),
	}, run.out)
}

func runPageMove(run pageRun, cmd *cli.Command) error {
	return page.Move(run.api, run.refs[cmd.Args().First()], page.MoveOptions{
		Parent: run.ref(cmd, "parent"),
		Before: run.ref(cmd, "before"),
		After:  run.ref(cmd, "after"),
		DryRun: cmd.Bool("dry-run"),
	}, run.out)
}

func runPageRename(run pageRun, cmd *cli.Command) error {
	return page.Rename(run.api, run.refs[cmd.Args().First()], cmd.Args().Get(1), cmd.Bool("dry-run"), run.out)
}

func runPageArchive(run pageRun, cmd *cli.Command) error {
	return page.Archive(run.api, run.refs[cmd.Args().First()], cmd.Bool("dry-run"), run.out)
}

func runPageDelete(run pageRun, cmd *cli.Command) error {
	return page.Delete(run.api, run.refs[cmd.Args().First()], cmd.Bool("recursive"), cmd.Bool("dry-run"), run.out)
}

func runPageWatch(run pageRun, cmd *cli.Command) error {
	return page.Watch(run.api, run.refs[cmd.Args().First()], cmd.StringSlice("user"), true, cmd.Bool("recursive"), cmd.Bool("dry-run"), run.out)
}

func runPageUnwatch(run pageRun, cmd *cli.Command) error {
	return page.Watch(run.api, run.refs[cmd.Args().First()], cmd.StringSlice("user"), false, cmd.Bool("recursive"), cmd.Bool("dry-run"), run.out)
}
