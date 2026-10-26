package confluence

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
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

// watchBeforeWrite records a page mark is about to write to, and whether the
// authenticated user watched it before mark first wrote to it in this run, for
// UnwatchWritten. Only the first write counts: by the second, mark's own write
// has made them a watcher.
func (api *API) watchBeforeWrite(pageID string) {
	if !api.Unwatch {
		return
	}

	api.watchMutex.Lock()
	_, known := api.watchedBefore[pageID]
	api.watchMutex.Unlock()
	if known {
		return
	}

	watching, err := api.IsWatching(pageID, "")
	if err != nil {
		// Not knowing, the page is left as it is rather than unwatched.
		// UnwatchWritten reports it with the rest.
		log.Debug().Err(err).Msgf("unable to read whether page %s is watched, so it is left watched", pageID)
		api.watchMutex.Lock()
		api.watchUnread = append(api.watchUnread, pageID)
		if api.watchErr == nil {
			api.watchErr = err
		}
		api.watchMutex.Unlock()
		watching = true
	}

	api.rememberWatching(pageID, watching)
}

// watchCreated records a page mark created, which nobody watched before.
func (api *API) watchCreated(pageID string) {
	if api.Unwatch {
		api.rememberWatching(pageID, false)
	}
}

func (api *API) rememberWatching(pageID string, watching bool) {
	api.watchMutex.Lock()
	defer api.watchMutex.Unlock()
	if api.watchedBefore == nil {
		api.watchedBefore = map[string]bool{}
	}
	api.watchedBefore[pageID] = watching
}

// UnwatchWritten stops the authenticated user watching each page mark wrote to
// under Unwatch, once, unless they watched it before. It goes after the last
// write, since any write -- content, a property, an attachment -- can make
// them a watcher again. A failure is only a warning: the writes have been
// made, and the page is no worse than without Unwatch.
func (api *API) UnwatchWritten() {
	api.watchMutex.Lock()
	var pages []string
	for pageID, watching := range api.watchedBefore {
		if !watching {
			pages = append(pages, pageID)
		}
	}
	unread, unreadErr := api.watchUnread, api.watchErr
	total := len(api.watchedBefore)
	api.watchedBefore, api.watchUnread, api.watchErr = nil, nil, nil
	api.watchMutex.Unlock()

	// One line for each kind of failure, covering every page: a refusal that
	// holds for all of them would otherwise read as one page's trouble.
	if len(unread) > 0 {
		slices.Sort(unread)
		log.Warn().Err(unreadErr).Msgf(
			"unable to read whether %d of %d page(s) mark wrote to were watched before, so they stay watched: %s",
			len(unread), total, strings.Join(unread, ", "),
		)
	}

	slices.Sort(pages)
	var failed []string
	var first error
	for _, pageID := range pages {
		if err := api.unwatchWritten(pageID); err != nil {
			log.Debug().Err(err).Msgf("unable to stop watching page %s", pageID)
			failed = append(failed, pageID)
			if first == nil {
				first = err
			}
		}
	}

	if len(failed) > 0 {
		log.Warn().Err(first).Msgf(
			"unable to stop watching %d of %d page(s) mark wrote to, so they stay watched: %s",
			len(failed), len(pages), strings.Join(failed, ", "),
		)
	}
}

// unwatchWritten unwatches a page, sending the request again when Confluence
// was unavailable for it. A page left watched now stays watched: the next run
// takes the watch for the user's own.
func (api *API) unwatchWritten(pageID string) error {
	send := func() (struct{}, error) {
		return struct{}{}, api.UnwatchPage(pageID, "")
	}
	landed := func() (struct{}, bool, error) {
		watching, err := api.IsWatching(pageID, "")
		return struct{}{}, err == nil && !watching, err
	}

	_, err := retryUnavailable(api, "stop watching page "+pageID, send, landed)
	return err
}
