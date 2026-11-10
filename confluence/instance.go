package confluence

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// gatewayHost is where a scoped API token reaches a Cloud site:
// https://api.atlassian.com/ex/confluence/<cloudId>/wiki.
const gatewayHost = "api.atlassian.com"

// ErrOtherInstance is what an *InstanceMismatchError matches with errors.Is:
// the two URLs given to SameInstance are on different Confluence instances.
var ErrOtherInstance = errors.New("not the configured Confluence instance")

// InstanceMismatchError is SameInstance's answer when both URLs are well formed
// but name different instances. Reason says which part differs.
type InstanceMismatchError struct {
	// Configured and Other are the URLs as SameInstance was given them.
	Configured string
	Other      string

	// Reason is the part that differs, such as "the host b.example.com is
	// not a.example.com".
	Reason string
}

func (e *InstanceMismatchError) Error() string {
	return fmt.Sprintf("%s is not on the Confluence instance at %s: %s", e.Other, e.Configured, e.Reason)
}

// Is makes errors.Is(err, ErrOtherInstance) true.
func (e *InstanceMismatchError) Is(target error) bool { return target == ErrOtherInstance }

// SameInstance reports whether other, the base URL of a page -- as
// export.ParsePageURL gives it in PageRef.BaseURL -- is on the Confluence
// instance at configured, the base URL credentials are meant for. It answers
// nil only when it is; any error means the credentials for configured must not
// be sent to other.
//
// other must be a base URL, not a page's own URL: the path of
// https://example.atlassian.net/wiki/spaces/DOCS/pages/1 is not /wiki, so it
// fails against a configured base of https://example.atlassian.net/wiki. Take
// the base from the page URL first, with export.ParsePageURL.
//
// An error that is an *InstanceMismatchError (errors.Is matches
// ErrOtherInstance) says what differs. Any other error says one of the URLs is
// not one SameInstance compares: unparsable, not http or https, without a
// host, with an invalid port, with a user name or password in it, or with a
// "." or ".." path segment.
//
// The two are the same instance when:
//
//   - the schemes are the same: http is not https, so a downgrade is refused;
//   - the hosts are the same, compared in their ASCII (punycode) form, without
//     regard to case or a trailing dot;
//   - the ports are the same, a port left out being the scheme's default, so
//     https://host and https://host:443 match;
//   - the paths -- the context path an instance is served under, such as
//     /wiki -- are the same, compared whole after trailing slashes, so /wiki
//     does not match /wikiX or /wiki/x. Paths are compared as escaped, so the
//     same path percent-encoded differently, /wiki against /%77iki, does not
//     match. Either path may be left out: a base URL of
//     https://example.atlassian.net matches https://example.atlassian.net/wiki
//     and the other way about. That is no wider than the credentials reach
//     already: both name the same scheme, host and port, so the same server.
//
// The api.atlassian.com gateway, through which a scoped API token reaches a
// Cloud site, is the exception: configured on the gateway, any Cloud host
// (IsCloudHost) over https on port 443 matches, since a page URL a browser
// shows is on the site, and credentials for the gateway still go to the
// gateway. The reverse does not hold: with a site configured, a URL on the
// gateway names a site by its cloud id, which no URL ties to the configured
// host. A gateway URL against another gateway URL is compared as any other
// host is, so /ex/confluence/A does not match /ex/confluence/B.
//
// A nil answer says other's page ids may be asked of configured: send the
// requests, and the credentials, to configured. With the gateway configured,
// other is a different host, and nil does not make it a place to send them.
//
// A user name or password is refused in either argument, the configured one
// included. API.AttachmentURL is less strict about the configured base URL: it
// ignores a user name there, since whoever configured it gave it, and refuses
// one only in the download link.
func SameInstance(configured, other string) error {
	have, err := parseInstanceURL("base URL", configured)
	if err != nil {
		return err
	}
	want, err := parseInstanceURL("URL", other)
	if err != nil {
		return err
	}

	if reason := instanceDifference(have, want); reason != "" {
		return &InstanceMismatchError{Configured: configured, Other: other, Reason: reason}
	}

	return nil
}

// origin is the server a URL reaches.
type origin struct {
	scheme string
	host   string // ASCII, lowercase, without a trailing dot
	port   int
}

// instance is a URL reduced to what SameInstance compares.
type instance struct {
	origin
	path string // escaped, without trailing slashes
}

// instanceDifference says how want is not on the instance have, or "" when it
// is.
func instanceDifference(have, want instance) string {
	if have.scheme != want.scheme {
		return fmt.Sprintf("the scheme %s is not %s", want.scheme, have.scheme)
	}

	if have.host == gatewayHost && want.host != gatewayHost && IsCloudHost(want.host) {
		if have.scheme != "https" || have.port != 443 || want.port != 443 {
			return "the api.atlassian.com gateway fronts Cloud sites over https on port 443 only"
		}
		return ""
	}

	if have.host != want.host {
		return fmt.Sprintf("the host %s is not %s", want.host, have.host)
	}

	if have.port != want.port {
		return fmt.Sprintf("the port %d is not %d", want.port, have.port)
	}

	if have.path != "" && want.path != "" && have.path != want.path {
		return fmt.Sprintf("the path %s is not %s", want.path, have.path)
	}

	return ""
}

// parseInstanceURL reads raw as SameInstance compares it; what names the URL
// in an error.
func parseInstanceURL(what, raw string) (instance, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// url.Error quotes the whole URL, password included.
		var parseErr *url.Error
		if errors.As(err, &parseErr) {
			err = parseErr.Err
		}
		return instance{}, fmt.Errorf("unable to parse %s: %w", what, err)
	}

	// Redacted, since these errors end up in logs.
	shown := parsed.Redacted()

	server, err := originOf(parsed)
	if err != nil {
		return instance{}, fmt.Errorf("%s %q %w", what, shown, err)
	}

	path := strings.TrimRight(parsed.EscapedPath(), "/")
	// Decoded, since a server reads %2e%2e as ".." too.
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return instance{}, fmt.Errorf("%s %q has a %q segment in its path", what, shown, segment)
		}
	}

	return instance{origin: server, path: path}, nil
}

// originOf is the scheme, host and port of u, the server a request to it
// reaches, with the host in its ASCII form. Its error completes a sentence
// that names the URL.
func originOf(u *url.URL) (origin, error) {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return origin{}, errors.New("is not an http or https URL")
	}

	if u.User != nil {
		return origin{}, errors.New("carries a user name or password")
	}

	host, err := asciiHost(u.Hostname())
	if err != nil {
		return origin{}, err
	}

	port := 443
	if scheme == "http" {
		port = 80
	}
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return origin{}, fmt.Errorf("has an invalid port %q", p)
		}
	}

	return origin{scheme: scheme, host: host, port: port}, nil
}

// asciiHost is host as DNS looks it up: lowercase, without a trailing dot, and
// an internationalised name in its punycode form. An IP address is written
// the one way netip writes it, so ::1 and 0:0::1 are one host. Only a name
// with non-ASCII characters goes through IDNA, whose lookup rules refuse the
// underscores an ASCII host may carry.
func asciiHost(host string) (string, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", errors.New("names no host")
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.String(), nil
	}

	if !hasNonASCII(host) {
		return strings.ToLower(host), nil
	}

	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", fmt.Errorf("has a host that is not a valid domain name: %w", err)
	}

	return strings.TrimSuffix(ascii, "."), nil
}

func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return true
		}
	}
	return false
}
