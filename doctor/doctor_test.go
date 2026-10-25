package doctor

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) *confluencetest.Server {
	t.Helper()
	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Docs Home", "page", "")
	server.SetHomepage("DOCS", home.ID)
	return server
}

func doctor(t *testing.T, server *confluencetest.Server, base string, spaces ...string) (string, error) {
	t.Helper()
	if base == "" {
		base = server.URL
	}
	var out bytes.Buffer
	api := confluence.NewAPI(base, "user", "token", false)
	err := Run(api, base, "user", spaces, &out)
	return out.String(), err
}

func TestDoctorAllWell(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dataCenter bool
		gateway    bool
		platform   string
	}{
		{"data center", true, false, "Confluence Server or Data Center"},
		{"cloud", false, false, "Confluence Cloud"},
		{"gateway", false, true, "Confluence Cloud"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newServer(t)
			if tc.dataCenter {
				server.SetDataCenter()
			}
			base := server.URL
			if tc.gateway {
				base += "/ex/confluence/cloud-id"
			}

			out, err := doctor(t, server, base, "DOCS")
			require.NoError(t, err, out)
			assert.Contains(t, out, "[ok] login: logged in as Current User")
			assert.Contains(t, out, "[ok] platform: "+tc.platform)
			assert.Contains(t, out, "[ok] space DOCS: readable")
			assert.Contains(t, out, `[ok] space DOCS: home page "Docs Home"`)
			assert.Contains(t, out, "[ok] space DOCS: can create pages, upload attachments")
			assert.NotContains(t, out, "[warn]")
			assert.NotContains(t, out, "[fail]")
		})
	}
}

func TestDoctorMissingPermissions(t *testing.T) {
	server := newServer(t)
	server.Deny("DOCS", "delete", "page")

	out, err := doctor(t, server, "", "DOCS")
	require.NoError(t, err, "what only some runs need is a warning")
	assert.Contains(t, out, "[warn] space DOCS: cannot delete pages (--on-orphan delete)")

	server.Deny("DOCS", "create", "attachment")
	out, err = doctor(t, server, "", "DOCS")
	require.Error(t, err)
	assert.Contains(t, out, "[fail] space DOCS: cannot upload attachments, delete pages (--on-orphan delete)")
}

func TestDoctorUnknownSpace(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprint("gateway=", gateway), func(t *testing.T) {
			server := newServer(t)
			base := server.URL
			if gateway {
				base += "/ex/confluence/cloud-id"
			}

			out, err := doctor(t, server, base, "DOCS", "NOPE")
			require.EqualError(t, err, "1 of 6 checks failed")
			assert.Contains(t, out, "[fail] space NOPE: not found")
			assert.Contains(t, out, "[ok] space DOCS: readable", "the other space is still checked")
		})
	}
}

func TestDoctorSpaceWithoutHomePage(t *testing.T) {
	server := newServer(t)
	server.AddSpace("BARE")

	out, err := doctor(t, server, "", "BARE")
	require.NoError(t, err, "publishing does not need a home page")
	assert.Contains(t, out, "[warn] space BARE: no home page")
	assert.Contains(t, out, "at the top of the space")
}

func TestDoctorWithoutOperations(t *testing.T) {
	server := newServer(t)
	server.HideOperations()

	out, err := doctor(t, server, "", "DOCS")
	require.NoError(t, err)
	assert.Contains(t, out, "[warn] space DOCS: this Confluence does not say what the account may do in a space")
}

func TestDoctorNoSpace(t *testing.T) {
	out, err := doctor(t, newServer(t), "")
	require.NoError(t, err)
	assert.Contains(t, out, "[warn] space: no space to check")
}

func TestDoctorLoginRefused(t *testing.T) {
	server := newServer(t)
	server.SetFail(func(*http.Request) (int, string, bool) {
		return http.StatusUnauthorized, `{"message":"unauthorized"}`, true
	})

	out, err := doctor(t, server, "", "DOCS")
	require.EqualError(t, err, "1 of 1 checks failed")
	assert.Contains(t, out, "[fail] login:")
	assert.Contains(t, out, "personal access token goes in --password")
	assert.NotContains(t, out, "space DOCS", "nothing more can be checked")
}

var dnsError = net.DNSError{Err: "no such host", Name: "nope.example.com", IsNotFound: true}

func TestLoginFailureHints(t *testing.T) {
	unauthorized := fmt.Errorf("the Confluence API returned unexpected status: %w for GET x", confluence.ErrUnauthorized)

	for _, tc := range []struct {
		name, host, username string
		err                  error
		hint                 string
	}{
		{"cloud token without email", "example.atlassian.net", "", unauthorized, "a token without a username"},
		{"cloud with a username", "example.atlassian.net", "jane", unauthorized, "not a display name"},
		{"gateway", "api.atlassian.com", "jane@example.com", unauthorized, "scoped API token"},
		{"data center", "confluence.example.com", "jane", unauthorized, "personal access token"},
		{"wrong path", "confluence.example.com", "jane", fmt.Errorf("x: %w", confluence.ErrNotFound), "context path"},
		{"no such host", "nope.example.com", "", fmt.Errorf("x: %w", &dnsError), "does not resolve"},
		{"something else", "confluence.example.com", "", errors.New("boom"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := loginFailure(tc.err, tc.host, tc.username)
			assert.Equal(t, Fail, check.Status)
			if tc.hint == "" {
				assert.Empty(t, check.Hint)
			} else {
				assert.Contains(t, check.Hint, tc.hint)
			}
		})
	}
}
