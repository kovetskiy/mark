package util

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestFeaturesSubprocess is the worker for checkFeatures and does nothing
// unless a parent names it. Like TestConfigSubprocess, it runs in a process of
// its own because the flag set keeps what an earlier parse set.
func TestFeaturesSubprocess(t *testing.T) {
	encodedArgs := os.Getenv("MARK_TEST_FEATURES_ARGS")
	if encodedArgs == "" {
		t.Skip("not a subprocess case")
	}

	var args []string
	require.NoError(t, json.Unmarshal([]byte(encodedArgs), &args))

	var result struct {
		Features []string
		Err      string
	}

	ctx, cmd := mustParseArgs(t, &cli.Command{Flags: Flags}, append([]string{"mark"}, args...)...)
	if _, err := CheckFlags(ctx, cmd); err != nil {
		result.Err = err.Error()
	} else {
		result.Features = EnabledFeatures(cmd)
	}

	encoded, err := json.Marshal(result)
	require.NoError(t, err)

	fmt.Printf("\nCHECKED-FEATURES<%s>CHECKED-FEATURES\n", encoded)
}

// checkFeatures runs CheckFlags in a fresh process and returns the features
// it settled on, or its error.
func checkFeatures(t *testing.T, environment map[string]string, args ...string) ([]string, string) {
	t.Helper()

	encodedArgs, err := json.Marshal(args)
	require.NoError(t, err)

	worker := exec.Command(os.Args[0], "-test.run=^TestFeaturesSubprocess$", "-test.v")
	worker.Env = append(os.Environ(), "MARK_TEST_FEATURES_ARGS="+string(encodedArgs))
	for name, value := range environment {
		worker.Env = append(worker.Env, name+"="+value)
	}

	output, err := worker.CombinedOutput()
	require.NoError(t, err, string(output))

	_, after, found := strings.Cut(string(output), "CHECKED-FEATURES<")
	require.True(t, found, "worker printed no result: %s", output)
	encoded, _, found := strings.Cut(after, ">CHECKED-FEATURES")
	require.True(t, found, "worker printed no result: %s", output)

	var result struct {
		Features []string
		Err      string
	}
	require.NoError(t, json.Unmarshal([]byte(encoded), &result))

	return result.Features, result.Err
}

func configWithFeatures(t *testing.T, value string) string {
	t.Helper()

	return writeConfig(t, t.TempDir(), "mark.toml", "features = "+value+"\n")
}

// TestFeaturesNone: with --features replacing the defaults, "none" is how to
// ask for no optional feature at all, from wherever the setting comes.
func TestFeaturesNone(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		args func(t *testing.T) []string
	}{
		"flag":               {args: func(*testing.T) []string { return []string{"--features", "none"} }},
		"environment":        {env: map[string]string{"MARK_FEATURES": "none"}},
		"configuration file": {args: func(t *testing.T) []string { return []string{"--config", configWithFeatures(t, `["none"]`)} }},
	} {
		t.Run(name, func(t *testing.T) {
			var args []string
			if tc.args != nil {
				args = tc.args(t)
			}

			features, errText := checkFeatures(t, tc.env, args...)
			require.Empty(t, errText)
			assert.Empty(t, features)
		})
	}
}

func TestFeaturesNoneStandsAlone(t *testing.T) {
	_, errText := checkFeatures(t, nil, "--features", "none", "--features", "math")
	assert.Contains(t, errText, "cannot be combined")
}

// TestFeaturesKeepsTheDefaultsWhenNotGiven: "none" is opt-in; saying nothing
// still means the defaults.
func TestFeaturesKeepsTheDefaultsWhenNotGiven(t *testing.T) {
	features, errText := checkFeatures(t, nil)
	require.Empty(t, errText)
	assert.Equal(t, defaultFeatures, features)
}

// TestFeaturesRejectsAnEmptyValue: each way of writing an empty list was
// refused without a way out or, worse, read as no value and answered with the
// defaults.
func TestFeaturesRejectsAnEmptyValue(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		_, errText := checkFeatures(t, nil, "--features", "")
		assert.Contains(t, errText, `use "none"`)
	})

	t.Run("environment", func(t *testing.T) {
		_, errText := checkFeatures(t, map[string]string{"MARK_FEATURES": ""})
		assert.Contains(t, errText, "MARK_FEATURES")
		assert.Contains(t, errText, `use "none"`)
	})

	for name, value := range map[string]string{
		"empty list in the configuration file":   `[]`,
		"empty string in the configuration file": `""`,
	} {
		t.Run(name, func(t *testing.T) {
			path := configWithFeatures(t, value)

			_, errText := checkFeatures(t, nil, "--config", path)
			assert.Contains(t, errText, filepath.Base(filepath.Dir(path)))
			assert.Contains(t, errText, `use "none"`)
		})
	}

	t.Run("the flag wins over an empty environment value", func(t *testing.T) {
		features, errText := checkFeatures(t, map[string]string{"MARK_FEATURES": ""}, "--features", "math")
		require.Empty(t, errText)
		assert.Equal(t, []string{"math"}, features)
	})
}
