package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Child is one item directly beneath a page or a folder: a page, a folder, or
// on Cloud whatever else the v2 API lists there, such as a whiteboard.
type Child struct {
	ID    string
	Type  string
	Title string
}

// Children lists what is directly beneath a page or folder, in the order
// Confluence shows it. parentType is "page" or "folder".
//
// On Cloud this is the v2 listing, which is the only one that shows folders.
// Elsewhere it is the v1 page listing: Server and Data Center have no folders,
// so it misses nothing there, and a folder has no children to list.
func (api *API) Children(ctx context.Context, parentID, parentType string) ([]Child, error) {
	if isCloud, err := api.Cloud(ctx); err == nil && isCloud {
		var children []Child
		err := api.eachDirectChild(ctx, parentID, parentType, "list the children of "+parentID,
			func(id, typ, title string) bool {
				children = append(children, Child{ID: id, Type: typ, Title: title})
				return false
			},
		)
		return children, err
	}

	if parentType == "folder" {
		return nil, nil
	}

	pages, err := api.GetChildPages(ctx, parentID)
	if err != nil {
		return nil, err
	}

	children := make([]Child, len(pages))
	for i, page := range pages {
		children[i] = Child{ID: page.ID, Type: "page", Title: page.Title}
	}

	return children, nil
}

// CopyPage copies a page, without its children, under parentID with title,
// and returns the copy. Its attachments, labels and content properties come
// with it; its restrictions do not, so the copy is as open as the parent it
// lands under.
//
// The endpoint is v1's, which Cloud serves. A Confluence that answers it 404
// for a page that exists has no copy endpoint, and is told apart from a page
// that is gone by reading the page.
func (api *API) CopyPage(ctx context.Context, pageID, parentID, title string) (*PageInfo, error) {
	payload := map[string]any{
		"copyAttachments":    true,
		"copyLabels":         true,
		"copyProperties":     true,
		"copyPermissions":    false,
		"copyCustomContents": true,
		"destination": map[string]any{
			"type":  "parent_page",
			"value": parentID,
		},
		"pageTitle": title,
	}

	page, err := send[PageInfo](ctx, api.v1, http.MethodPost, []string{"content", pageID, "copy"}, payload)
	if errors.Is(err, ErrNotFound) {
		if _, readErr := api.GetPageByID(ctx, pageID); readErr == nil {
			return nil, fmt.Errorf(
				"this Confluence has no page copy endpoint (/rest/api/content/{id}/copy); "+
					"Confluence Cloud serves it: %w", err,
			)
		}
	}
	if err != nil {
		return nil, requestError("copy page "+pageID, err)
	}

	return &page, nil
}

// RootPages lists the pages at the top of a space, beneath no other page: the
// home page, and any page moved out from under it.
//
// It is v1's listing, which Cloud and Data Center both serve.
func (api *API) RootPages(ctx context.Context, space string) ([]Child, error) {
	pages, err := listV1[PageInfo](
		ctx, api, []string{"space", space, "content", "page"},
		url.Values{"depth": {"root"}}, 100, "list the top-level pages of space "+space, false, nil,
	)
	if err != nil {
		return nil, err
	}

	children := make([]Child, len(pages))
	for i, page := range pages {
		children[i] = Child{ID: page.ID, Type: "page", Title: page.Title}
	}

	return children, nil
}
