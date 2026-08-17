package page

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kovetskiy/mark/v16/confluence"
)

// The kinds of link --check-links understands.
const (
	// CheckInternal is a relative link to another Markdown file in the
	// repository.
	CheckInternal = "internal"

	// CheckConfluence is an ac: link, which names a Confluence page by title
	// rather than by path.
	CheckConfluence = "confluence"

	// CheckExternal is a link with a scheme, somewhere off this Confluence
	// entirely.
	CheckExternal = "external"

	// CheckAll is shorthand for the three above.
	CheckAll = "all"
)

// LinkChecks is the set of link kinds a run was asked to check.
//
// A set rather than a single mode because the three cost wildly different
// things. Internal links are answered from the filesystem, Confluence links
// cost a lookup each, and external ones leave the building entirely -- so a
// repository will often want the first two in CI and none of the third.
type LinkChecks struct {
	Internal   bool
	Confluence bool
	External   bool
}

// Any reports whether anything is being checked at all.
func (c LinkChecks) Any() bool {
	return c.Internal || c.Confluence || c.External
}

// ParseLinkChecks reads the values given to --check-links.
func ParseLinkChecks(values []string) (LinkChecks, error) {
	var checks LinkChecks

	for _, value := range values {
		for _, name := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "":
				continue
			case CheckInternal:
				checks.Internal = true
			case CheckConfluence:
				checks.Confluence = true
			case CheckExternal:
				checks.External = true
			case CheckAll:
				checks.Internal = true
				checks.Confluence = true
				checks.External = true
			default:
				return LinkChecks{}, fmt.Errorf(
					"unknown --check-links value %q: expected %s, %s, %s or %s",
					name, CheckInternal, CheckConfluence, CheckExternal, CheckAll,
				)
			}
		}
	}

	return checks, nil
}

// LinkChecker answers whether an external URL resolves.
//
// One is shared by every file in a run, because a URL that appears on twenty
// pages should be requested once. The answers are kept for the same reason:
// a run over a large repository otherwise spends most of its time asking the
// same hosts the same question.
type LinkChecker struct {
	Checks LinkChecks
	Client *http.Client

	mu      sync.Mutex
	seen    map[string]error
	pending map[string]pendingPage
}

// pendingPage is an ac: link waiting to be checked, and the first document that
// asked about it.
type pendingPage struct {
	space  string
	title  string
	source string
}

// NewLinkChecker returns a checker for the given set.
func NewLinkChecker(checks LinkChecks) *LinkChecker {
	return &LinkChecker{
		Checks: checks,
		Client: &http.Client{
			// Long enough for a slow host, short enough that a hung one does
			// not hold up a publish indefinitely.
			Timeout: 15 * time.Second,
		},
		seen:    map[string]error{},
		pending: map[string]pendingPage{},
	}
}

// NotePage records an ac: link to check once the run has finished.
//
// Deferred rather than looked up on the spot because a page named here may be
// published later in the same run, by a file that sorts after this one.
// Checking as each document compiles would make the answer depend on the order
// the files happen to be in, and a first run over a set of pages that link to
// each other would fail on roughly half of them.
func (c *LinkChecker) NotePage(space, title, source string) {
	if c == nil || !c.Checks.Confluence {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Keyed on the page, so a title linked from twenty documents costs one
	// lookup and is reported once.
	key := space + "\x00" + title
	if _, ok := c.pending[key]; !ok {
		c.pending[key] = pendingPage{space: space, title: title, source: source}
	}
}

// MissingPages reports the ac: links that named no page, once everything that
// was going to be published has been.
func (c *LinkChecker) MissingPages(finder PageFinder) ([]string, error) {
	if c == nil || len(c.pending) == 0 {
		return nil, nil
	}

	c.mu.Lock()
	keys := make([]string, 0, len(c.pending))
	for key := range c.pending {
		keys = append(keys, key)
	}
	pending := c.pending
	c.mu.Unlock()

	sort.Strings(keys)

	var missing []string
	for _, key := range keys {
		link := pending[key]

		found, err := finder.FindPage(link.space, link.title, "page")
		if err != nil {
			return nil, fmt.Errorf("find confluence page %q: %w", link.title, err)
		}

		if found == nil {
			missing = append(missing, fmt.Sprintf(
				"%s: link \"ac:%s\" does not resolve: there is no page %q in space %q",
				link.source, link.title, link.title, link.space,
			))
		}
	}

	return missing, nil
}

// PageFinder is the part of the Confluence API this needs, kept narrow so the
// check can be exercised without one.
type PageFinder interface {
	FindPage(space, title, pageType string) (*confluence.PageInfo, error)
}

// CheckExternal reports whether url answers, or nil if it was not asked.
//
// A HEAD is tried first because the body is of no interest. Plenty of servers
// answer HEAD with 405 or 403 while serving the same URL perfectly well over
// GET, so that answer is not taken at face value and the request is repeated.
func (c *LinkChecker) CheckExternal(url string) error {
	if c == nil || !c.Checks.External {
		return nil
	}

	c.mu.Lock()
	if err, ok := c.seen[url]; ok {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	err := c.request(http.MethodHead, url)
	if err != nil {
		if getErr := c.request(http.MethodGet, url); getErr == nil {
			err = nil
		} else {
			err = getErr
		}
	}

	c.mu.Lock()
	c.seen[url] = err
	c.mu.Unlock()

	return err
}

func (c *LinkChecker) request(method, url string) error {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return fmt.Errorf("not a usable URL: %w", err)
	}

	// Some hosts answer a request without a user agent with 403.
	req.Header.Set("User-Agent", "mark link checker")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("answered %s", resp.Status)
	}

	return nil
}
