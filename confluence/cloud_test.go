package confluence

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIdentifyCloud: Cloud describes a user by an Atlassian accountId, Server
// and Data Center by a username and userKey with no accountId, and a user with
// neither identifies nothing.
func TestIdentifyCloud(t *testing.T) {
	for name, tc := range map[string]struct {
		user    User
		cloud   bool
		wantErr bool
	}{
		"accountId":            {User{AccountID: "712020:6a85", PublicName: "Jane Doe"}, true, false},
		"username and userKey": {User{Username: "jdoe", UserKey: "4028", DisplayName: "Jane Doe"}, false, false},
		"username only":        {User{Username: "jdoe"}, false, false},
		"userKey only":         {User{UserKey: "4028"}, false, false},
		"neither":              {User{DisplayName: "Anonymous"}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			cloud, err := identifyCloud(&tc.user)
			assert.Equal(t, tc.cloud, cloud)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCloudKeepsTheErrorOfAFailedLookup: IsCloud answers "not Cloud" for a
// target it could not identify, and cloud keeps the reason for a caller that
// must not guess.
func TestCloudKeepsTheErrorOfAFailedLookup(t *testing.T) {
	server := confluencetest.New(t)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/rest/api/user/current" {
			return http.StatusUnauthorized, `{"message":"bad credentials"}`, true
		}
		return 0, "", false
	})
	api := NewAPI(server.URL, "user", "token", false)

	cloud, err := api.cloud()
	assert.False(t, cloud)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to identify the Confluence platform")
}

// TestGetCurrentUserIsAskedOnce: the platform probe and an edit lock both need
// the current user, and it does not change during a run. A failed lookup is
// not kept, so the next call asks again.
func TestGetCurrentUserIsAskedOnce(t *testing.T) {
	server := confluencetest.New(t)
	var refuse atomic.Bool
	refuse.Store(true)
	server.SetFail(func(r *http.Request) (int, string, bool) {
		if refuse.Load() && r.URL.Path == "/rest/api/user/current" {
			return http.StatusServiceUnavailable, `{"message":"down"}`, true
		}
		return 0, "", false
	})
	api := NewAPI(server.URL, "user", "token", false)

	_, err := api.GetCurrentUser()
	require.Error(t, err)

	refuse.Store(false)
	before := server.CountRequests("GET", "/rest/api/user/current")

	first, err := api.GetCurrentUser()
	require.NoError(t, err)
	second, err := api.GetCurrentUser()
	require.NoError(t, err)

	assert.Equal(t, before+1, server.CountRequests("GET", "/rest/api/user/current"),
		"a failure is asked again, a success is reused")
	assert.Equal(t, first, second)
	assert.NotSame(t, first, second, "each caller gets its own copy")
}
