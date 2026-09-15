package browser

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cookies(name string) []*http.Cookie {
	return []*http.Cookie{{Name: name, Value: "v"}}
}

// TestWaitForSessionReturnsOnFirstSuccess covers the ordinary login: the user
// takes a few seconds, and the poll that follows their success wins.
func TestWaitForSessionReturnsOnFirstSuccess(t *testing.T) {
	calls := 0
	src := func() ([]*http.Cookie, error) {
		calls++
		return cookies("JSESSIONID"), nil
	}
	probe := func([]*http.Cookie) bool { return calls >= 3 }

	got, err := waitForSession(context.Background(), src, probe, time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, "JSESSIONID", got[0].Name)
	assert.Equal(t, 3, calls)
}

// TestWaitForSessionTimesOut covers a login that never completes. The error
// has to be distinguishable so Login can attach the timeout it used.
func TestWaitForSessionTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	src := func() ([]*http.Cookie, error) { return cookies("JSESSIONID"), nil }
	probe := func([]*http.Cookie) bool { return false }

	_, err := waitForSession(ctx, src, probe, time.Millisecond)

	assert.ErrorIs(t, err, ErrTimeout)
}

// TestWaitForSessionSurvivesReadErrors covers Chrome answering badly for a
// poll or two -- a page mid-navigation. It is not fatal; keep polling.
func TestWaitForSessionSurvivesReadErrors(t *testing.T) {
	calls := 0
	src := func() ([]*http.Cookie, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("browser busy")
		}
		return cookies("JSESSIONID"), nil
	}
	probe := func([]*http.Cookie) bool { return true }

	got, err := waitForSession(context.Background(), src, probe, time.Millisecond)
	require.NoError(t, err)

	assert.Len(t, got, 1)
}

// TestWaitForSessionIgnoresEmptyCookies covers the first moments after Chrome
// opens, when the page has set nothing yet: no probe should be sent.
func TestWaitForSessionIgnoresEmptyCookies(t *testing.T) {
	calls, probes := 0, 0
	src := func() ([]*http.Cookie, error) {
		calls++
		if calls < 3 {
			return nil, nil
		}
		return cookies("JSESSIONID"), nil
	}
	probe := func([]*http.Cookie) bool {
		probes++
		return true
	}

	_, err := waitForSession(context.Background(), src, probe, time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, 1, probes)
}

// TestWaitForSessionHonoursCancellation covers ctrl-c: return promptly rather
// than after the poll interval, and do not spin.
func TestWaitForSessionHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := func() ([]*http.Cookie, error) { return cookies("JSESSIONID"), nil }
	probe := func([]*http.Cookie) bool { return false }

	done := make(chan struct{})
	go func() {
		_, _ = waitForSession(ctx, src, probe, time.Hour)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForSession did not return on a cancelled context")
	}
}

// TestConvertCookiesCarriesFieldsAcross covers the ordinary case: nothing
// gets lost or transposed on the way from a CDP cookie to an http.Cookie.
func TestConvertCookiesCarriesFieldsAcross(t *testing.T) {
	got := convertCookies([]*network.Cookie{{
		Name:     "JSESSIONID",
		Value:    "abc123",
		Domain:   "confluence.example.com",
		Path:     "/wiki",
		Secure:   true,
		HTTPOnly: true,
		Expires:  -1,
	}})

	require.Len(t, got, 1)
	cookie := got[0]
	assert.Equal(t, "JSESSIONID", cookie.Name)
	assert.Equal(t, "abc123", cookie.Value)
	assert.Equal(t, "confluence.example.com", cookie.Domain)
	assert.Equal(t, "/wiki", cookie.Path)
	assert.True(t, cookie.Secure)
	assert.True(t, cookie.HttpOnly)
}

