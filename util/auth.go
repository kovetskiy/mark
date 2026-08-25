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
// A helper that prompts for input or hangs on a locked keyring then fails the run instead of stalling it.
const passwordCommandTimeout = 30 * time.Second

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

	// Either can arrive from the flag, the environment or the config file, so a password left in one layer would otherwise shadow the command in silence.
	if password != "" && passwordCommand != "" {
		log.Warn().Msg("both password and password-command are set; using password and ignoring password-command")
	}

	// Ahead of the empty-password check below, otherwise supplying only a command errors out before the command ever runs.
	// Behind the "-" branch above, so that whatever the command prints is the token itself and cannot be taken for the flag that says to read one from standard input.
	// A compile-only run never authenticates, so running the command there would prompt for a passphrase or wait on a hardware key to produce a token nothing uses.
	if password == "" && passwordCommand != "" && !compileOnly {
		password, err = runPasswordCommand(ctx, passwordCommand)
		if err != nil {
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

// runPasswordCommand runs command and returns its trimmed stdout.
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

	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("command did not complete: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}

	password := strings.TrimSpace(string(out))
	if password == "" {
		return "", errors.New("command produced no output")
	}

	return password, nil
}
