package util

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
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
		{"page labels with help disabled", "labels", []string{"--page-id", "42", "--help=false"}, "/api/v2/pages/42/labels", `{"results":[{"id":12,"name":"alpha","prefix":"global"}]}`, `[{"id":"12","name":"alpha","prefix":"global"}]`},
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
			require.NoError(t, os.WriteFile(config, []byte("space = 'ALSO_IGNORED'\n"), 0o600))

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

			args := []string{"--config", config, "--base-url", server.URL + "/wiki", "--username", "user", "--password", "token", "metadata", testCase.resource, "list", "--json"}
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
			require.NoError(t, os.WriteFile(config, nil, 0o600))
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
	t.Run("disabled help is not a selector", func(t *testing.T) {
		stdout, _, err := runMark(t, "metadata", "statuses", "list", "--help=false")
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
			require.NoError(t, os.WriteFile(config, nil, 0o600))

			calls := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)

				calls++

				w.Header().Set("Content-Type", "application/json")

				failFirstPage := testCase.status != http.StatusOK && !testCase.second
				failLaterPage := testCase.second && r.URL.Query().Get("cursor") != ""
				if failFirstPage || failLaterPage {
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

			stdout, _, err := runMark(t, "--config", config, "--base-url", server.URL+"/wiki", "-u", "user", "-p", "token", "metadata", "labels", "list", "--page-id", "42", "--json")
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

func TestMetadataTableOutput(t *testing.T) {
	for _, testCase := range []struct {
		name, resource, body, want string
		selectors                  []string
	}{
		{"labels", "labels", `{"results":[{"id":"12","name":"alpha","prefix":"global"},{"id":"13","name":"beta","prefix":"team"}]}`, `^NAME +ID +PREFIX\nalpha +12 +global\nbeta +13 +team\n$`, nil},
		{"statuses", "statuses", `[{"id":73,"name":"In progress","color":"#2684ff"}]`, `^NAME +ID +COLOR\nIn progress +73 +#2684ff\n$`, []string{"--space", "RD"}},
		{"empty labels", "labels", `{"results":[]}`, `^No labels found\.\n$`, nil},
		{"empty statuses", "statuses", `[]`, `^No statuses found\.\n$`, []string{"--personal"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0o600))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, testCase.body)
			}))
			defer server.Close()

			args := []string{"--config", config, "--base-url", server.URL, "-u", "user", "-p", "token", "metadata", testCase.resource, "list"}
			args = append(args, testCase.selectors...)
			stdout, _, err := runMark(t, args...)
			require.NoError(t, err)
			assert.Regexp(t, testCase.want, stdout)
		})
	}
}

// TestMetadataRequestFailures keeps failed status reads and space lookups distinct from empty listings.
func TestMetadataRequestFailures(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		args     []string
		path     string
		status   int
		want     error
		fallback bool
	}{
		{"missing page status", []string{"statuses", "list", "--page-id", "42"}, "/rest/api/content/42/state", http.StatusNotFound, confluence.ErrNotFound, false},
		{"forbidden space statuses", []string{"statuses", "list", "--space", "RD"}, "/rest/api/space/RD/state", http.StatusForbidden, confluence.ErrForbidden, false},
		{"unauthorized personal statuses", []string{"statuses", "list", "--personal"}, "/rest/api/content-states", http.StatusUnauthorized, confluence.ErrUnauthorized, false},
		{"missing space lookup", []string{"labels", "list", "--space", "MISSING"}, "/rest/api/space/MISSING", http.StatusNotFound, confluence.ErrNotFound, true},
		{"forbidden space lookup", []string{"labels", "list", "--space", "RD"}, "/rest/api/space/RD", http.StatusForbidden, confluence.ErrForbidden, true},
		{"unauthorized space lookup", []string{"labels", "list", "--space", "RD"}, "/rest/api/space/RD", http.StatusUnauthorized, confluence.ErrUnauthorized, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0o600))

			var paths []string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				paths = append(paths, r.URL.Path)

				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case testCase.path:
					w.WriteHeader(testCase.status)
					_, _ = fmt.Fprint(w, `{"message":"cannot access metadata target"}`)
				case "/api/v2/spaces":
					if testCase.status == http.StatusNotFound {
						assert.Equal(t, "MISSING", r.URL.Query().Get("keys"))

						_, _ = fmt.Fprint(w, `{"results":[]}`)
					} else {
						assert.Equal(t, "RD", r.URL.Query().Get("keys"))
						w.WriteHeader(http.StatusNotFound)
						_, _ = fmt.Fprint(w, `{"message":"v2 endpoint unavailable"}`)
					}
				default:
					t.Errorf("unexpected request after failed lookup: %s", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			args := []string{"--config", config, "--base-url", server.URL, "-u", "user", "-p", "token", "metadata"}
			args = append(args, testCase.args...)
			stdout, _, err := runMark(t, args...)
			require.ErrorIs(t, err, testCase.want)
			assert.Empty(t, stdout)

			wantPaths := []string{testCase.path}
			if testCase.fallback {
				wantPaths = append(wantPaths, "/api/v2/spaces")
			}

			assert.Equal(t, wantPaths, paths)
		})
	}
}

