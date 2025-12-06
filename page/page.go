package page

import (
	"strings"

	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/metadata"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
)

func ResolvePage(
	dryRun bool,
	api *confluence.API,
	meta *metadata.Meta,
) (*confluence.PageInfo, *confluence.PageInfo, error) {
	if meta == nil {
		return nil, nil, karma.Format(nil, "metadata is empty")
	}
	page, err := api.FindPage(meta.Space, meta.Title, meta.Type)
	if err != nil {
		return nil, nil, karma.Format(
			err,
			"error while finding page %q",
			meta.Title,
		)
	}

	if meta.Type == "blogpost" {
		log.Infof(
			nil,
			"blog post will be stored as: %s",
			meta.Title,
		)

		return nil, page, nil
	}

	// check to see if home page is in Parents
	homepage, err := api.FindHomePage(meta.Space)
	if err != nil {
		return nil, nil, karma.Format(
			err,
			"can't obtain home page from space %q",
			meta.Space,
		)
	}

	skipHomeAncestry := false
	if len(meta.Parents) > 0 {
		if homepage.Title == meta.Parents[0] {
			skipHomeAncestry = true
		}
	}

	// Handle mixed folder and page hierarchy
	var parent *confluence.PageInfo

	if len(meta.Folders) > 0 {
		// Use new mixed ancestry logic for folders + pages
		ancestry := meta.Parents
		if page != nil && !skipHomeAncestry {
			ancestry = append(ancestry, page.Title)
		}

		if len(ancestry) > 0 {
			// Validate existing page ancestry if it exists
			existingPage, err := ValidateAncestry(
				api,
				meta.Space,
				ancestry,
			)
			if err != nil {
				return nil, nil, err
			}

			if existingPage == nil {
				log.Warningf(
					nil,
					"page %q is not found ",
					ancestry[len(ancestry)-1],
				)
			}
		}

		// Build the complete path for logging
		fullPath := append(meta.Folders, meta.Parents...)
		fullPath = append(fullPath, meta.Title)

		log.Debugf(
			nil,
			"resolving mixed hierarchy path: %s",
			strings.Join(fullPath, ` > `),
		)

		parent, err = EnsureMixedAncestry(
			dryRun,
			api,
			meta.Space,
			meta.Folders,
			meta.Parents,
		)
		if err != nil {
			return nil, nil, karma.Format(
				err,
				"can't create mixed folder/page ancestry tree: folders=%s, pages=%s",
				strings.Join(meta.Folders, ` > `),
				strings.Join(meta.Parents, ` > `),
			)
		}
	} else {
		// Traditional page-only ancestry
		ancestry := meta.Parents
		if page != nil && !skipHomeAncestry {
			ancestry = append(ancestry, page.Title)
		}

		if len(ancestry) > 0 {
			page, err := ValidateAncestry(
				api,
				meta.Space,
				ancestry,
			)
			if err != nil {
				return nil, nil, err
			}

			if page == nil {
				log.Warningf(
					nil,
					"page %q is not found ",
					meta.Parents[len(ancestry)-1],
				)
			}

			path := meta.Parents
			path = append(path, meta.Title)

			log.Debugf(
				nil,
				"resolving page path: ??? > %s",
				strings.Join(path, ` > `),
			)
		}

		parent, err = EnsureAncestry(
			dryRun,
			api,
			meta.Space,
			meta.Parents,
		)
		if err != nil {
			return nil, nil, karma.Format(
				err,
				"can't create ancestry tree: %s",
				strings.Join(meta.Parents, ` > `),
			)
		}
	}

	// Build the display path showing the complete hierarchy
	var displayPath []string

	if len(meta.Folders) > 0 {
		// Show folders first, then page hierarchy
		displayPath = append(displayPath, meta.Folders...)
		if parent != nil {
			// Add page ancestors if any
			for _, ancestor := range parent.Ancestors {
				displayPath = append(displayPath, ancestor.Title)
			}
			displayPath = append(displayPath, parent.Title)
		}
	} else {
		// Traditional page hierarchy
		if parent != nil {
			for _, ancestor := range parent.Ancestors {
				displayPath = append(displayPath, ancestor.Title)
			}
			displayPath = append(displayPath, parent.Title)
		}
	}

	if len(displayPath) > 0 {
		log.Infof(
			nil,
			"page will be stored under path: %s > %s",
			strings.Join(displayPath, ` > `),
			meta.Title,
		)
	} else {
		log.Infof(
			nil,
			"page will be stored at space root: %s",
			meta.Title,
		)
	}

	return parent, page, nil
}