// TestConvertCookiesExpiryFromEpochSeconds covers a cookie with a real
// expiry: Chrome reports it as epoch seconds, and that has to survive the
// conversion as an actual time.
func TestConvertCookiesExpiryFromEpochSeconds(t *testing.T) {
	got := convertCookies([]*network.Cookie{{
		Name:    "remember",
		Value:   "v",
		Expires: 1893456000, // 2030-01-01T00:00:00Z
	}})

	require.Len(t, got, 1)
	assert.Equal(t, int64(1893456000), got[0].Expires.Unix())
}

// TestConvertCookiesSessionSentinelStaysZero covers the -1 CDP uses for a
// session cookie -- what a login usually produces. Converting -1 with
// time.Unix would produce a moment in 1969, which is not "no expiry": it is
// a bogus date in the past that would make LoadCookies discard the cookie as
// stale the moment it was cached.
func TestConvertCookiesSessionSentinelStaysZero(t *testing.T) {
	got := convertCookies([]*network.Cookie{{
		Name:    "JSESSIONID",
		Value:   "abc123",
		Expires: -1,
	}})

	require.Len(t, got, 1)
	assert.True(t, got[0].Expires.IsZero())
}

// TestDomainMatchesExact covers a host-only cookie: the Domain attribute is
// the host itself, with no leading dot.
func TestDomainMatchesExact(t *testing.T) {
	assert.True(t, domainMatches("confluence.example.com", "confluence.example.com"))
}

// TestDomainMatchesLeadingDotParentDomain covers the ordinary case a Domain
// attribute is set for: ".example.com" is meant to cover subdomains, and
// Confluence lives on one of them.
func TestDomainMatchesLeadingDotParentDomain(t *testing.T) {
	assert.True(t, domainMatches(".example.com", "confluence.example.com"))
}

// TestDomainMatchesDotlessParentDomain covers the same parent-domain cookie
// without the leading dot -- some servers omit it, and it means the same
// thing.
func TestDomainMatchesDotlessParentDomain(t *testing.T) {
	assert.True(t, domainMatches("example.com", "confluence.example.com"))
}

// TestDomainMatchesRejectsUnrelatedHost is the whole point: an identity
// provider's own domain must not match the Confluence host.
func TestDomainMatchesRejectsUnrelatedHost(t *testing.T) {
	assert.False(t, domainMatches("idp.okta.com", "confluence.example.com"))
}

// TestDomainMatchesRejectsSuffixThatIsNotADomainBoundary guards against a
// naive strings.HasSuffix check, which "evilconfluence.example.com" would
// pass against a Domain of "confluence.example.com".
func TestDomainMatchesRejectsSuffixThatIsNotADomainBoundary(t *testing.T) {
	assert.False(t, domainMatches("confluence.example.com", "evilconfluence.example.com"))
}

// TestDomainMatchesEmptyDomainDropped covers a cookie with no Domain
// information to match against: the safe reading is to drop it rather than
// assume it belongs to the Confluence host.
func TestDomainMatchesEmptyDomainDropped(t *testing.T) {
	assert.False(t, domainMatches("", "confluence.example.com"))
}

// TestFilterCookiesForHostDropsTheIdentityProvider is finding 2 end to end at
// the filtering layer: cookies from both the Confluence host and the IdP
// come back from the browser, and only the Confluence ones should leave.
func TestFilterCookiesForHostDropsTheIdentityProvider(t *testing.T) {
	cookies := []*http.Cookie{
		{Name: "JSESSIONID", Value: "abc", Domain: "confluence.example.com"},
		{Name: "sid", Value: "def", Domain: ".example.com"},
		{Name: "okta-session", Value: "ghi", Domain: "idp.okta.com"},
	}

	got := filterCookiesForHost(cookies, "confluence.example.com")

	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.ElementsMatch(t, []string{"JSESSIONID", "sid"}, names)
}

// TestHostForFilterStripsPort covers a baseURL with an explicit port, which a
// cookie's Domain attribute never carries.
func TestHostForFilterStripsPort(t *testing.T) {
	host, err := hostForFilter("https://confluence.example.com:8443/wiki")
	require.NoError(t, err)
	assert.Equal(t, "confluence.example.com", host)
}
