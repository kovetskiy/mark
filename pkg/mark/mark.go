package mark

import (
	"strings"

	"github.com/kovetskiy/lorg"
	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/reconquest/karma-go"
)

var (
	logger lorg.Logger = lorg.NewDiscarder()
)

func SetLogger(log lorg.Logger) {
	logger = log
}

func ResolvePage(
	api *confluence.API,
	meta *Meta,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(meta.Space, meta.Title)
	if err != nil {
		return nil, karma.Format(
			err,
			"error during finding page '%s': %s",
			meta.Title,
		)
	}

	ancestry := meta.Parents
	if page != nil {
		ancestry = append(ancestry, page.Title)
	}

	if len(ancestry) > 0 {
		page, err := ValidateAncestry(
			api,
			meta.Space,
			ancestry,
		)
		if err != nil {
			return nil, err
		}

		if page == nil {
			logger.Warningf(
				"page '%s' is not found ",
				meta.Parents[len(ancestry)-1],
			)
		}

		path := meta.Parents
		path = append(path, meta.Title)

		logger.Debugf(
			"resolving page path: ??? > %s",
			strings.Join(path, ` > `),
		)
	}

	parent, err := EnsureAncestry(
		api,
		meta.Space,
		meta.Parents,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"can't create ancestry tree: %s; error: %s",
			strings.Join(meta.Parents, ` > `),
		)
	}

	titles := []string{}
	for _, page := range parent.Ancestors {
		titles = append(titles, page.Title)
	}

	titles = append(titles, parent.Title)

	logger.Infof(
		"page will be stored under path: %s > %s",
		strings.Join(titles, ` > `),
		meta.Title,
	)

	return page, nil
}
