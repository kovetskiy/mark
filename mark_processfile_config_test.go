package mark

import (
	"bytes"
	"io"
	"math"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/confluence/confluencetest"
	"github.com/kovetskiy/mark/v16/mermaid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// processFileFixture is one publishable document and the server it goes to,
// for comparing what ProcessFile and Run make of the same settings.
func processFileFixture(t *testing.T, body string) (*confluencetest.Server, *confluence.API, string, Config) {
	t.Helper()

	server := confluencetest.New(t)
	home := server.AddPage("DOCS", "Home", "page", "")
	server.SetHomepage("DOCS", home.ID)

	dir := t.TempDir()
	file := writeFile(t, dir, "doc.md", "<!-- Space: DOCS -->\n<!-- Title: Doc -->\n\n"+body)

	config := Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "token",
		Files:    filepath.Join(dir, "doc.md"),
		Features: []string{"mention"},
		Output:   io.Discard,
	}

	return server, confluence.NewAPI(server.URL, "user", "token", false), file, config
}

// TestProcessFileRefusesWhatRunRefuses: ProcessFile is the library's other way
// in, and it used to skip every check Run makes of the settings. A MermaidOutput
// it did not recognise published a PNG without a word, and a bundle asked for
// alongside one was dropped with it.
func TestProcessFileRefusesWhatRunRefuses(t *testing.T) {
	cases := map[string]func(*Config){
		"an unknown mermaid engine": func(c *Config) { c.MermaidEngine = "graphviz" },
		"an unknown mermaid output": func(c *Config) { c.MermaidOutput = "jpeg" },
		"a bundle inside a png":     func(c *Config) { c.MermaidBundle = true },
		"an unknown d2 output":      func(c *Config) { c.D2Output = "jpeg" },
		"a d2 scale of nothing": func(c *Config) {
			c.Features = []string{"d2"}
			c.D2Scale = math.NaN()
		},
		"warn-only with nothing checked": func(c *Config) { c.CheckLinksWarnOnly = true },
		"an unknown link check":          func(c *Config) { c.CheckLinks = []string{"everything"} },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			server, api, file, config := processFileFixture(t, "Body.\n")
			change(&config)

			runErr := Run(config)
			require.Error(t, runErr)

			server.ResetRequests()

			target, err := ProcessFile(file, api, config)
			require.Error(t, err)
			assert.Nil(t, target)
			assert.Equal(t, runErr.Error(), err.Error(), "the same setting is refused in the same words")
			assert.Empty(t, server.Requests(), "refused before Confluence is asked anything")
		})
	}
}

// TestProcessFileUsesTheMermaidEngine: the engine is a process-wide choice
// that Run made and ProcessFile did not, so a library caller asking for merman
// got whichever engine happened to be current -- chrome, unless something else
// in the process had run first.
func TestProcessFileUsesTheMermaidEngine(t *testing.T) {
	var logged bytes.Buffer

	restore := log.Logger
	log.Logger = zerolog.New(&logged)
	t.Cleanup(func() {
		log.Logger = restore
		require.NoError(t, mermaid.UseEngine(""))
	})

	_, api, file, config := processFileFixture(t, "Body.\n")
	config.MermaidEngine = mermaid.EngineMerman

	_, err := ProcessFile(file, api, config)
	require.NoError(t, err)

	// The warning is said by choosing merman and by nothing else, so it is how
	// the choice shows from outside the package.
	assert.Contains(t, logged.String(), "merman render engine is experimental")
}

// TestProcessFileReportsMissingPagesAsRunDoes: the ac: links a run leaves
// unresolved are counted and introduced, and ProcessFile's were the bare list
// with nothing in front of it.
func TestProcessFileReportsMissingPagesAsRunDoes(t *testing.T) {
	_, api, file, config := processFileFixture(t, "See [it](ac:Nowhere).\n")
	config.CheckLinks = []string{"confluence"}

	runErr := Run(config)
	require.Error(t, runErr)

	_, err := ProcessFile(file, api, config)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "1 link does not resolve:\n  ")
	assert.Contains(t, err.Error(), `"Nowhere"`)
	assert.Equal(t, runErr.Error(), err.Error())
}
