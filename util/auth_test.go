package util

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

const (
	helperModeEnv   = "MARK_TEST_PASSWORD_HELPER"
	helperOutputEnv = "MARK_TEST_PASSWORD_HELPER_OUTPUT"
)

// TestMain doubles as the password command these tests run.
// That command has to be a real executable, and what is on PATH varies by platform, so re-executing the test binary removes the assumption.
// A child process inherits helperModeEnv, and TestMain hands it to the named helper instead of running the suite.
func TestMain(m *testing.M) {
	switch os.Getenv(helperModeEnv) {
	case "":
		os.Exit(m.Run())

	case "print":
		fmt.Println(os.Getenv(helperOutputEnv))
		os.Exit(0)

	case "fail":
		fmt.Fprintln(os.Stderr, "helper refused to produce a token")
		os.Exit(3)

	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)

	case "linger":
		// A helper that prints its token and exits, leaving something behind
		// that still holds the output pipe -- which is what a helper starting
		// an agent on demand does.
		child := exec.Command(os.Args[0]) //nolint:gosec // G204: the test binary itself
		child.Env = append(os.Environ(), helperModeEnv+"=hang")
		child.Stdout = os.Stdout
		_ = child.Start()

		fmt.Println(os.Getenv(helperOutputEnv))
		os.Exit(0)

	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(2)
	}
}

// helperCommand returns a password-command string that re-executes the test binary in the given mode.
func helperCommand(t *testing.T, mode string, output string) string {
	t.Helper()

	executable := os.Args[0]
	if strings.ContainsAny(executable, " \t") {
		t.Skipf("test binary path %q contains whitespace, which a password command cannot express", executable)
	}

	t.Setenv(helperModeEnv, mode)
	t.Setenv(helperOutputEnv, output)

	return executable
}

// captureLogs redirects the package logger for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logged bytes.Buffer
	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() { log.Logger = restore })

	return &logged
}

// feedStdin replaces os.Stdin with a pipe carrying value, for the duration of the test.
func feedStdin(t *testing.T, value string) {
	t.Helper()

	read, write, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stdin
	os.Stdin = read
	t.Cleanup(func() { os.Stdin = original })

	go func() {
		defer write.Close()
		_, _ = write.WriteString(value)
	}()
}

// TestGetCredentialsBaseURLFromTargetURL covers the shortest way to publish:
// paste the URL of an existing page and let mark take the instance and the page
// id out of it.
func TestGetCredentialsBaseURLFromTargetURL(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "user", "secret", "",
		"https://confluence.example.com/pages/viewpage.action?pageId=12345", "", false)
	require.NoError(t, err)

	assert.Equal(t, "https://confluence.example.com", creds.BaseURL)
	assert.Equal(t, "12345", creds.PageID)
	assert.Equal(t, "user", creds.Username)
	assert.Equal(t, "secret", creds.Password)
}

// TestGetCredentialsExplicitBaseURLWins covers -l beside a target URL: the flag
// is the instance, and the URL only supplies the page id.
func TestGetCredentialsExplicitBaseURLWins(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "user", "secret", "",
		"https://old.example.com/pages/viewpage.action?pageId=7", "https://new.example.com/wiki", false)
	require.NoError(t, err)

	assert.Equal(t, "https://new.example.com/wiki", creds.BaseURL)
	assert.Equal(t, "7", creds.PageID)
}

// TestGetCredentialsTrimsTrailingSlashes covers the base URL as people actually
// write it in a config file. Every request path is appended to this value, so a
// trailing slash would produce "//rest/api" and a 404 that says nothing about
// its cause.
func TestGetCredentialsTrimsTrailingSlashes(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "user", "secret", "", "", "https://confluence.example.com/wiki///", false)
	require.NoError(t, err)

	assert.Equal(t, "https://confluence.example.com/wiki", creds.BaseURL)
}

// TestGetCredentialsRequiresAPassword covers the message people see most often.
// It names the flag, because the alternative -- a 401 from Confluence -- sends
// them looking at their account instead of at their invocation.
func TestGetCredentialsRequiresAPassword(t *testing.T) {
	_, err := GetCredentials(context.Background(), "user", "", "", "", "https://confluence.example.com", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "-p")
}

// TestGetCredentialsRequiresABaseURL covers the same for the instance: with
// neither a target URL to derive it from nor -l, there is nothing to talk to.
func TestGetCredentialsRequiresABaseURL(t *testing.T) {
	_, err := GetCredentials(context.Background(), "user", "secret", "", "", "", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "-l")
}

