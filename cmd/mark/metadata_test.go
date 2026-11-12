package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetadataFailuresExit runs the real entry point to check failure streams and exit status.
func TestMetadataFailuresExit(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		args   []string
		path   string
		status int
	}{
		{"missing label target with JSON", []string{"labels", "list", "--page-id", "42", "--json"}, "/wiki/api/v2/pages/42/labels", http.StatusNotFound},
		{"forbidden status target with table", []string{"statuses", "list", "--page-id", "42"}, "/wiki/rest/api/content/42/state", http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, testCase.path, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(testCase.status)
				_, _ = fmt.Fprint(w, `{"message":"cannot access metadata target"}`)
			}))
			defer server.Close()

			config := filepath.Join(t.TempDir(), "mark.toml")
			require.NoError(t, os.WriteFile(config, nil, 0o600))
			args := []string{"--config", config, "--base-url", server.URL + "/wiki", "--color", "never", "metadata"}
			args = append(args, testCase.args...)
			encoded, err := json.Marshal(args)
			require.NoError(t, err)

			worker := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMetadataCommandProcess$")
			worker.WaitDelay = 2 * time.Second

			for _, variable := range os.Environ() {
				if !strings.HasPrefix(variable, "MARK_") {
					worker.Env = append(worker.Env, variable)
				}
			}

			worker.Env = append(worker.Env, "MARK_TEST_METADATA_ARGS="+string(encoded), "MARK_USERNAME=user", "MARK_PASSWORD=token")

			var stdout, stderr bytes.Buffer

			worker.Stdout, worker.Stderr = &stdout, &stderr
			err = worker.Run()

			var exitError *exec.ExitError
			require.ErrorAs(t, err, &exitError, stderr.String())
			assert.Equal(t, 1, exitError.ExitCode())
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "cannot access metadata target")
			assert.Contains(t, stderr.String(), strconv.Itoa(testCase.status))
		})
	}
}

// TestMetadataCommandProcess invokes main in a child test process so its os.Exit is observable.
func TestMetadataCommandProcess(t *testing.T) {
	encoded := os.Getenv("MARK_TEST_METADATA_ARGS")
	if encoded == "" {
		t.Skip("subprocess helper")
	}

	var args []string
	require.NoError(t, json.Unmarshal([]byte(encoded), &args))
	os.Args = append([]string{"mark"}, args...)

	main()
}
