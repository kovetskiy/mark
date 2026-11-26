package confluence

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ReadPageContentStatus reads the status assigned to a published page or blog post.
func (api *API) ReadPageContentStatus(ctx context.Context, pageID string) (*ContentStatus, error) {
	type stateResponse struct {
		ContentState *ContentStatus `json:"contentState"`
	}

	state, err := get[stateResponse](ctx, api.v1, []string{"content", pageID, "state"}, url.Values{"status": {"current"}})
	return state.ContentState, requestError("read content status of page "+pageID, err)
}

// ResolvePageContentStatus finds a unique name among the destination page's
// available statuses. The ID it returns is what SetPageContentStatus uses.
func (api *API) ResolvePageContentStatus(ctx context.Context, pageID, name string) (*ContentStatus, error) {
	type available struct {
		Space  []ContentStatus `json:"spaceContentStates"`
		Custom []ContentStatus `json:"customContentStates"`
	}
	states, err := get[available](ctx, api.v1, []string{"content", pageID, "state", "available"}, nil)
	if err != nil {
		return nil, requestError("read available content statuses of page "+pageID, err)
	}

	var matched *ContentStatus
	for _, state := range append(states.Space, states.Custom...) {
		if !strings.EqualFold(name, state.Name) {
			continue
		}
		if matched != nil && matched.ID != state.ID {
			return nil, fmt.Errorf("content status %q is ambiguous on page %s", name, pageID)
		}
		matched = &state
	}
	if matched == nil {
		return nil, fmt.Errorf("content status %q is not available on page %s", name, pageID)
	}
	return matched, nil
}

// SetPageContentStatus assigns an existing status, or clears it when status
// is nil. It updates page.Version after confirming the new Confluence version.
func (api *API) SetPageContentStatus(ctx context.Context, page *PageInfo, status *ContentStatus) error {
	method := http.MethodDelete
	var payload any
	if status != nil {
		method = http.MethodPut
		payload = struct {
			ID int64 `json:"id"`
		}{ID: status.ID}
	}
	_, err := api.v1.do(ctx, nil, method, []string{"content", page.ID, "state"},
		url.Values{"status": {"current"}}, payload, &struct{}{})
	if err != nil {
		return requestError("write content status of page "+page.ID, err)
	}

	confirm, release := withGrace(ctx, nil)
	defer release()
	latest, err := api.GetPageByIDExpanded(confirm, page.ID, "version")
	if err != nil {
		return fmt.Errorf("read version after writing content status: %w", err)
	}
	if latest.Version.Number != page.Version.Number+1 {
		return fmt.Errorf("page %s changed while writing content status: %w", page.ID, ErrConflict)
	}

	page.Version = latest.Version
	api.updateCachedPageVersion(page.ID, latest.Version.Number)
	return nil
}