// TestGetCredentialsCompileOnlyNeedsNothing covers --compile-only, which never
// reaches Confluence. Requiring credentials to render Markdown locally would
// make the flag useless in CI, so both are filled in with placeholders.
func TestGetCredentialsCompileOnlyNeedsNothing(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "", "", "", "", "", true)
	require.NoError(t, err)

	assert.Equal(t, "none", creds.Password)
	assert.Equal(t, "http://localhost", creds.BaseURL)
	assert.Empty(t, creds.PageID)
}

// TestGetCredentialsCompileOnlyKeepsRealValues is the boundary of that rule:
// the placeholders fill gaps, they do not override what was given.
func TestGetCredentialsCompileOnlyKeepsRealValues(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "user", "secret", "", "", "https://confluence.example.com", true)
	require.NoError(t, err)

	assert.Equal(t, "secret", creds.Password)
	assert.Equal(t, "https://confluence.example.com", creds.BaseURL)
}

// TestGetCredentialsPasswordFromStdin covers "-p -", which is how a token gets
// in without appearing in the process list or in shell history. The trailing
// newline a pipe or a heredoc adds has to go: it would be sent as part of the
// token and answered with a 401.
func TestGetCredentialsPasswordFromStdin(t *testing.T) {
	feedStdin(t, "token-from-stdin\n")

	creds, err := GetCredentials(context.Background(), "user", "-", "", "", "https://confluence.example.com", false)
	require.NoError(t, err)

	assert.Equal(t, "token-from-stdin", creds.Password)
}

// TestGetCredentialsRejectsAnEmptyPasswordOnStdin covers "-p -" with nothing
// piped in, which is what a shell substitution that produced nothing looks like.
//
// The flag names where the token is rather than carrying one, so an empty
// standard input means no token was given -- the same as passing none, and
// worth the same error. Sent as an empty password instead, it comes back from
// Confluence as a 401: the report for a credential that is wrong, not for one
// that was never there.
func TestGetCredentialsRejectsAnEmptyPasswordOnStdin(t *testing.T) {
	read, write, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, write.Close())

	original := os.Stdin
	os.Stdin = read

	defer func() { os.Stdin = original }()

	_, err = GetCredentials(context.Background(), "user", "-", "", "", "https://confluence.example.com", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "-p")
}

// TestGetCredentialsRejectsAnUnparseableURL covers the target URL that is not
// one at all, which is worth an error naming the value rather than a request to
// an empty host.
func TestGetCredentialsRejectsAnUnparseableURL(t *testing.T) {
	_, err := GetCredentials(context.Background(), "user", "secret", "", "https://exam ple.com/x", "", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "as url")
}

// TestRunPasswordCommandTrimsTheHelpersNewline covers the newline every helper adds.
// It would be sent as part of the token and answered with a 401, which is the same trap "-p -" has.
func TestRunPasswordCommandTrimsTheHelpersNewline(t *testing.T) {
	command := helperCommand(t, "print", "s3cret")

	password, err := runPasswordCommand(context.Background(), command)
	require.NoError(t, err)

	assert.Equal(t, "s3cret", password)
}

// TestRunPasswordCommandTrimsSurroundingWhitespace covers the helper that pads what it prints.
// Trimming both ends means a token survives a wrapper script that indents its output.
func TestRunPasswordCommandTrimsSurroundingWhitespace(t *testing.T) {
	command := helperCommand(t, "print", "  s3cret\t")

	password, err := runPasswordCommand(context.Background(), command)
	require.NoError(t, err)

	assert.Equal(t, "s3cret", password)
}

// TestRunPasswordCommandAcceptsAPaddedCommand covers padding around the command itself.
// strings.Fields absorbs it and collapses runs, so the setting needs no trimming of its own.
func TestRunPasswordCommandAcceptsAPaddedCommand(t *testing.T) {
	command := helperCommand(t, "print", "s3cret")

	password, err := runPasswordCommand(context.Background(), "  "+command+" \t ")
	require.NoError(t, err)

	assert.Equal(t, "s3cret", password)
}

// TestRunPasswordCommandRejectsACommandWithNoWords covers a setting that holds nothing runnable.
// Without the guard, indexing the first word would panic rather than report the empty value.
func TestRunPasswordCommandRejectsACommandWithNoWords(t *testing.T) {
	_, err := runPasswordCommand(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is empty")

	_, err = runPasswordCommand(context.Background(), "   \t ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is empty")
}

// TestRunPasswordCommandRejectsEmptyOutput covers the helper that succeeds and prints nothing.
// Accepting that would authenticate with an empty token and produce a 401 naming nothing useful.
func TestRunPasswordCommandRejectsEmptyOutput(t *testing.T) {
	command := helperCommand(t, "print", "   ")

	_, err := runPasswordCommand(context.Background(), command)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "produced no output")
}

