package browser

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthenticatesAgainstRealAPI uses the fake Confluence, which serves
// /rest/api/user/current, so the probe is exercised against the same shape of
// response a real instance returns.
func TestAuthenticatesAgainstRealAPI(t *testing.T) {
	server := confluencetest.New(t)

	ok := Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}})

	assert.True(t, ok)
}

func TestAuthenticatesFalseOn401(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusUnauthorized, `{"message":"no"}`, true
	})

	ok := Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}})

	assert.False(t, ok)
}

// TestAuthenticatesFalseOn500 covers a broken server. It is not a failed
// login, but it is not a working cookie either, so the answer is still no.
func TestAuthenticatesFalseOn500(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		return http.StatusInternalServerError, `{"message":"boom"}`, true
	})

	ok := Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}})

	assert.False(t, ok)
}

// TestAuthenticatesFalseForAnAnonymousUser covers an instance with anonymous
// access, which answers a caller that has not logged in with 200 and an
// anonymous user. The browser holds a cookie long before the user has logged
// in, so taking that 200 at its word would end the login on the first poll.
func TestAuthenticatesFalseForAnAnonymousUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"anonymous","displayName":"Anonymous"}`))
		}))
	t.Cleanup(server.Close)

	ok := Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "not-logged-in-yet"}})

	assert.False(t, ok)
}

// TestAuthenticatesFalseForALoginPage covers an SSO proxy that redirects an
// unauthenticated API request to its login page: the client follows the
// redirect, and the 200 that comes back is HTML, not a user.
func TestAuthenticatesFalseForALoginPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/login" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><title>Sign in</title>`))
		}))
	t.Cleanup(server.Close)

	ok := Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "not-logged-in-yet"}})

	assert.False(t, ok)
}

// TestAuthenticatesSendsTheCookies is the point of the whole function: the
// cookies under test have to reach the server.
func TestAuthenticatesSendsTheCookies(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Cookie")
			w.WriteHeader(http.StatusOK)
		}))
	t.Cleanup(server.Close)

	Authenticates(context.Background(), server.Client(), server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "abc"}})

	assert.Equal(t, "JSESSIONID=abc", got)
}

// TestAuthenticatesFalseOnTransportError covers an unreachable instance: no
// panic, no retry storm, just no.
func TestAuthenticatesFalseOnTransportError(t *testing.T) {
	ok := Authenticates(context.Background(), http.DefaultClient,
		"http://127.0.0.1:1", []*http.Cookie{{Name: "a", Value: "b"}})

	assert.False(t, ok)
}

// TestAuthenticatesDoesNotAccumulateCookiesAcrossPolls guards a login that
// times out even though the user demonstrably logged in: an anonymous
// probe's Set-Cookie response lands in the client's jar, and http.Client.send
// appends jar cookies to whatever AddCookie already wrote. A jar shared
// across every poll would then let a second probe carry a stale cookie
// alongside the fresh one it was actually given.
func TestAuthenticatesDoesNotAccumulateCookiesAcrossPolls(t *testing.T) {
	var gotCookieHeaders []string

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotCookieHeaders = append(gotCookieHeaders, r.Header.Get("Cookie"))
			http.SetCookie(w, &http.Cookie{Name: "leftover", Value: "from-anonymous-probe"})
			w.WriteHeader(http.StatusUnauthorized)
		}))
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	// First poll: no session cookie yet. The server's Set-Cookie response
	// lands in the shared jar.
	Authenticates(context.Background(), client, server.URL, nil)

	// Second poll: the user has now logged in, and this is the only cookie
	// that should be sent.
	Authenticates(context.Background(), client, server.URL,
		[]*http.Cookie{{Name: "JSESSIONID", Value: "real-session"}})

	require.Len(t, gotCookieHeaders, 2)
	assert.Equal(t, "JSESSIONID=real-session", gotCookieHeaders[1],
		"the second probe must not carry the jar's leftover cookie from the first response")
}
