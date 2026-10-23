package confluence

import (
	"fmt"
	"net/http"
)

// IsWatching reports whether user watches a page. An empty user is the
// authenticated one; see watcher for how a name is read.
func (api *API) IsWatching(pageID, user string) (bool, error) {
	query, err := api.watcher(user)
	if err != nil {
		return false, err
	}

	var result struct {
		Watching bool `json:"watching"`
	}

	request, err := api.v1().Res("user/watch/content/"+pageID, &result).Get(query)
	if err != nil {
		return false, newTransportError(request, "read who watches "+pageID, err)
	}
	if request.Raw.StatusCode != http.StatusOK {
		return false, newErrorStatusNotOK(request)
	}

	return result.Watching, nil
}

// WatchPage makes user watch a page, so that Confluence notifies them of its
// changes. Anyone but the authenticated user takes space administration.
func (api *API) WatchPage(pageID, user string) error {
	return api.changeWatch(http.MethodPost, pageID, user)
}

// UnwatchPage stops user watching a page.
func (api *API) UnwatchPage(pageID, user string) error {
	return api.changeWatch(http.MethodDelete, pageID, user)
}

func (api *API) changeWatch(method, pageID, user string) error {
	query, err := api.watcher(user)
	if err != nil {
		return err
	}

	resource := api.v1().Res("user/watch/content/"+pageID, &struct{}{})
	resource.SetQuery(query)
	// Server and Data Center refuse both without it, as a cross-site request.
	resource.SetHeader("X-Atlassian-Token", "no-check")

	send := resource.Post
	if method == http.MethodDelete {
		send = resource.Delete
	}
	request, err := send()
	if err != nil {
		return newTransportError(request, fmt.Sprintf("change who watches %s", pageID), err)
	}

	switch request.Raw.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	default:
		return newErrorStatusNotOK(request)
	}
}

// watcher is the query naming user to the watch endpoints, or nil for the
// authenticated user, whom the endpoints act on when named by nobody: that
// needs no administration. Cloud takes an accountId, so a name has to match
// one user exactly, as for restrictions; Server and Data Center take the
// username as it is.
func (api *API) watcher(user string) (map[string]string, error) {
	if user == "" {
		return nil, nil
	}

	cloud, err := api.Cloud()
	if err != nil {
		return nil, err
	}

	current, err := api.GetCurrentUser()
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the authenticated user to tell watcher %q apart from it: %w", user, err)
	}

	if !cloud {
		if current.isNamed(user) {
			return nil, nil
		}
		return map[string]string{"username": user}, nil
	}

	id, err := api.accountIDOf("watcher", user, current)
	if err != nil {
		return nil, err
	}
	if id == current.AccountID {
		return nil, nil
	}

	return map[string]string{"accountId": id}, nil
}
