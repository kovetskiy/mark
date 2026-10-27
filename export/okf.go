package export

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kovetskiy/mark/v17/confluence"
	"go.yaml.in/yaml/v3"
)

// Format is what an export writes. The zero value is FormatMark.
type Format string

// The formats an export writes.
const (
	// FormatMark is a document mark publishes back to the page it came from.
	FormatMark Format = "mark"

	// FormatOKF is a concept of the Open Knowledge Format, and a tree a
	// bundle of them, for agents and knowledge catalogs to read. mark does
	// not publish it back.
	FormatOKF Format = "okf"
)

// OKFVersion is the version of the Open Knowledge Format an OKF export
// follows: https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md
const OKFVersion = "0.2"

// okfReferences is the bundle directory an OKF tree keeps attachments in,
// one directory per page, by the OKF convention for material that is not a
// concept of its own.
const okfReferences = "references"

// okfReserved are the names, without .md, that OKF gives a meaning of their
// own in every directory: its index and its log.
var okfReserved = []string{"index", "log"}

// okfReservedFile reports whether a file called name would be read as one of
// them.
func okfReservedFile(name string) bool {
	stem, ok := strings.CutSuffix(strings.ToLower(name), ".md")
	return ok && slices.Contains(okfReserved, stem)
}

// ParseFormat reads the value given to --format.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case "", FormatMark:
		return FormatMark, nil
	case FormatOKF:
		return FormatOKF, nil
	default:
		return "", fmt.Errorf("unknown export format %q: expected %s or %s", value, FormatMark, FormatOKF)
	}
}

// okfConcept is the front matter of an OKF concept, in the order the
// specification lists its keys.
type okfConcept struct {
	Type      string       `yaml:"type"`
	Title     string       `yaml:"title,omitempty"`
	Resource  string       `yaml:"resource,omitempty"`
	Tags      []string     `yaml:"tags,omitempty"`
	Generated okfGenerated `yaml:"generated"`
	Sources   []okfSource  `yaml:"sources,omitempty"`
}

// okfGenerated names mark as what produced the concept. It has no at: the
// page's last edit is a person's, and stays with its author in sources, and
// the export time would make every export of an unchanged page differ.
type okfGenerated struct {
	By string `yaml:"by"`
}

type okfSource struct {
	ID           string `yaml:"id"`
	Resource     string `yaml:"resource"`
	Title        string `yaml:"title,omitempty"`
	Author       string `yaml:"author,omitempty"`
	LastModified string `yaml:"last_modified,omitempty"`
}

// okfFrontMatter is the front matter of the concept a page is exported as:
// what kind of page it is, its title and address, its labels as tags, and the
// page as the concept's one source, with who last edited it and when.
func okfFrontMatter(page *confluence.PageInfo, resource, generator string, labels []string) (string, error) {
	kind := "Confluence Page"
	if page.Type == "blogpost" {
		kind = "Confluence Blog Post"
	}

	modified := okfTime(page.Version.When)

	concept := okfConcept{
		Type:      kind,
		Title:     page.Title,
		Resource:  resource,
		Tags:      labels,
		Generated: okfGenerated{By: generator},
		Sources: []okfSource{{
			ID:           "confluence",
			Resource:     resource,
			Title:        page.Title,
			Author:       okfActor(page.Version.By),
			LastModified: modified,
		}},
	}
	if resource == "" {
		concept.Sources = nil
	}

	var b bytes.Buffer
	b.WriteString("---\n")
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(concept); err != nil {
		return "", fmt.Errorf("unable to write the front matter of page %s: %w", page.ID, err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("unable to write the front matter of page %s: %w", page.ID, err)
	}
	b.WriteString("---\n")

	return b.String(), nil
}

// okfTime is a Confluence timestamp in the UTC form OKF asks for, or "" when
// there is none or it does not parse.
func okfTime(value string) string {
	when, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}
	return when.UTC().Format(time.RFC3339)
}

// okfActor names the user who wrote a version in OKF's actor convention:
// human:<username> on Server and Data Center, human:<accountId> on Cloud,
// which has no usernames, and process:<accountId> for a Cloud app, since
// consumers trust human: as hand-authored. v2 gives only the author's id, so
// through the gateway an app's edit still reads as human.
func okfActor(user *confluence.User) string {
	if user == nil {
		return ""
	}
	kind := "human:"
	if user.AccountType == "app" {
		kind = "process:"
	}
	for _, id := range []string{user.Username, user.AccountID, user.UserKey} {
		if id != "" {
			return kind + id
		}
	}
	return ""
}

// pageURL is the address a browser shows the page at.
func pageURL(api *confluence.API, page *confluence.PageInfo) string {
	webui := page.Links.Full
	if webui == "" || strings.Contains(webui, "://") {
		return webui
	}

	base := page.Links.Base
	if !strings.HasPrefix(base, "http") {
		base = api.BaseURL
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(webui, "/")
}

// okfIndexes are the index.md files of an OKF bundle, by path: one in every
// directory that holds a concept, listing its concepts and then the
// directories beneath it that hold more, each in the order Confluence lists
// the pages. The bundle's own declares the OKF version.
func okfIndexes(root, heading string, plan []planned) map[string]string {
	// The planned paths are clean, and root has to compare equal to them.
	root = filepath.Clean(root)

	type entry struct{ title, target string }
	// The bundle declares its version even with nothing in it.
	listed := map[string][]entry{root: nil}
	titleOf := map[string]string{}

	for _, p := range plan {
		dir := filepath.Dir(p.output)
		listed[dir] = append(listed[dir], entry{p.title, filepath.Base(p.output)})
		// A page's children are in the directory named like its document.
		titleOf[strings.TrimSuffix(p.output, ".md")] = p.title
	}

	for _, p := range plan {
		below := strings.TrimSuffix(p.output, ".md")
		if _, ok := listed[below]; ok {
			dir := filepath.Dir(p.output)
			listed[dir] = append(listed[dir], entry{"Pages beneath " + p.title, filepath.Base(below) + "/"})
		}
	}

	indexes := make(map[string]string, len(listed))
	for dir, entries := range listed {
		var b strings.Builder
		title := titleOf[dir]
		if dir == root {
			fmt.Fprintf(&b, "---\nokf_version: %q\n---\n\n", OKFVersion)
			title = heading
		}
		fmt.Fprintf(&b, "# %s\n\n", escapeClosingHashes(indexText(title)))
		for _, e := range entries {
			fmt.Fprintf(&b, "* [%s](%s)\n", indexText(e.title), pathDestination(e.target))
		}
		indexes[filepath.Join(dir, "index.md")] = b.String()
	}

	return indexes
}

// indexText is a title as the text of an index entry or heading: on one line,
// with anything Markdown would read as markup escaped.
func indexText(title string) string {
	return escapeInline(strings.Join(strings.Fields(title), " "), inlineOptions{})
}

// okfName is name, as an OKF attachment is written: an attachment ending in
// .md would be read as a concept that has no front matter, so it gets .txt
// after it.
func okfName(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".md") {
		return name + ".txt"
	}
	return name
}
