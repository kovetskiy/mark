package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// LabelQuery selects site labels, labels used in a space, or labels on an object.
// Collection names a v2 resource: spaces, pages, blogposts, attachments, or custom-content.
// An empty query selects site-wide labels.
type LabelQuery struct {
	Space      string
	Collection string
	ID         string
}

// ContentStatus is a Confluence content state, including its numeric API id.
type ContentStatus struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Label collection names select the Confluence v2 object carrying the labels.
const (
	LabelCollectionSpaces        = "spaces"
	LabelCollectionPages         = "pages"
	LabelCollectionBlogposts     = "blogposts"
	LabelCollectionAttachments   = "attachments"
	LabelCollectionCustomContent = "custom-content"
)

// ListLabels reads every label in API order through the Cloud v2 endpoints.
// Missing objects and unsupported endpoints return errors.
func (api *API) ListLabels(ctx context.Context, query LabelQuery) ([]Label, error) {
	path := []string{"labels"}

	switch {
	case query.Space != "":
		id, err := api.GetSpaceID(ctx, query.Space)
		if err != nil {
			return nil, err
		}

		path = []string{LabelCollectionSpaces, id, "content", "labels"}
	case query.Collection != "":
		switch query.Collection {
		case LabelCollectionSpaces, LabelCollectionPages, LabelCollectionBlogposts,
			LabelCollectionAttachments, LabelCollectionCustomContent:
		default:
			return nil, fmt.Errorf("unsupported label collection %q", query.Collection)
		}

		if query.ID == "" {
			return nil, fmt.Errorf("label collection %q requires an id", query.Collection)
		}

		path = []string{query.Collection, query.ID, "labels"}
	}

	type labelV2 struct {
		ID     json.Number `json:"id"`
		Name   string      `json:"name"`
		Prefix string      `json:"prefix"`
	}

	labels := []Label{}

	err := eachV2Required(ctx, api, path, url.Values{"limit": {"250"}}, "list labels (Confluence Cloud endpoint)", func(label labelV2) bool {
		labels = append(labels, Label{ID: label.ID.String(), Name: label.Name, Prefix: label.Prefix})
		return false
	})
	if err != nil {
		return nil, err
	}

	return labels, nil
}

// GetSpaceContentStatuses reads the suggested statuses configured for a Cloud space.
func (api *API) GetSpaceContentStatuses(ctx context.Context, space string) ([]ContentStatus, error) {
	statuses, err := get[[]ContentStatus](ctx, api.v1, []string{"space", space, "state"}, nil)
	if err != nil {
		return nil, requestError("list space statuses (Confluence Cloud endpoint)", err)
	}

	if statuses == nil {
		statuses = []ContentStatus{}
	}

	return statuses, nil
}

// GetPersonalContentStatuses reads custom statuses created by the authenticated Cloud user.
func (api *API) GetPersonalContentStatuses(ctx context.Context) ([]ContentStatus, error) {
	statuses, err := get[[]ContentStatus](ctx, api.v1, []string{"content-states"}, nil)
	if err != nil {
		return nil, requestError("list personal statuses (Confluence Cloud endpoint)", err)
	}

	if statuses == nil {
		statuses = []ContentStatus{}
	}

	return statuses, nil
}

// GetPageContentStatus reads the assigned Cloud page status as a singleton or empty list.
func (api *API) GetPageContentStatus(ctx context.Context, pageID string) ([]ContentStatus, error) {
	type stateResponse struct {
		ContentState *ContentStatus `json:"contentState"`
	}

	state, err := get[stateResponse](ctx, api.v1, []string{"content", pageID, "state"}, nil)
	if err != nil {
		return nil, requestError("read page status (Confluence Cloud endpoint)", err)
	}

	statuses := []ContentStatus{}
	if state.ContentState != nil {
		statuses = append(statuses, *state.ContentState)
	}

	return statuses, nil
}
