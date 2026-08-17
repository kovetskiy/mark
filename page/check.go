package page

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LinkCheck says how thoroughly links should be checked.
type LinkCheck string

const (
	// LinkCheckNone leaves links unchecked, which is what mark has always done.
	LinkCheckNone LinkCheck = ""

	// LinkCheckRelative checks links to other files in the repository.
	LinkCheckRelative LinkCheck = "relative-only"

	// LinkCheckAll additionally asks whether external URLs answer.
	LinkCheckAll LinkCheck = "all"
)

// ParseLinkCheck reads the value given to --check-links.
func ParseLinkCheck(value string) (LinkCheck, error) {
	switch LinkCheck(strings.TrimSpace(value)) {
	case LinkCheckNone:
		return LinkCheckNone, nil
	case LinkCheckRelative:
		return LinkCheckRelative, nil
	case LinkCheckAll:
		return LinkCheckAll, nil
	default:
		return LinkCheckNone, fmt.Errorf(
			"unknown --check-links value %q: expected %q or %q",
			value, LinkCheckRelative, LinkCheckAll,
		)
	}
}

// LinkChecker answers whether an external URL resolves.
//
// One is shared by every file in a run, because a URL that appears on twenty
// pages should be requested once. The answers are kept for the same reason:
// a run over a large repository otherwise spends most of its time asking the
// same hosts the same question.
type LinkChecker struct {
	Mode   LinkCheck
	Client *http.Client

	mu   sync.Mutex
	seen map[string]error
}

// NewLinkChecker returns a checker for the given mode.
func NewLinkChecker(mode LinkCheck) *LinkChecker {
	return &LinkChecker{
		Mode: mode,
		Client: &http.Client{
			// Long enough for a slow host, short enough that a hung one does
			// not hold up a publish indefinitely.
			Timeout: 15 * time.Second,
		},
		seen: map[string]error{},
	}
}

// CheckExternal reports whether url answers, or nil if it was not asked.
//
// A HEAD is tried first because the body is of no interest. Plenty of servers
// answer HEAD with 405 or 403 while serving the same URL perfectly well over
// GET, so that answer is not taken at face value and the request is repeated.
func (c *LinkChecker) CheckExternal(url string) error {
	if c == nil || c.Mode != LinkCheckAll {
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
