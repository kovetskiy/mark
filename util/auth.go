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

	"github.com/kovetskiy/mark/v17/confluence"
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

// ErrNoPageID is a --url that names more than the instance and carries no
// page id.
var ErrNoPageID = errors.New("the URL carries no page id")

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string

	// Offline says a compile was given no Confluence to reach: BaseURL is
	// then a placeholder, never to be asked anything.
	Offline bool
}

func GetCredentials(
	ctx context.Context,
	username string,
	password string,
	passwordCommand string,
	pageURL string,
	baseURL string,
	compileOnly bool,

) (*Credentials, error) {
	// Two answers to one question, and no way to tell which is meant: a
	// password left in the configuration file from before the command was set
	// up looks exactly like one somebody wants used. Either resolves through
	// its own chain -- command line, then environment, then configuration file
	// -- so preferring one of them by value cannot tell a token typed just now
	// from one that has sat in a file for a year, and preferring one by layer
	// only moves the guess. Say so instead, the way mark says it about every
	// other pair of settings that contradict each other.
	if password != "" && passwordCommand != "" {
		return nil, errors.New(
			"password and password-command are mutually exclusive. Please specify only one, " +
				"whether by flag, environment variable or configuration file",
		)
	}

	// Ahead of everything to do with the password. A run that cannot go
	// anywhere for want of a base URL has no business asking a keyring for a
	// token it will never send: putting somebody through a pinentry dialog and
	// then failing on the configuration is a strange order to do things in.
	pageURLGiven := pageURL != ""
	offline := compileOnly && !pageURLGiven && baseURL == ""
	if compileOnly && !pageURLGiven {
		pageURL = "http://localhost"
	}

	url, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse %q as url: %w", pageURL, err)
	}

	if url.Host == "" && baseURL == "" {
		return nil, errors.New(
			"confluence base URL should be specified using -l " +
				"flag or be stored in configuration file",
		)
	}

	contextPath, pageID := confluence.SplitPageURL(url)
	pageBase := url.Scheme + "://" + url.Host + contextPath

	if pageURLGiven && pageID == "" {
		pageID, err = shortLinkOrInstance(url, contextPath, baseURL)
		if err != nil {
			return nil, fmt.Errorf(
				"the page URL %q (--url, MARK_PAGE_URL or url in the configuration file) names no page "+
					"mark can publish to by its id: give the page's address as "+
					".../pages/viewpage.action?pageId=ID, .../spaces/KEY/pages/ID/Title or a short /x/ link, "+
					"or set it aside with --url \"\", set the base URL and name the page with Space and Title headers: %w",
				pageURL, err,
			)
		}
	}

	if baseURL == "" {
		baseURL = pageBase
	} else if pageURLGiven && url.Host != "" {
		err := checkPageURLInstance(baseURL, pageBase,
			"set the base URL to "+pageBase+", or leave --url out and name the page with Space and Title headers")
		if err != nil {
			return nil, err
		}
	}

	baseURL = strings.TrimRight(baseURL, `/`)

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

	// Ahead of the empty-password check below, otherwise supplying only a command errors out before the command ever runs.
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

	creds := &Credentials{
		Username: username,
		Password: password,
		BaseURL:  baseURL,
		PageID:   pageID,
		Offline:  offline,
	}

	return creds, nil
}

// instanceLanding are the paths, below the context path, a browser shows for
// the instance itself, which name no page of it.
var instanceLanding = map[string]bool{
	"":                  true,
	"/home":             true,
	"/dashboard.action": true,
	"/index.action":     true,
}

// shortLinkOrInstance settles a page URL in which SplitPageURL found no page
// id: a short link carries one, encoded, and a URL that names the instance
// alone needs none. Anything else names something in the instance that is not
// a page id, and reading it as the instance alone would publish wherever each
// document's headers say; it is ErrNoPageID.
func shortLinkOrInstance(target *url.URL, contextPath, baseURL string) (string, error) {
	// The base URL names the instance whatever its context path looks like,
	// and so does a page URL that repeats it.
	if base, err := url.Parse(baseURL); err == nil && baseURL != "" && target.Host != "" &&
		strings.TrimRight(base.EscapedPath(), "/") == strings.TrimRight(target.EscapedPath(), "/") {
		return "", nil
	}

	rest := strings.TrimPrefix(strings.TrimRight(target.EscapedPath(), "/"), contextPath)

	if code, ok := strings.CutPrefix(rest, "/x/"); ok {
		pageID, err := confluence.DecodeTinyLinkID(code)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrNoPageID, err)
		}

		return pageID, nil
	}

	// Without a host there is no instance for the value to name.
	if target.Host == "" || !instanceLanding[rest] {
		return "", ErrNoPageID
	}

	return "", nil
}

// checkPageURLInstance refuses a page URL, by its base pageBase, that is not
// on the configured instance, as confluence.SameInstance decides: the page id
// in it names a page of that one instance only. advice says what to do
// instead. The error wraps SameInstance's, so errors.Is matches
// confluence.ErrOtherInstance.
func checkPageURLInstance(configured, pageBase, advice string) error {
	err := confluence.SameInstance(configured, pageBase)
	if err != nil && !errors.Is(err, confluence.ErrOtherInstance) {
		return fmt.Errorf("unable to compare --url with the base URL: %w", err)
	}
	if err != nil {
		return fmt.Errorf(
			"--url is not a page of the instance the base URL (--base-url, MARK_BASE_URL or base-url "+
				"in the configuration file) names, and a page id names a page of one instance only (%s): %w",
			advice, err,
		)
	}

	return nil
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

	// A newline is what makes that first line whole. Without one, what was read
	// may be the front of a token whose tail never arrived, and a truncated
	// credential authenticates no better than no credential at all -- while
	// looking, from the 401 it comes back as, exactly like the wrong password.
	whole := strings.Contains(string(out), "\n")

	// The command's own outcome first. Output() returns once the pipe closes,
	// which can be after the deadline even for a helper that printed its token
	// and exited cleanly -- and reading ctx.Err() ahead of err threw that token
	// away and failed a run that had everything it needed.
	switch {
	case err == nil:
		// Nothing to explain.

	case errors.Is(err, exec.ErrWaitDelay) && whole:
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
