package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// passwordCommandTimeout bounds how long a password command may run.
// A helper that hangs on a locked keyring fails the run instead of stalling it,
// with room left for one that opens a terminal of its own -- pinentry against a
// cold gpg-agent is the ordinary case -- and waits for a passphrase to be typed.
const passwordCommandTimeout = 60 * time.Second

// passwordCommandWaitDelay bounds the wait for the helper's output pipe once
// the helper itself has been killed, so that a process it left behind cannot
// hold the run open past the timeout.
const passwordCommandWaitDelay = 2 * time.Second

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	ctx context.Context,
	username string,
	password string,
	passwordCommand string,
	targetURL string,
	baseURL string,
	compileOnly bool,

) (*Credentials, error) {
	var err error

	// Ahead of the empty check and not after it. "-p -" does not carry a token,
	// it says where the token is, so whether one was given at all is only known
	// once standard input has been read. Checked first, the check passed on the
	// "-" itself and a run with nothing piped in went on to authenticate with
	// an empty password -- reported by Confluence as a 401, which reads as the
	// wrong credential rather than as a missing one.
	if password == "-" {
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("unable to read password from stdin: %w", err)
		}

		password = strings.TrimSpace(string(stdin))
	}

	// Both still carrying a value means they came from the same layer, since
	// passwordPrecedence has already dropped the weaker of two layers. Warn
	// rather than settle it in silence: a password left beside a command is
	// usually the older of the two.
	if password != "" && passwordCommand != "" {
		log.Warn().Msg("both password and password-command are set; using password and ignoring password-command")
	}

	// Ahead of the empty-password check below, otherwise supplying only a command errors out before the command ever runs.
	// Behind the "-" branch above, so that whatever the command prints is the token itself and cannot be taken for the flag that says to read one from standard input.
	//
	// A compile-only run is not exempt. It looks like one that never
	// authenticates, and it is not: a relative link to another document is
	// resolved by looking that page up, so compiling one file that links to
	// another makes two authenticated requests before it prints anything.
	// Skipping the command there left the password at "none" and 401'd them --
	// while the same configuration with password= set worked. --check-links
	// confluence is the same shape. What a compile does not do is fail when the
	// helper is unavailable; see below.
	if password == "" && passwordCommand != "" {
		password, err = runPasswordCommand(ctx, passwordCommand)

		switch {
		case err == nil:
			// What it printed is the password.

		case compileOnly:
			// Validating documents without Confluence credentials is the whole
			// of what --compile-only is for, and a helper that is absent -- a
			// CI image without the password manager, a configuration file
			// shared with a workstation -- must not take that offline. The
			// command is still resolved whenever it can be, which is what the
			// links a compile follows need.
			log.Warn().Msgf(
				"unable to read password from command: %s; continuing without a token, as --compile-only is set",
				err,
			)

		default:
			return nil, fmt.Errorf("unable to read password from command: %w", err)
		}
	}

	if password == "" {
		if !compileOnly {
			return nil, errors.New(
				"confluence password should be specified using -p " +
					"flag or be stored in configuration file",
			)
		}
		password = "none"
	}

	if compileOnly && targetURL == "" {
		targetURL = "http://localhost"
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse %q as url: %w", targetURL, err)
	}

	if url.Host == "" && baseURL == "" {
		return nil, errors.New(
			"confluence base URL should be specified using -l " +
				"flag or be stored in configuration file",
		)
	}

	if baseURL == "" {
		baseURL = url.Scheme + "://" + url.Host
	}

	baseURL = strings.TrimRight(baseURL, `/`)

	pageID := url.Query().Get("pageId")

	creds := &Credentials{
		Username: username,
		Password: password,
		BaseURL:  baseURL,
		PageID:   pageID,
	}

	return creds, nil
}

