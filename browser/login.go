package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/rs/zerolog/log"

	"github.com/kovetskiy/mark/v16/chrome"
)

// ErrTimeout is returned when no session authenticated before the deadline.
// Login wraps it with the timeout it was given; callers match with errors.Is.
var ErrTimeout = errors.New("no Confluence session appeared before the deadline")

const (
	defaultInterval = time.Second

	// defaultTimeout is generous on purpose. What is being waited on is a
	// human completing an SSO login, which can mean a password manager, a
	// push notification to a phone, and an MFA prompt.
	defaultTimeout = 5 * time.Minute
)

// Options configure a login. The zero value is usable: Client falls back to
// http.DefaultClient, and both durations to their defaults.
type Options struct {
	// Client probes the API. Passing the client mark itself uses means the
	// probe inherits its TLS, proxy and retry configuration.
	Client *http.Client

	Interval time.Duration
	Timeout  time.Duration
}

func (o Options) withDefaults() Options {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}

	if o.Interval <= 0 {
		o.Interval = defaultInterval
	}

	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}

	return o
}

// Login opens a visible browser at baseURL and waits until the session
// authenticates, returning the cookies that did it.
func Login(ctx context.Context, baseURL string, opts Options) ([]*http.Cookie, error) {
	opts = opts.withDefaults()

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:], chrome.HeadfulAllocatorOptions()...)...)
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	log.Info().Msgf("opening a browser at %s; log in to continue", baseURL)

	if err := chromedp.Run(browserCtx, chromedp.Navigate(baseURL)); err != nil {
		return nil, fmt.Errorf(
			"--login needs a browser mark can open a window with; "+
				"in CI use --password with a personal access token instead: %w", err)
	}

	cookies, err := waitForSession(ctx,
		func() ([]*http.Cookie, error) { return readCookies(browserCtx, baseURL) },
		func(cookies []*http.Cookie) bool {
			return Authenticates(ctx, opts.Client, baseURL, cookies)
		},
		opts.Interval)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return nil, fmt.Errorf(
				"timed out after %s waiting for a Confluence session; "+
					"if you completed the login and this still failed, "+
					"the account may lack REST API access: %w", opts.Timeout, err)
		}

		return nil, err
	}

	log.Info().Msg("Confluence session established")

	return cookies, nil
}

// waitForSession polls src until the cookies it returns authenticate, or ctx
// is done. Chrome is one implementation of src.
//
// Split out from Login so the policy -- how often to look, what counts as
// success, what a read error means -- is testable without a browser.
func waitForSession(
	ctx context.Context,
	src func() ([]*http.Cookie, error),
	probe func([]*http.Cookie) bool,
	interval time.Duration,
) ([]*http.Cookie, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		cookies, err := src()
		switch {
		case err != nil:
			// The browser is navigating, or between pages. Not fatal: the
			// next poll is a second away.
			log.Debug().Err(err).Msg("unable to read cookies from the browser")
		case len(cookies) == 0:
			// Nothing set yet. Probing with no cookies would ask the server
			// whether anonymous access works, which is a different question.
		case probe(cookies):
			return cookies, nil
		}

		select {
		case <-ctx.Done():
			return nil, ErrTimeout
		case <-ticker.C:
		}
	}
}

// readCookies takes the browser's current cookies for every host it has
// visited -- an SSO login redirects through the identity provider and back --
// and returns only those scoped to baseURL's host. Without this, the identity
// provider's own session cookies would be cached and probed right alongside
// Confluence's: cookiejar drops them for the real API calls since they never
// match, so nothing is gained, but the whole SSO identity would still be
// written to disk and put on the wire toward the Confluence host.
func readCookies(ctx context.Context, baseURL string) ([]*http.Cookie, error) {
	host, err := hostForFilter(baseURL)
	if err != nil {
		return nil, err
	}

	var cookies []*http.Cookie

	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}

		cookies = filterCookiesForHost(convertCookies(got), host)

		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("unable to read cookies from the browser: %w", err)
	}

	return cookies, nil
}

// hostForFilter extracts the host cookies must match, without the port: a
// cookie's Domain attribute never carries one.
func hostForFilter(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse %q as url: %w", baseURL, err)
	}

	if parsed.Hostname() == "" {
		return "", fmt.Errorf("no host in %q", baseURL)
	}

	return parsed.Hostname(), nil
}

// convertCookies turns Chrome's cookie representation into the standard
// library's. Split out from readCookies so the conversion -- in particular
// the session-cookie sentinel below -- is testable without a browser.
func convertCookies(got []*network.Cookie) []*http.Cookie {
	cookies := make([]*http.Cookie, 0, len(got))

	for _, cookie := range got {
		// Secure and HttpOnly are carried across as Chrome reported them.
		// They describe the cookie the instance issued; overriding them here
		// would misrepresent it, and SameSite is a server directive that says
		// nothing about a cookie being sent on a request.
		converted := &http.Cookie{ //nolint:gosec // G124: reconstructing a captured cookie, not issuing one
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HTTPOnly,
		}

		// Chrome reports expiry as epoch seconds, and -1 for a session
		// cookie -- which is what a login usually produces. Leaving
		// converted.Expires at its zero value rather than converting -1
		// keeps it a session cookie instead of turning it into a moment in
		// 1969.
		if cookie.Expires > 0 {
			converted.Expires = time.Unix(int64(cookie.Expires), 0)
		}

		cookies = append(cookies, converted)
	}

	return cookies
}

// filterCookiesForHost keeps only the cookies whose Domain matches host,
// dropping everything the identity provider set along the way.
func filterCookiesForHost(cookies []*http.Cookie, host string) []*http.Cookie {
	var kept []*http.Cookie

	for _, cookie := range cookies {
		if domainMatches(cookie.Domain, host) {
			kept = append(kept, cookie)
		}
	}

	return kept
}

// domainMatches reports whether a cookie's Domain attribute covers host.
//
// A Domain may carry a leading dot -- ".example.com" -- which the dot and
// dotless forms mean the same thing for: the cookie also covers subdomains,
// so "confluence.example.com" matches both ".example.com" and "example.com".
//
// An empty Domain carries no information to match against. Chrome's CDP
// normally fills it in even for a host-only cookie, so this should not arise
// in practice; when it does, the safe reading is to drop the cookie rather
// than guess it belongs to the Confluence host.
func domainMatches(domain, host string) bool {
	if domain == "" {
		return false
	}

	domain = strings.TrimPrefix(domain, ".")

	return host == domain || strings.HasSuffix(host, "."+domain)
}
