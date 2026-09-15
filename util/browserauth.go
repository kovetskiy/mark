package util

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"

	"github.com/rs/zerolog/log"

	"github.com/kovetskiy/mark/v16/browser"
	"github.com/kovetskiy/mark/v16/confluence"
)

// loginFunc is browser.Login, as a value so tests can stand in for it. Nothing
// else in this package may open a browser.
type loginFunc func(ctx context.Context, baseURL string, opts browser.Options) ([]*http.Cookie, error)

// BrowserAuth is what the resolution policy needs to know about the run.
type BrowserAuth struct {
	// CompileOnly means no Confluence call will be made, so no credential is
	// needed and no browser should open.
	//
	// DryRun is not the same: it still calls page.ResolvePage /
	// api.GetPageByID to report what would happen, so it needs a session like
	// any other run -- an unauthenticated dry run against an SSO instance
	// would report every page as new.
	CompileOnly bool
	DryRun      bool

	// CI means no one is watching a browser window.
	CI bool

	InsecureSkipTLSVerify bool

	// authenticates decides whether a cached cookie still works. Unset in
	// production, where it is browser.Authenticates against a real instance;
	// set in tests, which have no instance to ask.
	authenticates func([]*http.Cookie) bool
}

// ResolveBrowserAuth returns the session cookies for a --login run, opening a
// browser only when it has to.
func ResolveBrowserAuth(ctx context.Context, baseURL string, cfg BrowserAuth) ([]*http.Cookie, error) {
	return resolveBrowserAuth(ctx, baseURL, cfg, browser.Login)
}

func resolveBrowserAuth(
	ctx context.Context,
	baseURL string,
	cfg BrowserAuth,
	login loginFunc,
) ([]*http.Cookie, error) {
	// Only a compile never talks to Confluence at all. A dry run does --
	// resolving where each page would go -- so it needs a session just like a
	// real publish.
	if cfg.CompileOnly {
		log.Debug().Msg("--login ignored: this run does not contact Confluence")
		return nil, nil
	}

	client := confluence.NewHTTPClient(cfg.InsecureSkipTLSVerify)

	authenticates := cfg.authenticates
	if authenticates == nil {
		authenticates = func(cookies []*http.Cookie) bool {
			return browser.Authenticates(ctx, client, baseURL, cookies)
		}
	}

	if cached := browser.LoadCookies(baseURL); len(cached) > 0 {
		if authenticates(cached) {
			log.Debug().Msg("reusing the cached Confluence session")
			return cached, nil
		}

		log.Info().Msg("the cached Confluence session is no longer valid; logging in again")
	}

	// Checked only once a login is actually needed, so that a cached session
	// still publishes from a machine with no display.
	if err := canOpenBrowser(cfg.CI); err != nil {
		return nil, err
	}

	cookies, err := login(ctx, baseURL, browser.Options{Client: client})
	if err != nil {
		return nil, err
	}

	if err := browser.SaveCookies(baseURL, cookies); err != nil {
		// The run has its session; only the next run pays for this.
		log.Warn().Err(err).Msg("unable to cache the Confluence session; the next run will log in again")
	}

	return cookies, nil
}

// canOpenBrowser reports whether a login window could be seen by anyone.
//
// Detected before launching Chrome rather than after it fails, so the two
// cases that are certain -- a CI run, and a Linux box with no display -- fail
// in milliseconds with the right advice instead of after a browser timeout
// that reads like a Chrome fault.
func canOpenBrowser(ci bool) error {
	unusable := errors.New(
		"--login needs a browser mark can open a window with; " +
			"in CI use --password with a personal access token instead")

	if ci {
		return unusable
	}

	// macOS and Windows always have a display. On Linux, neither variable
	// being set means no session a window could appear in.
	if runtime.GOOS == "linux" &&
		os.Getenv("DISPLAY") == "" &&
		os.Getenv("WAYLAND_DISPLAY") == "" {
		return unusable
	}

	return nil
}
