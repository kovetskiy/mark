package util

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataListings(t *testing.T) {
	testCases := []struct {
		name, resource   string
		selectors        []string
		path, body, want string
	}{
		{"site labels", "labels", nil, "/api/v2/labels", `{"results":[{"id":"12","name":"alpha","prefix":"global"}]}`, `[{"id":"12","name":"alpha","prefix":"global"}]`},
		{"space content labels", "labels", []string{"--space", "RD"}, "/api/v2/spaces/55/content/labels", `{"results":[]}`, `[]`},
		{"space labels", "labels", []string{"--space-id", "55"}, "/api/v2/spaces/55/labels", `{"results":[]}`, `[]`},
		{"page labels", "labels", []string{"--page-id", "42"}, "/api/v2/pages/42/labels", `{"results":[{"id":12,"name":"alpha","prefix":"global"}]}`, `[{"id":"12","name":"alpha","prefix":"global"}]`},
		{"blog labels", "labels", []string{"--blogpost-id", "42"}, "/api/v2/blogposts/42/labels", `{"results":[]}`, `[]`},
		{"attachment labels", "labels", []string{"--attachment-id", "42"}, "/api/v2/attachments/42/labels", `{"results":[]}`, `[]`},
		{"custom labels", "labels", []string{"--custom-content-id", "42"}, "/api/v2/custom-content/42/labels", `{"results":[]}`, `[]`},
		{"space statuses", "statuses", []string{"--space", "RD"}, "/rest/api/space/RD/state", `[{"id":73,"name":"Draft","color":"#abc"}]`, `[{"id":73,"name":"Draft","color":"#abc"}]`},
		{"personal statuses", "statuses", []string{"--personal"}, "/rest/api/content-states", `[]`, `[]`},
		{"page status", "statuses", []string{"--page-id", "42"}, "/rest/api/content/42/state", `{"contentState":{"id":73,"name":"Draft","color":"#abc"},"lastUpdated":"ignored"}`, `[{"id":73,"name":"Draft","color":"#abc"}]`},
		{"unassigned page", "statuses", []string{"--page-id", "42"}, "/rest/api/content/42/state", `{"contentState":null}`, `[]`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("MARK_SPACE", "IGNORED")
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, []byte("space = 'ALSO_IGNORED'\n"), 0600))
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				user, pass, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "user", user)
				assert.Equal(t, "token", pass)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/wiki/rest/api/space/RD" {
					_, _ = fmt.Fprint(w, `{"id":55,"key":"RD"}`)
					return
				}
				requests++
				assert.Equal(t, "/wiki"+testCase.path, r.URL.Path)
				_, _ = fmt.Fprint(w, testCase.body)
			}))
			defer server.Close()
			args := []string{"--config", config, "--base-url", server.URL + "/wiki", "--username", "user", "--password", "token", "metadata", testCase.resource, "list"}
			args = append(args, testCase.selectors...)
			stdout, _, err := runMark(t, args...)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, stdout)
			assert.Equal(t, 1, requests)
		})
	}
}

func TestMetadataSelectors(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want string
	}{
		{"label conflict", []string{"labels", "list", "--space", "RD", "--page-id", "42"}, "only one"},
		{"status conflict", []string{"statuses", "list", "--personal", "--page-id", "42"}, "only one"},
		{"empty selector", []string{"labels", "list", "--page-id="}, "empty"},
		{"extra argument", []string{"labels", "list", "oops"}, "unexpected argument"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0600))
			args := append([]string{"--config", config, "metadata"}, testCase.args...)
			stdout, _, err := runMark(t, args...)
			require.ErrorContains(t, err, testCase.want)
			assert.Empty(t, stdout)
		})
	}
	t.Run("status help without credentials", func(t *testing.T) {
		t.Setenv("MARK_SPACE", "IGNORED")
		stdout, _, err := runMark(t, "metadata", "statuses", "list")
		require.NoError(t, err)
		assert.Contains(t, stdout, "--personal")
		assert.Contains(t, stdout, "--page-id")
	})
}

func TestMetadataPaginationAndErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		second bool
	}{
		{"pagination", http.StatusOK, false},
		{"missing target", http.StatusNotFound, false},
		{"forbidden", http.StatusForbidden, false},
		{"unauthorized", http.StatusUnauthorized, false},
		{"later failure", http.StatusNotFound, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0600))
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				calls++
				w.Header().Set("Content-Type", "application/json")
				if (testCase.status != http.StatusOK && !testCase.second) || (testCase.second && r.URL.Query().Get("cursor") != "") {
					w.WriteHeader(testCase.status)
					_, _ = fmt.Fprint(w, `{"message":"listing failed"}`)
					return
				}
				if r.URL.Query().Get("cursor") == "" {
					_, _ = fmt.Fprint(w, `{"results":[{"id":"2","name":"z","prefix":"global"}],"_links":{"next":"/wiki/api/v2/pages/42/labels?cursor=next"}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"results":[{"id":"1","name":"a","prefix":"global"}]}`)
				}
			}))
			defer server.Close()
			stdout, _, err := runMark(t, "--config", config, "--base-url", server.URL+"/wiki", "-u", "user", "-p", "token", "metadata", "labels", "list", "--page-id", "42")
			if testCase.status != http.StatusOK {
				require.Error(t, err)
				assert.Empty(t, stdout)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
			assert.JSONEq(t, `[{"id":"2","name":"z","prefix":"global"},{"id":"1","name":"a","prefix":"global"}]`, stdout)
		})
	}
}