// runPasswordCommand runs command and returns the first line of what it printed.
// No shell is involved, so the string undergoes no expansion and quoted arguments are not honoured.
func runPasswordCommand(ctx context.Context, command string) (string, error) {
	argv := strings.Fields(command)
	if len(argv) == 0 {
		return "", errors.New("command is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, passwordCommandTimeout)
	defer cancel()

	// Running this through a shell would let a config value expand into a command nobody wrote.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: operator-supplied command

	// Output() captures stdout only, and its ExitError does not reliably carry stderr.
	// Without this the helper's own diagnostics are lost.
	cmd.Stderr = os.Stderr

	// Cancelling kills the helper, and nothing else: Wait goes on reading the
	// stdout pipe for as long as anything still holds it open, which for a
	// helper that starts an agent -- gpg-agent, on demand, is the ordinary case
	// -- is long after the helper itself is gone. Without a delay the deadline
	// bounds the process and not the call, which is the opposite of what it is
	// for.
	cmd.WaitDelay = passwordCommandWaitDelay

	out, err := cmd.Output()
	password := firstLine(string(out))

	// The command's own outcome first. Output() returns once the pipe closes,
	// which can be after the deadline even for a helper that printed its token
	// and exited cleanly -- and reading ctx.Err() ahead of err threw that token
	// away and failed a run that had everything it needed.
	switch {
	case err == nil:
		// Nothing to explain.

	case errors.Is(err, exec.ErrWaitDelay) && password != "":
		// The helper printed its token and exited; something it started still
		// held the output pipe, so Wait gave up on the pipe rather than on the
		// command. What the helper printed was read before any of that.
		log.Debug().Msg(
			"password command left a process holding its output; using the token it printed",
		)

	case ctx.Err() != nil:
		return "", fmt.Errorf(
			"command did not complete within %s: %w", passwordCommandTimeout, ctx.Err(),
		)

	default:
		return "", fmt.Errorf("command failed: %w", err)
	}

	if password == "" {
		return "", errors.New("command produced no output")
	}

	return password, nil
}

// firstLine returns the first line of what a helper printed, trimmed.
//
// A password manager prints an entry, not a token: pass show, which the README
// names, puts the password on the first line and metadata below it. Keeping the
// rest would put a newline inside the Authorization header -- rejected by the
// transport with an error naming nothing about the helper, or, once a username
// makes it Basic auth, base64-encoded into a silent 401. The same applies to
// anything a process the helper left behind appends to the pipe.
func firstLine(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")

	return strings.TrimSpace(line)
}

// passwordPrecedence returns the password and the password command a run should
// go on with, decided by where each of them was set.
//
// Each of the two resolves through its own chain -- command line, then
// environment, then configuration file -- so a rule stated over the two
// resolved values alone cannot tell a token typed on the command line from one
// that has been sitting in the configuration file for a year. Preferring the
// password whenever it is non-empty therefore made --password-command the one
// flag in mark that the configuration file overrides, and left editing that
// file the only way to use it. The setting from the stronger layer wins
// instead. A tie -- both from the same layer -- is left to GetCredentials,
// which keeps the password.
//
// args is the command line mark was run with, os.Args in a real run.
func passwordPrecedence(password string, passwordCommand string, args []string) (string, string) {
	if password == "" || passwordCommand == "" {
		return password, passwordCommand
	}

	byPassword := layerOf(args, "MARK_PASSWORD", "password", "p")
	byCommand := layerOf(args, "MARK_PASSWORD_COMMAND", "password-command")

	switch {
	case byCommand > byPassword:
		log.Warn().Msgf(
			"both password and password-command are set; using password-command from %s and ignoring password from %s",
			byCommand, byPassword,
		)

		return "", passwordCommand

	case byPassword > byCommand:
		log.Warn().Msgf(
			"both password and password-command are set; using password from %s and ignoring password-command from %s",
			byPassword, byCommand,
		)

		return password, ""
	}

	return password, passwordCommand
}

// layer is where a setting was given, ordered the way every flag in mark is
// resolved: the command line beats the environment, which beats the
// configuration file. urfave applies that order to each flag on its own and
// keeps no record of which layer answered, so a rule spanning two flags has to
// work it out again.
type layer int

const (
	fromConfigFile layer = iota
	fromEnvironment
	fromCommandLine
)

func (l layer) String() string {
	switch l {
	case fromCommandLine:
		return "the command line"

	case fromEnvironment:
		return "the environment"

	default:
		return "the configuration file"
	}
}

// layerOf reports where a setting that has a value was given. A value named
// nowhere on the command line and absent from the environment can only have
// come from the configuration file, which is the last place urfave looks.
func layerOf(args []string, env string, names ...string) layer {
	switch {
	case namedIn(args, names):
		return fromCommandLine

	case os.Getenv(env) != "":
		return fromEnvironment

	default:
		return fromConfigFile
	}
}

// namedIn reports whether any of names appears in args as a flag, in the forms
// urfave accepts: one dash or two, with the value either attached or following.
func namedIn(args []string, names []string) bool {
	for _, arg := range args {
		// Nothing past this is a flag, whatever it looks like.
		if arg == "--" {
			return false
		}

		for _, name := range names {
			for _, dashed := range []string{"-" + name, "--" + name} {
				if arg == dashed || strings.HasPrefix(arg, dashed+"=") {
					return true
				}
			}
		}
	}

	return false
}