// TestMetadataGatewayListings keeps space resolution, cursors, and status reads under the configured gateway path.
func TestMetadataGatewayListings(t *testing.T) {
	for _, testCase := range []struct {
		resource, want string
		paths          []string
	}{
		{"labels", `[{"id":"12","name":"alpha","prefix":"global"},{"id":"13","name":"beta","prefix":"team"}]`, []string{
			"/ex/confluence/cloud-id/api/v2/spaces",
			"/ex/confluence/cloud-id/api/v2/spaces/55/content/labels",
			"/ex/confluence/cloud-id/api/v2/spaces/55/content/labels",
		}},
		{"statuses", `[{"id":73,"name":"Draft","color":"#abc"}]`, []string{
			"/ex/confluence/cloud-id/rest/api/space/RD/state",
		}},
	} {
		t.Run(testCase.resource, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0o600))

			var paths []string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				paths = append(paths, r.URL.Path)

				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case "/ex/confluence/cloud-id/api/v2/spaces":
					assert.Equal(t, "RD", r.URL.Query().Get("keys"))

					_, _ = fmt.Fprint(w, `{"results":[{"id":"55","key":"RD"}]}`)
				case "/ex/confluence/cloud-id/api/v2/spaces/55/content/labels":
					if r.URL.Query().Get("cursor") == "" {
						_, _ = fmt.Fprint(w, `{"results":[{"id":"12","name":"alpha","prefix":"global"}],"_links":{"next":"https://site.invalid/wiki/api/v2/spaces/55/content/labels?cursor=next"}}`)
					} else {
						assert.Equal(t, "next", r.URL.Query().Get("cursor"))

						_, _ = fmt.Fprint(w, `{"results":[{"id":"13","name":"beta","prefix":"team"}]}`)
					}
				case "/ex/confluence/cloud-id/rest/api/space/RD/state":
					_, _ = fmt.Fprint(w, `[{"id":73,"name":"Draft","color":"#abc"}]`)
				default:
					t.Errorf("unexpected gateway request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			stdout, _, err := runMark(t, "--config", config, "--base-url", server.URL+"/ex/confluence/cloud-id",
				"-u", "user", "-p", "token", "metadata", testCase.resource, "list", "--space", "RD", "--json")
			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, stdout)
			assert.Equal(t, testCase.paths, paths)
		})
	}
}

// TestMetadataOutputSources checks JSON selection and overrides across configuration layers.
func TestMetadataOutputSources(t *testing.T) {
	for _, testCase := range []struct {
		name, config, environment string
		args                      []string
		wantJSON                  bool
	}{
		{"configuration selects JSON", "json = true\n", "", nil, true},
		{"environment selects JSON", "json = false\n", "true", nil, true},
		{"environment disables configured JSON", "json = true\n", "false", nil, false},
		{"command line selects JSON", "json = false\n", "false", []string{"--json"}, true},
		{"command line disables inherited JSON", "json = true\n", "true", []string{"--json=false"}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.environment != "" {
				t.Setenv("MARK_JSON", testCase.environment)
			}

			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, []byte(testCase.config), 0o600))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/v2/labels", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"results":[]}`)
			}))
			defer server.Close()

			args := []string{"--config", config, "--base-url", server.URL, "-u", "user", "-p", "token", "metadata", "labels", "list"}
			args = append(args, testCase.args...)
			stdout, _, err := runMark(t, args...)
			require.NoError(t, err)

			if testCase.wantJSON {
				assert.JSONEq(t, `[]`, stdout)
			} else {
				assert.Equal(t, "No labels found.\n", stdout)
			}
		})
	}
}

// TestMetadataOutputFailures requires the command to fail when its output cannot be written.
func TestMetadataOutputFailures(t *testing.T) {
	for _, testCase := range []struct {
		name, resource, path, body string
		args                       []string
	}{
		{"label table flush", "labels", "/api/v2/labels", `{"results":[{"id":"12","name":"alpha","prefix":"global"}]}`, nil},
		{"status table flush", "statuses", "/rest/api/content-states", `[{"id":73,"name":"Draft","color":"#abc"}]`, []string{"--personal"}},
		{"label JSON write", "labels", "/api/v2/labels", `{"results":[{"id":"12","name":"alpha","prefix":"global"}]}`, []string{"--json"}},
		{"status JSON write", "statuses", "/rest/api/content-states", `[{"id":73,"name":"Draft","color":"#abc"}]`, []string{"--personal", "--json"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0o600))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, testCase.path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, testCase.body)
			}))
			defer server.Close()

			output, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
			require.NoError(t, err)
			require.NoError(t, output.Close())

			cmd := newCommand("test")
			cmd.Writer = output
			args := []string{"mark", "--config", config, "--base-url", server.URL, "-u", "user", "-p", "token", "metadata", testCase.resource, "list"}
			args = append(args, testCase.args...)
			_, _, err = captureOutput(t, func() error {
				return cmd.Run(t.Context(), args)
			})
			require.ErrorIs(t, err, os.ErrClosed)
			assert.ErrorContains(t, err, "unable to write "+testCase.resource)
		})
	}
}
