package mark

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/stdlib"
	"github.com/kovetskiy/mark/v17/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const widthsTable = "| a | b |\n|---|---|\n| 1 | 2 |\n"

// compileLogged compiles src on one path and returns the output together with
// what was logged while doing it.
func compileLogged(t *testing.T, name, src, path string) (string, string) {
	t.Helper()

	var logs bytes.Buffer
	saved := log.Logger
	log.Logger = zerolog.New(&logs).Level(zerolog.WarnLevel)
	t.Cleanup(func() { log.Logger = saved })

	lib, err := stdlib.New(nil)
	require.NoError(t, err)

	out, _, err := compilers[name]([]byte(src), lib, path, types.MarkConfig{})
	require.NoError(t, err)

	return out, logs.String()
}

func TestColumnWidthsEmitColgroup(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "<!-- Column-Widths: 160,720 -->\n\n"+widthsTable, "test.md")

			assert.Contains(t, out, "<table>\n<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n<thead>")
			assert.NotContains(t, out, "Column-Widths", "the directive is consumed")
			assert.Empty(t, logs)
		})
	}
}

func TestColumnWidthsAdjacentWithoutBlankLine(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Column-Widths: 10, 20 -->\n"+widthsTable, "test.md")

			assert.Contains(t, out, "width: 10px;")
			assert.Contains(t, out, "width: 20px;")
		})
	}
}

// TestColumnWidthsDirectiveSpellings: the directive is matched without regard
// to case, and may run over several lines.
func TestColumnWidthsDirectiveSpellings(t *testing.T) {
	for label, directive := range map[string]string{
		"lowercase":     "<!--column-widths:10,20-->",
		"several lines": "<!--\nColumn-Widths:\n10,\n20\n-->",
	} {
		for name := range compilers {
			t.Run(name+"/"+label, func(t *testing.T) {
				out, logs := compileLogged(t, name, directive+"\n\n"+widthsTable, "test.md")

				assert.Contains(t, out, "<colgroup>\n<col style=\"width: 10px;\"/>\n<col style=\"width: 20px;\"/>\n</colgroup>")
				assert.NotContains(t, out, "idths")
				assert.Empty(t, logs)
			})
		}
	}
}

func TestColumnWidthsKeepAlignment(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Column-Widths: 100,200 -->\n| a | b |\n| :-: | --: |\n| 1 | 2 |\n", "test.md")

			assert.Contains(t, out, "<colgroup>")
			assert.Contains(t, out, `<th style="text-align:center">a</th>`)
			assert.Contains(t, out, `<td style="text-align:right">2</td>`)
		})
	}
}

func TestTableWithoutDirectiveIsUnchanged(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, widthsTable, "test.md")

			assert.NotContains(t, out, "colgroup")
			assert.True(t, bytes.HasPrefix([]byte(out), []byte("<table>\n<thead>\n")), out)
		})
	}
}

func TestColumnWidthsOnlyApplyToTheNextTable(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Column-Widths: 5,6 -->\n"+widthsTable+"\n"+widthsTable, "test.md")

			assert.Equal(t, 1, bytes.Count([]byte(out), []byte("<colgroup>")))
		})
	}
}

func TestColumnWidthsFallBackToBareTable(t *testing.T) {
	cases := map[string]string{
		"too few":       "160",
		"too many":      "1,2,3",
		"percent":       "30%,70%",
		"auto":          "auto,100",
		"unit":          "100px,200px",
		"zero":          "0,100",
		"negative":      "-5,100",
		"text":          "wide,narrow",
		"empty entry":   "100,,200",
		"empty":         "",
		"huge":          "99999999999999999999,1",
		"quote":         `1"><x/>,2`,
		"inner comment": "1 -- 2,3",
		"fractional":    "1.5,2",
		"leading plus":  "+5,6",
	}

	for name := range compilers {
		for label, value := range cases {
			t.Run(name+"/"+label, func(t *testing.T) {
				out, logs := compileLogged(t, name, "<!-- Column-Widths: "+value+" -->\n\n"+widthsTable, "test.md")

				assert.NotContains(t, out, "colgroup")
				assert.Contains(t, out, "<table>\n<thead>")
				assert.Contains(t, logs, "Column-Widths")
				assert.Contains(t, logs, `"level":"warn"`)
			})
		}
	}
}

func TestColumnWidthsNotBeforeATableIsIgnoredWithWarning(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "<!-- Column-Widths: 1,2 -->\n\nsome text\n\n"+widthsTable, "test.md")

			assert.NotContains(t, out, "colgroup")
			assert.Contains(t, logs, "not directly followed by a table")
		})
	}
}

func TestColumnWidthsInsideBlockquote(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "> <!-- Column-Widths: 30,40 -->\n>\n> | a | b |\n> |---|---|\n> | 1 | 2 |\n", "test.md")

			assert.Contains(t, out, "width: 30px;")
			assert.Empty(t, logs)
		})
	}
}

func TestColumnWidthsAfterMacroDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"box.md": "<b>{{ .Text }}</b>\n"})

	src := "<!-- Macro: BOX-([0-9]+)\n     Template: box.md\n     Text: ${1} -->\n\nBOX-7\n\n<!-- Column-Widths: 50,60 -->\n\n" + widthsTable

	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, src, filepath.Join(dir, "doc.md"))

			assert.Contains(t, out, "<b>7</b>")
			assert.Contains(t, out, "width: 50px;")
			assert.Contains(t, out, "width: 60px;")
			assert.Empty(t, logs)
		})
	}
}

func TestColumnWidthsInsideIncludedFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"frag.md": "<!-- Column-Widths: 70,80 -->\n\n" + widthsTable})

	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "intro\n\n<!-- Include: frag.md -->\n", filepath.Join(dir, "doc.md"))

			assert.Contains(t, out, "width: 70px;")
			assert.Contains(t, out, "width: 80px;")
			assert.NotContains(t, out, "Column-Widths")
			assert.Empty(t, logs)
		})
	}
}

// The source compiled here is the body after the header comments are gone and
// the macros and includes are expanded, so a line counted in it is not a line
// of the file. The warning names the file and quotes the directive instead.
func TestColumnWidthsWarningNamesFileNotLine(t *testing.T) {
	cases := map[string]string{
		"invalid":      "<!-- Column-Widths: 30%,70% -->\n\n" + widthsTable,
		"mismatch":     "<!-- Column-Widths: 1,2,3 -->\n\n" + widthsTable,
		"not adjacent": "<!-- Column-Widths: 1,2,3 -->\n\ntext\n\n" + widthsTable,
	}

	for name := range compilers {
		for label, src := range cases {
			t.Run(name+"/"+label, func(t *testing.T) {
				_, logs := compileLogged(t, name, src, "docs/page.md")

				assert.Contains(t, logs, `"file":"docs/page.md"`)
				assert.Contains(t, logs, "Column-Widths")
				assert.NotContains(t, logs, "line")
			})
		}
	}
}
