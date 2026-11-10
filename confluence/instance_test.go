package confluence

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSameInstance: credentials for the configured base URL may go to the
// other URL only when it is on the same instance.
func TestSameInstance(t *testing.T) {
	const (
		site    = "https://example.atlassian.net/wiki"
		gateway = "https://api.atlassian.com/ex/confluence/0123-abcd/wiki"
		server  = "https://confluence.example.com/confluence"
	)

	for _, test := range []struct {
		name, configured, other string
	}{
		{"identical", site, site},
		{"trailing slash", site + "/", site},
		{"trailing slashes", site + "//", site},
		{"host in upper case", site, "https://EXAMPLE.Atlassian.NET/wiki"},
		{"scheme in upper case", site, "HTTPS://example.atlassian.net/wiki"},
		{"trailing dot", site, "https://example.atlassian.net./wiki"},
		{"explicit default port", site, "https://example.atlassian.net:443/wiki"},
		{"explicit default port, configured", "https://example.atlassian.net:443/wiki", site},
		{"explicit http default port", "http://confluence.local", "http://confluence.local:80"},
		{"leading zero in a port", "https://confluence.local:8443", "https://confluence.local:08443"},
		{"configured without a context path", "https://example.atlassian.net", site},
		{"other without a context path", site, "https://example.atlassian.net"},
		{"server context path", server, server},
		{"query and fragment ignored", site, site + "?x=1#y"},
		{"IP address", "http://127.0.0.1:8090", "http://127.0.0.1:8090/"},
		{"IPv6 address", "http://[::1]:8090", "http://[0:0::1]:8090"},
		{"underscore in a host", "https://confluence_01.corp", "https://Confluence_01.corp"},
		{"internationalised host and its punycode", "https://bücher.example/wiki", "https://xn--bcher-kva.example/wiki"},
		{"internationalised host in upper case", "https://BÜCHER.example", "https://bücher.example"},
		{"gateway configured, site page", gateway, site},
		{"gateway configured, jira.com site", gateway, "https://example.jira.com/wiki"},
		{"gateway configured, site without a path", gateway, "https://example.atlassian.net"},
		{"gateway, same cloud id", gateway, gateway},
		{"gateway without /wiki", "https://api.atlassian.com/ex/confluence/0123-abcd", gateway[:len(gateway)-len("/wiki")]},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.NoError(t, SameInstance(test.configured, test.other))
		})
	}

	for _, test := range []struct {
		name, configured, other, reason string
	}{
		{"another site", site, "https://other.atlassian.net/wiki", "host"},
		{"lookalike host suffix", site, "https://example.atlassian.net.evil.com/wiki", "host"},
		{"lookalike host prefix", site, "https://evilexample.atlassian.net/wiki", "host"},
		{"subdomain", "https://example.com", "https://a.example.com", "host"},
		{"parent domain", "https://a.example.com", "https://example.com", "host"},
		{"https downgraded to http", site, "http://example.atlassian.net/wiki", "scheme"},
		{"http upgraded to https", "http://confluence.local", "https://confluence.local", "scheme"},
		{"http on port 443", site, "http://example.atlassian.net:443/wiki", "scheme"},
		{"another port", site, "https://example.atlassian.net:8443/wiki", "port"},
		{"implicit and explicit other port", "http://confluence.local", "http://confluence.local:8080", "port"},
		{"path prefix", site, "https://example.atlassian.net/wikiX", "path"},
		{"path extended", site, "https://example.atlassian.net/wiki/x", "path"},
		{"path in another case", site, "https://example.atlassian.net/WIKI", "path"},
		{"another context path", server, "https://confluence.example.com/jira", "path"},
		{"encoded path", site, "https://example.atlassian.net/%77iki", "path"},
		{"internationalised lookalike", site, "https://exаmple.atlassian.net/wiki", "host"},
		{"site configured, gateway page", site, gateway, "host"},
		{"gateway, another cloud id", gateway, "https://api.atlassian.com/ex/confluence/9999/wiki", "path"},
		{"gateway configured, self-hosted page", gateway, server, "host"},
		{"gateway configured, lookalike Cloud host", gateway, "https://example.atlassian.net.evil.com/wiki", "host"},
		{"gateway configured, notatlassian.net", gateway, "https://example.notatlassian.net/wiki", "host"},
		{"gateway configured, site over http", gateway, "http://example.atlassian.net/wiki", "scheme"},
		{"gateway configured, site on another port", gateway, "https://example.atlassian.net:8443/wiki", "443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := SameInstance(test.configured, test.other)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrOtherInstance)

			var mismatch *InstanceMismatchError
			require.True(t, errors.As(err, &mismatch))
			assert.Equal(t, test.configured, mismatch.Configured)
			assert.Equal(t, test.other, mismatch.Other)
			assert.Contains(t, mismatch.Reason, test.reason)
		})
	}

	for _, test := range []struct {
		name, configured, other, says string
	}{
		{"user name before a lookalike host", site, "https://example.atlassian.net@evil.com/wiki", "user name"},
		{"user name on the configured host", site, "https://user:secret@example.atlassian.net/wiki", "user name"},
		{"configured with a user name", "https://user@example.atlassian.net/wiki", site, "user name"},
		{"backslash before an @", site, `https://evil.com\@example.atlassian.net/wiki`, "parse"},
		{"no scheme", site, "example.atlassian.net/wiki", "http or https"},
		{"protocol-relative", site, "//example.atlassian.net/wiki", "http or https"},
		{"another scheme", site, "ftp://example.atlassian.net/wiki", "http or https"},
		{"no host", site, "https:///wiki", "no host"},
		{"opaque", site, "https:example.atlassian.net", "no host"},
		{"port out of range", site, "https://example.atlassian.net:65536/wiki", "port"},
		{"port zero", site, "https://example.atlassian.net:0/wiki", "port"},
		{"dot-dot segment", site, "https://example.atlassian.net/wiki/../other", ".."},
		{"dot segment", site, "https://example.atlassian.net/./wiki", `"."`},
		{"percent-encoded dot-dot segment", site, "https://example.atlassian.net/wiki/%2e%2e/other", ".."},
		{"empty configured", "", site, "http or https"},
		{"empty other", site, "", "http or https"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := SameInstance(test.configured, test.other)
			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrOtherInstance)
			assert.Contains(t, err.Error(), test.says)
		})
	}
}

