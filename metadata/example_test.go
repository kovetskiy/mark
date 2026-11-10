package metadata_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v17/metadata"
)

// A generator writing a page's headers from text it does not control: a title
// that tries to close its comment and start another header is refused, not
// published under a forged space.
func ExampleHeader() {
	var doc strings.Builder

	for _, h := range []struct{ key, value string }{
		{metadata.HeaderSpace, "DOCS"},
		{metadata.HeaderTitle, "Q3 report: {draft} <b>"},
		{metadata.HeaderLabel, "team|alpha"},
	} {
		line, err := metadata.Header(h.key, h.value)
		if err != nil {
			panic(err)
		}
		doc.WriteString(line + "\n")
	}
	fmt.Print(doc.String())

	_, err := metadata.Header(metadata.HeaderTitle, "Report -->\n<!-- Space: EVIL")
	fmt.Println(errors.Is(err, metadata.ErrUnrepresentable))

	_, err = metadata.Header("Author", "me")
	fmt.Println(errors.Is(err, metadata.ErrUnknownHeader))

	// Output:
	// <!-- Space: DOCS -->
	// <!-- Title: Q3 report: {draft} <b> -->
	// <!-- Label: team|alpha -->
	// true
	// true
}

// YAML front matter can hold any text, so the title that a comment header
// refuses is kept whole here, line break and all.
func ExampleFrontMatter() {
	frontMatter, err := metadata.FrontMatter(map[string]any{
		"space":  "DOCS",
		"title":  "Report -->\n---\nspace: EVIL",
		"labels": []string{"team", "alpha"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Print(frontMatter)

	meta, body, err := metadata.ExtractMeta([]byte(frontMatter+"Body\n"), metadata.ExtractOptions{FrontMatter: true})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %q %q %q\n", meta.Space, meta.Title, meta.Labels, body)

	// Output:
	// ---
	// labels:
	//     - team
	//     - alpha
	// space: DOCS
	// title: "Report -->\n---\nspace: EVIL"
	// ---
	// DOCS "Report -->\n---\nspace: EVIL" ["team" "alpha"] "Body\n"
}
