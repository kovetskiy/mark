package confluence

import (
	"fmt"
	"net/http"
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
func (api *API) Children(parentID, parentType string) ([]Child, error) {
	if isCloud, err := api.Cloud(); err == nil && isCloud {
		var children []Child
		err := api.eachDirectChild(parentID, parentType, "list the children of "+parentID,
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

	pages, err := api.GetChildPages(parentID)
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
func (api *API) CopyPage(pageID, parentID, title string) (*PageInfo, error) {
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

	request, err := api.v1().Res("content/"+pageID+"/copy", &PageInfo{}).Post(payload)
	if err != nil {
		return nil, newTransportError(request, "copy page "+pageID, err)
	}

	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return request.Response.(*PageInfo), nil
	case http.StatusNotFound:
		if _, readErr := api.GetPageByID(pageID); readErr == nil {
			return nil, fmt.Errorf(
				"this Confluence has no page copy endpoint (/rest/api/content/{id}/copy); "+
					"Confluence Cloud serves it: %w", newErrorStatusNotOK(request),
			)
		}
		return nil, newErrorStatusNotOK(request)
	default:
		return nil, newErrorStatusNotOK(request)
	}
}
