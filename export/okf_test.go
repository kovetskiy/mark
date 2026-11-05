package export

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// A version's author is a person, human:, unless Cloud says the account is an
// app, which OKF names a process so that it is not trusted as hand-authored.
func TestOKFActor(t *testing.T) {
	for name, test := range map[string]struct {
		by   string
		want string
	}{
		"app":               {`{"type":"known","accountId":"557058:abc","accountType":"app"}`, "process:557058:abc"},
		"cloud person":      {`{"type":"known","accountId":"557058:abc","accountType":"atlassian"}`, "human:557058:abc"},
		"customer":          {`{"accountId":"qm:abc","accountType":"customer"}`, "human:qm:abc"},
		"no account type":   {`{"accountId":"557058:abc"}`, "human:557058:abc"},
		"data center":       {`{"type":"known","username":"jdoe","userKey":"8a7f"}`, "human:jdoe"},
		"user key only":     {`{"userKey":"8a7f"}`, "human:8a7f"},
		"app without an id": {`{"username":"bot","accountType":"app"}`, "process:bot"},
		"nobody named":      {`{}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var user confluence.User
			require.NoError(t, json.Unmarshal([]byte(test.by), &user))
			assert.Equal(t, test.want, okfActor(&user))
		})
	}
	assert.Empty(t, okfActor(nil))
}

// OKF front matter writes emoji as they are, where yaml.v3 escapes them.
func TestOKFFrontMatterWritesEmojiAsThemselves(t *testing.T) {
	page := &confluence.PageInfo{ID: "1", Title: "🚀 Launch"}
	out, err := okfFrontMatter(page, "https://x/1", "mark/v1", []string{"🔥"})
	require.NoError(t, err)
	assert.Contains(t, out, "\ntitle: \"🚀 Launch\"\n")
	assert.Contains(t, out, "\n  - \"🔥\"\n")
	assert.Contains(t, out, "\n    title: \"🚀 Launch\"\n")
	assert.NotContains(t, out, `\U`)
}

// Every value in OKF front matter reads back as the string it was, each on
// one line.
func TestOKFFrontMatterReadsBack(t *testing.T) {
	values := []string{
		"🚀 Launch", "👩‍💻 Dev", "🇩🇪 Flag", "Plain", "",
		`say "hi"`, "it's", `back\slash`, "a # b", "#hash", "a: b", "ends:",
		"- item", "yes", "No", "on", "null", "~", "12", "1.5", "0x1F",
		"2026-05-01", "line\nbreak", "cr\rlf", "tab\there", "para\u2028sep",
		"next\u0085line", "\ufeffbom", " leading", "trailing ", "[a]", "{b}",
		"&anchor", "*alias", "!tag", "|pipe", ">fold", "@at", "`tick", "%p",
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			page := &confluence.PageInfo{ID: "1", Title: value}
			page.Version.When = "2026-05-01T10:30:00Z"
			page.Version.By = &confluence.User{Username: value}
			out, err := okfFrontMatter(page, value, value, []string{value, "🏷️ " + value})
			require.NoError(t, err)

			body, ok := strings.CutPrefix(out, "---\n")
			require.True(t, ok)
			body, ok = strings.CutSuffix(body, "---\n")
			require.True(t, ok)
			for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
				assert.Regexp(t, `^(\S| {2}\S| {2}- \S| {4}\S)`, line, "a value on a line of its own")
			}

			var got okfConcept
			require.NoError(t, yaml.Unmarshal([]byte(body), &got), out)
			want := okfConcept{
				Type:      "Confluence Page",
				Title:     value,
				Resource:  value,
				Tags:      []string{value, "🏷️ " + value},
				Generated: okfGenerated{By: value},
			}
			if value != "" {
				want.Sources = []okfSource{{
					ID:           "confluence",
					Resource:     value,
					Title:        value,
					Author:       "human:" + value,
					LastModified: "2026-05-01T10:30:00Z",
				}}
			}
			assert.Equal(t, want, got, out)
		})
	}
}

// okfConcept.frontMatter writes what yaml.v3 writes for every field of the
// struct, filled and empty, so that a field added to it and not to the writer
// fails here.
func TestOKFFrontMatterMatchesYAMLEncoder(t *testing.T) {
	var full okfConcept
	fill(reflect.ValueOf(&full).Elem())
	for name, concept := range map[string]okfConcept{"full": full, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			var want bytes.Buffer
			encoder := yaml.NewEncoder(&want)
			encoder.SetIndent(2)
			require.NoError(t, encoder.Encode(concept))
			require.NoError(t, encoder.Close())

			got, err := concept.frontMatter()
			require.NoError(t, err)
			assert.Equal(t, "---\n"+want.String()+"---\n", got)
		})
	}
}

// fill gives every string under v a value and every slice two elements.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("value")
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range v.Len() {
			fill(v.Index(i))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i))
		}
	default:
		panic("fill: no value for " + v.Type().String())
	}
}