// TestSameInstanceKeepsPasswordsOutOfErrors: its errors are printed, so a
// password in either URL must not appear in them.
func TestSameInstanceKeepsPasswordsOutOfErrors(t *testing.T) {
	for _, pair := range [][2]string{
		{"https://user:s3cret@example.com/wiki", "https://example.com/wiki"},
		{"https://example.com/wiki", "https://user:s3cret@example.com/wiki"},
		{"https://user:s3cret@exa mple.com/wiki", "https://example.com/wiki"},
		{"https://example.com/wiki", "https://user:s3cret@example.com/wiki/../x"},
	} {
		err := SameInstance(pair[0], pair[1])
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "s3cret")
	}
}

// TestSameInstanceWantsABaseURL: other is a base URL. A page's own URL has a
// longer path than the context path, so against a configured base that has one
// it does not match; the base has to be taken from it first.
func TestSameInstanceWantsABaseURL(t *testing.T) {
	const page = "https://example.atlassian.net/wiki/spaces/DOCS/pages/1/Title"

	err := SameInstance("https://example.atlassian.net/wiki", page)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOtherInstance)
	assert.Contains(t, err.Error(), "path")

	assert.NoError(t, SameInstance("https://example.atlassian.net/wiki", "https://example.atlassian.net/wiki"),
		"the page's base URL, as export.ParsePageURL gives it, matches")
}