// TestRunPasswordCommandReportsANonZeroExit covers the helper that refuses.
// A locked keyring and a missing entry both arrive this way.
func TestRunPasswordCommandReportsANonZeroExit(t *testing.T) {
	command := helperCommand(t, "fail", "")

	_, err := runPasswordCommand(context.Background(), command)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "command failed")
}

// TestRunPasswordCommandReportsAnUnrunnableCommand covers the name that is not on PATH at all.
// That is the shape of a typo in the setting, and it has to name the failure rather than yield an empty token.
func TestRunPasswordCommandReportsAnUnrunnableCommand(t *testing.T) {
	_, err := runPasswordCommand(context.Background(), "mark-no-such-password-helper")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "command failed")
}

// TestRunPasswordCommandStopsAHelperThatHangs covers the helper that waits on input nobody will give it.
// Unbounded, that stalls the whole run instead of failing it.
// The caller's deadline is the earlier of the two, so this exercises the branch without waiting out passwordCommandTimeout.
func TestRunPasswordCommandStopsAHelperThatHangs(t *testing.T) {
	command := helperCommand(t, "hang", "")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runPasswordCommand(ctx, command)
	require.Error(t, err)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "did not complete")
	assert.Less(t, time.Since(start), passwordCommandTimeout,
		"the caller's deadline should fire well before the built-in one")
}

// TestGetCredentialsResolvesThePasswordFromACommand covers the case the setting exists for.
// The token reaches mark without being written to the configuration file or exported into the environment.
func TestGetCredentialsResolvesThePasswordFromACommand(t *testing.T) {
	command := helperCommand(t, "print", "from-command")
	logged := captureLogs(t)

	creds, err := GetCredentials(context.Background(), "user", "", command, "https://confluence.example.com", "", false)
	require.NoError(t, err)

	assert.Equal(t, "from-command", creds.Password)
	assert.NotContains(t, logged.String(), "ignoring password-command",
		"nothing is being shadowed, so there is nothing to warn about")
}

// TestGetCredentialsPrefersAnExplicitPasswordOverTheCommand covers both being set.
// A flag on the command line has to be able to override a token already in the configuration file.
func TestGetCredentialsPrefersAnExplicitPasswordOverTheCommand(t *testing.T) {
	command := helperCommand(t, "print", "from-command")

	creds, err := GetCredentials(context.Background(), "user", "from-flag", command, "https://confluence.example.com", "", false)
	require.NoError(t, err)

	assert.Equal(t, "from-flag", creds.Password)
}

// TestGetCredentialsWarnsThatTheCommandIsIgnored covers what that precedence costs.
// Either value can arrive from a different layer, so a password left in one would otherwise shadow the command in silence.
func TestGetCredentialsWarnsThatTheCommandIsIgnored(t *testing.T) {
	command := helperCommand(t, "print", "from-command")
	logged := captureLogs(t)

	_, err := GetCredentials(context.Background(), "user", "from-flag", command, "https://confluence.example.com", "", false)
	require.NoError(t, err)

	assert.Contains(t, logged.String(), "ignoring password-command")
}

// TestGetCredentialsSurfacesAFailingCommand covers the helper that fails with no password set.
// Reporting the missing password instead would send people looking at their invocation rather than at their helper.
func TestGetCredentialsSurfacesAFailingCommand(t *testing.T) {
	command := helperCommand(t, "fail", "")

	_, err := GetCredentials(context.Background(), "user", "", command, "https://confluence.example.com", "", false)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "unable to read password from command: command failed")
}

// TestGetCredentialsTreatsACommandsDashAsTheToken covers a helper printing a bare dash.
// Whatever the command prints is the token, so it must not reach the "-p -" branch and block on stdin.
func TestGetCredentialsTreatsACommandsDashAsTheToken(t *testing.T) {
	command := helperCommand(t, "print", "-")
	feedStdin(t, "token-from-stdin\n")

	creds, err := GetCredentials(context.Background(), "user", "", command, "https://confluence.example.com", "", false)
	require.NoError(t, err)

	assert.Equal(t, "-", creds.Password)
}

// TestGetCredentialsResolvesTheCommandWhenCompilingOnly covers --compile-only
// beside a command.
//
// A compile looks like a run that never authenticates and is not one: a
// relative link to another document is resolved by looking that page up, so
// compiling one file that links to another makes two authenticated requests
// before it prints anything. Skipping the command left the password at "none"
// and 401'd them, while the same configuration with a literal password worked.
//
// The helper prints, so reaching its token proves it was invoked.
func TestGetCredentialsResolvesTheCommandWhenCompilingOnly(t *testing.T) {
	command := helperCommand(t, "print", "s3cret")

	creds, err := GetCredentials(context.Background(), "user", "", command, "https://confluence.example.com", "", true)
	require.NoError(t, err)

	assert.Equal(t, "s3cret", creds.Password)
}

