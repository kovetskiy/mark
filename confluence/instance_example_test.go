package confluence_test

import (
	"errors"
	"fmt"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/export"
)

// A caller given a page URL checks it is on the configured instance before
// asking that instance, with its credentials, for the page.
func ExampleSameInstance() {
	const configured = "https://example.atlassian.net/wiki"

	for _, pageURL := range []string{
		"https://example.atlassian.net/wiki/spaces/DOCS/pages/123/Title",
		"https://example.atlassian.net.evil.example/wiki/spaces/DOCS/pages/123/Title",
		"http://example.atlassian.net/wiki/spaces/DOCS/pages/123/Title",
	} {
		ref, err := export.ParsePageURL(pageURL)
		if err != nil {
			fmt.Println(err)
			continue
		}

		err = confluence.SameInstance(configured, ref.BaseURL)
		var mismatch *confluence.InstanceMismatchError
		switch {
		case err == nil:
			fmt.Println("read page", ref.PageID, "from", configured)
		case errors.As(err, &mismatch):
			fmt.Println("refused:", mismatch.Reason)
		default:
			fmt.Println("refused:", err)
		}
	}

	// Output:
	// read page 123 from https://example.atlassian.net/wiki
	// refused: the host example.atlassian.net.evil.example is not example.atlassian.net
	// refused: the scheme http is not https
}
