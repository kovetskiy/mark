package page_test

import (
	"context"
	"fmt"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/page"
)

// emptySpace is a Confluence space holding no pages at all.
type emptySpace struct{}

func (emptySpace) FindPage(context.Context, string, string, confluence.ContentType) (*confluence.PageInfo, error) {
	return nil, nil
}

// A dry run publishes nothing, so the pages it would create are noted as
// published rather than looked for.
func ExampleLinkChecker_NotePublishes() {
	checker := page.NewLinkChecker(page.LinkChecks{Confluence: true})

	checker.NotePublishes("DOCS", "Guide")
	checker.NotePage("DOCS", "Guide", "intro.md")
	checker.NotePage("DOCS", "Glossary", "intro.md")

	missing, err := checker.MissingPages(context.Background(), emptySpace{})
	if err != nil {
		panic(err)
	}

	for _, link := range missing {
		fmt.Println(link)
	}
	// Output:
	// intro.md: link "ac:Glossary" does not resolve: there is no page "Glossary" in space "DOCS"
}
