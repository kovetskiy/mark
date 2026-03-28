package page

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/rs/zerolog/log"
)

func ResolvePage(
	dryRun bool,
	api *confluence.API,
	meta *metadata.Meta,
) (*confluence.PageInfo, *confluence.PageInfo, error) {
	if meta == nil {
		return nil, nil, fmt.Errorf("metadata is empty")
	}
	page, err := api.FindPage(meta.Space, meta.Title, meta.Type)
	if err != nil {
		return nil, nil, fmt.Errorf("error while finding page %q: %w", meta.Title, err)
	}

	if meta.Type == "blogpost" {
		log.Info().
			Msgf(
				"blog post will be stored as: %s",
				meta.Title,
			)

		return nil, page, nil
	}

	// check to see if home page is in Parents
	homepage, err := api.FindHomePage(meta.Space)
	if err != nil {
		return nil, nil, fmt.Errorf("can't obtain home page from space %q: %w", meta.Space, err)
	}

	skipHomeAncestry := false
	if len(meta.Parents) > 0 {
		if homepage.Title == meta.Parents[0] {
			skipHomeAncestry = true
		}
	}

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
			log.Warn().
				Msgf(
					"page %q is not found ",
					ancestry[len(ancestry)-1],
				)
		}

		path := meta.Parents
		path = append(path, meta.Title)

		log.Debug().
			Msgf(
				"resolving page path: ??? > %s",
				strings.Join(path, ` > `),
			)
	}

	parent, err := EnsureAncestry(
		dryRun,
		api,
		meta.Space,
		meta.Parents,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("can't create ancestry tree %q: %w", strings.Join(meta.Parents, ` > `), err)
	}

	titles := []string{}
	for _, page := range parent.Ancestors {
		titles = append(titles, page.Title)
	}

	titles = append(titles, parent.Title)

	log.Info().Msgf(
		"page will be stored under path: %s > %s",
		strings.Join(titles, ` > `),
		meta.Title,
	)

	return parent, page, nil
}
