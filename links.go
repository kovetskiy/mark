// Reporting links that do not resolve.

package mark

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// reportBrokenLinks says what failed a link check, and decides whether it ends
// the file.
//
// Every one is named, not just the first: a page with six broken links should
// take one run to find out, not six. Whether that is fatal is the caller's
// choice, because adopting --check-links on a repository that has been
// publishing for years wants to see the list before the build starts failing
// over it.
func reportBrokenLinks(broken []string, file string, warnOnly bool) error {
	if len(broken) == 0 {
		return nil
	}

	if warnOnly {
		for _, item := range broken {
			log.Warn().Msgf("%s: %s", file, item)
		}

		return nil
	}

	return unresolvedLinks(broken)
}

// unresolvedLinks is the error a failed link check ends with, or nil when
// every link resolved. One wording for all of them: a file's broken links, the
// ac: links a run left unresolved, and the ones ProcessFile did, which used to
// be the bare list with no count and no sentence in front of it.
func unresolvedLinks(links []string) error {
	if len(links) == 0 {
		return nil
	}

	return fmt.Errorf("%s:\n  %s", pluraliseLinks(len(links)), strings.Join(links, "\n  "))
}

// pluraliseLinks names a count of links in a sentence that reads.
func pluraliseLinks(n int) string {
	if n == 1 {
		return "1 link does not resolve"
	}

	return fmt.Sprintf("%d links do not resolve", n)
}
