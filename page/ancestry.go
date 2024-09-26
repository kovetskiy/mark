package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/confluence"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

func EnsureAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	var parent *confluence.PageInfo

	rest := ancestry

	for i, title := range ancestry {
		page, err := api.FindPage(space, title, "page")
		if err != nil {
			return nil, karma.Format(
				err,
				`error during finding parent page with title %q`,
				title,
			)
		}

		if page == nil {
			break
		}

		log.Debugf(nil, "parent page %q exists: %s", title, page.Links.Full)

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.FindRootPage(space)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't find root page for space %q",
				space,
			)
		}

		parent = page
	}
	if len(rest) == 0 {
		return parent, nil
	}

	log.Debugf(
		nil,
		"empty pages under %q to be created: %s",
		parent.Title,
		strings.Join(rest, ` > `),
	)

	if !dryRun {
		for _, title := range rest {
			page, err := api.CreatePage(space, "page", parent, title, ``)
			if err != nil {
				return nil, karma.Format(
					err,
					`error during creating parent page with title %q`,
					title,
				)
			}

			parent = page
		}
	} else {
		log.Infof(
			nil,
			"skipping page creation due to enabled dry-run mode, "+
				"need to create %d pages: %v",
			len(rest),
			rest,
		)
	}

	return parent, nil
}

func ValidateAncestry(
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(space, ancestry[len(ancestry)-1], "page")
	if err != nil {
		return nil, err
	}

	if page == nil {
		return nil, nil
	}

	isHomepage := false
	if len(page.Ancestors) < 1 {
		homepage, err := api.FindHomePage(space)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't obtain home page from space %q",
				space,
			)
		}

		if page.ID == homepage.ID {
			log.Debugf(nil, "page is homepage for space %q", space)
			isHomepage = true
		} else {
			return nil, fmt.Errorf(`page %q has no parents`, page.Title)
		}
	}

	if !isHomepage && len(page.Ancestors) < len(ancestry) {
		actual := []string{}
		for _, ancestor := range page.Ancestors {
			actual = append(actual, ancestor.Title)
		}

		valid := false

		if len(actual) == len(ancestry)-1 {
			broken := false
			for i := 0; i < len(actual); i++ {
				if actual[i] != ancestry[i] {
					broken = true
					break
				}
			}

			if !broken {
				if ancestry[len(ancestry)-1] == page.Title {
					valid = true
				}
			}
		}

		if !valid {
			return nil, karma.Describe("title", page.Title).
				Describe("actual", strings.Join(actual, " > ")).
				Describe("expected", strings.Join(ancestry, " > ")).
				Format(nil, "the page has fewer parents than expected")
		}
	}

	for _, parent := range ancestry[:len(ancestry)-1] {
		found := false

		// skipping root article title
		for _, ancestor := range page.Ancestors {
			if ancestor.Title == parent {
				found = true
				break
			}
		}

		if !found {
			list := []string{}

			for _, ancestor := range page.Ancestors {
				list = append(list, ancestor.Title)
			}

			return nil, karma.Describe("expected parent", parent).
				Describe("list", strings.Join(list, "; ")).
				Format(
					nil,
					"unexpected ancestry tree, did not find expected parent page in the tree",
				)
		}
	}

	return page, nil
}