// TestGetCredentialsCompileOnlyStillNeedsNoPassword is the boundary: a compile
// with nothing to run still gets its placeholder rather than an error.
func TestGetCredentialsCompileOnlyStillNeedsNoPassword(t *testing.T) {
	creds, err := GetCredentials(context.Background(), "user", "", "", "https://confluence.example.com", "", true)
	require.NoError(t, err)

	assert.Equal(t, "none", creds.Password)
}

// TestGetCredentialsCompileOnlyFailsOnAFailingCommand is the other side of that boundary.
// A compile authenticates, so a helper that cannot produce a token fails the run rather than being skipped.
// Falling back to "none" would turn a broken helper into a 401 further along, naming the wrong cause.
func TestGetCredentialsCompileOnlyFailsOnAFailingCommand(t *testing.T) {
	command := helperCommand(t, "fail", "")

	_, err := GetCredentials(context.Background(), "user", "", command, "https://confluence.example.com", "", true)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "command failed")
}

// TestRunPasswordCommandKeepsTheTokenOfAHelperThatLeftAProcessBehind: Output()
// returns once the stdout pipe closes, and the pipe is held by everything that
// inherited it, not just the helper. A helper that starts an agent -- gpg-agent,
// on demand, is the ordinary case -- therefore returns long after it exited.
//
// Reading ctx.Err() before err threw away a token that had been printed, read
// and exited cleanly for, and failed a run that had everything it needed.
func TestRunPasswordCommandKeepsTheTokenOfAHelperThatLeftAProcessBehind(t *testing.T) {
	command := helperCommand(t, "linger", "s3cret")

	start := time.Now()
	password, err := runPasswordCommand(context.Background(), command)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "s3cret", password)

	// And it did not sit there for the minute the lingering process lives.
	assert.Less(t, elapsed, 10*time.Second,
		"the wait delay bounds the pipe, not the process that inherited it")
}

// TestPasswordCommandFlagReachesCredentials drives the real flag set rather than calling GetCredentials directly.
// RunMark forwards the flag with a single cmd.String("password-command"), and a name matching no registered flag yields "" instead of an error.
// That would leave the setting silently inert, so the run has to stop at credential resolution, before any Confluence request.
func TestPasswordCommandFlagReachesCredentials(t *testing.T) {
	command := helperCommand(t, "fail", "")

	config := filepath.Join(t.TempDir(), "mark.toml")
	require.NoError(t, os.WriteFile(config, nil, 0o600))

	restore := log.Logger
	t.Cleanup(func() { log.Logger = restore })

	cmd := &cli.Command{
		Flags:  Flags,
		Before: CheckFlags,
		Action: RunMark,
	}

	err := cmd.Run(context.Background(), []string{
		"mark",
		"--config", config,
		"--username", "user",
		"--base-url", "https://confluence.example.invalid",
		"--password-command", command,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to read password from command",
		"--password-command must reach GetCredentials")
}

// TestPasswordCommandIsMaskedInTheConfigDump covers the debug dump that names every flag and its value.
// A command can name a token inline, so printing it in full would put a credential in the log the way printing the password would.
// The dump runs after credential resolution, so this uses --compile-only to reach it without a Confluence request.
func TestPasswordCommandIsMaskedInTheConfigDump(t *testing.T) {
	command := helperCommand(t, "print", "tok")

	dir := t.TempDir()
	config := filepath.Join(dir, "mark.toml")
	require.NoError(t, os.WriteFile(config, nil, 0o600))

	document := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(document, []byte("# Title\n\nBody.\n"), 0o600))

	captured := filepath.Join(dir, "stderr")
	file, err := os.Create(captured)
	require.NoError(t, err)

	originalStderr := os.Stderr
	originalLogger := log.Logger
	originalLevel := zerolog.GlobalLevel()
	os.Stderr = file

	t.Cleanup(func() {
		os.Stderr = originalStderr
		log.Logger = originalLogger
		zerolog.SetGlobalLevel(originalLevel)
	})

	cmd := &cli.Command{
		Flags:  Flags,
		Before: CheckFlags,
		Action: RunMark,
	}

	_ = cmd.Run(context.Background(), []string{
		"mark",
		"--config", config,
		"--log-level", "DEBUG",
		"--color", "never",
		"--compile-only",
		"--password-command", command,
		"--files", document,
	})

	require.NoError(t, file.Close())

	logged, err := os.ReadFile(captured)
	require.NoError(t, err)

	assert.Contains(t, string(logged), "password-command: ******")
	assert.NotContains(t, string(logged), command,
		"the command must not reach the log in full")
}
