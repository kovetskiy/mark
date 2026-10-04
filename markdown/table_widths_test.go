package mark

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
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

func TestTableWidthsEmitColgroup(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "<!-- Table-Widths: 160,720 -->\n\n"+widthsTable, "test.md")

			assert.Contains(t, out, "<table>\n<colgroup>\n<col style=\"width: 160px;\"/>\n<col style=\"width: 720px;\"/>\n</colgroup>\n<thead>")
			assert.NotContains(t, out, "Table-Widths", "the directive is consumed")
			assert.Empty(t, logs)
		})
	}
}

func TestTableWidthsAdjacentWithoutBlankLine(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Table-Widths: 10, 20 -->\n"+widthsTable, "test.md")

			assert.Contains(t, out, "width: 10px;")
			assert.Contains(t, out, "width: 20px;")
		})
	}
}

func TestTableWidthsKeepAlignment(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Table-Widths: 100,200 -->\n| a | b |\n| :-: | --: |\n| 1 | 2 |\n", "test.md")

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

func TestTableWidthsOnlyApplyToTheNextTable(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, _ := compileLogged(t, name, "<!-- Table-Widths: 5,6 -->\n"+widthsTable+"\n"+widthsTable, "test.md")

			assert.Equal(t, 1, bytes.Count([]byte(out), []byte("<colgroup>")))
		})
	}
}

func TestTableWidthsFallBackToBareTable(t *testing.T) {
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
				out, logs := compileLogged(t, name, "<!-- Table-Widths: "+value+" -->\n\n"+widthsTable, "test.md")

				assert.NotContains(t, out, "colgroup")
				assert.Contains(t, out, "<table>\n<thead>")
				assert.Contains(t, logs, "Table-Widths")
				assert.Contains(t, logs, `"level":"warn"`)
			})
		}
	}
}

func TestTableWidthsNotBeforeATableIsIgnoredWithWarning(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "<!-- Table-Widths: 1,2 -->\n\nsome text\n\n"+widthsTable, "test.md")

			assert.NotContains(t, out, "colgroup")
			assert.Contains(t, logs, "not directly followed by a table")
		})
	}
}

func TestTableWidthsInsideBlockquote(t *testing.T) {
	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "> <!-- Table-Widths: 30,40 -->\n>\n> | a | b |\n> |---|---|\n> | 1 | 2 |\n", "test.md")

			assert.Contains(t, out, "width: 30px;")
			assert.Empty(t, logs)
		})
	}
}

func TestTableWidthsAfterMacroDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"box.md": "<b>{{ .Text }}</b>\n"})

	src := "<!-- Macro: BOX-([0-9]+)\n     Template: box.md\n     Text: ${1} -->\n\nBOX-7\n\n<!-- Table-Widths: 50,60 -->\n\n" + widthsTable

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

func TestTableWidthsInsideIncludedFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"frag.md": "<!-- Table-Widths: 70,80 -->\n\n" + widthsTable})

	for name := range compilers {
		t.Run(name, func(t *testing.T) {
			out, logs := compileLogged(t, name, "intro\n\n<!-- Include: frag.md -->\n", filepath.Join(dir, "doc.md"))

			assert.Contains(t, out, "width: 70px;")
			assert.Contains(t, out, "width: 80px;")
			assert.NotContains(t, out, "Table-Widths")
			assert.Empty(t, logs)
		})
	}
}
