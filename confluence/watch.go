package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
)

// IsWatching reports whether user watches a page. An empty user is the
// authenticated one; see watcher for how a name is read.
func (api *API) IsWatching(ctx context.Context, pageID, user string) (bool, error) {
	query, err := api.watcher(ctx, user)
	if err != nil {
		return false, err
	}

	type watching struct {
		Watching bool `json:"watching"`
	}

	result, err := get[watching](ctx, api.v1, []string{"user", "watch", "content", pageID}, query)
	if err != nil {
		return false, requestError("read who watches "+pageID, err)
	}

	return result.Watching, nil
}

// WatchPage makes user watch a page, so that Confluence notifies them of its
// changes. Anyone but the authenticated user takes space administration.
func (api *API) WatchPage(ctx context.Context, pageID, user string) error {
	return api.changeWatch(ctx, http.MethodPost, pageID, user)
}

// UnwatchPage stops user watching a page.
func (api *API) UnwatchPage(ctx context.Context, pageID, user string) error {
	return api.changeWatch(ctx, http.MethodDelete, pageID, user)
}

func (api *API) changeWatch(ctx context.Context, method, pageID, user string) error {
	query, err := api.watcher(ctx, user)
	if err != nil {
		return err
	}

	// Server and Data Center refuse both without it, as a cross-site request.
	header := http.Header{"X-Atlassian-Token": {"no-check"}}

	// Through do rather than send: the answer is a 204 with nothing in it.
	_, err = api.v1.do(ctx, header, method, []string{"user", "watch", "content", pageID}, query, nil, &struct{}{})
	if err != nil {
		return requestError(fmt.Sprintf("change who watches %s", pageID), err)
	}

	return nil
}

// watcher is the query naming user to the watch endpoints, or nil for the
// authenticated user, whom the endpoints act on when named by nobody: that
// needs no administration. Cloud takes an accountId, so a name has to match
// one user exactly, as for restrictions; Server and Data Center take the
// username as it is.
func (api *API) watcher(ctx context.Context, user string) (url.Values, error) {
	if user == "" {
		return nil, nil
	}

	cloud, err := api.Cloud(ctx)
	if err != nil {
		return nil, err
	}

	current, err := api.GetCurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the authenticated user to tell watcher %q apart from it: %w", user, err)
	}

	if !cloud {
		if current.isNamed(user) {
			return nil, nil
		}
		return url.Values{"username": {user}}, nil
	}

	id, err := api.accountIDOf(ctx, "watcher", user, current)
	if err != nil {
		return nil, err
	}
	if id == current.AccountID {
		return nil, nil
	}

	return url.Values{"accountId": {id}}, nil
}

// watchState is whether the authenticated user watched a page before mark
// first wrote to it in the run.
type watchState int

const (
	wasUnwatched watchState = iota
	wasWatched
	// wasUnread is a page whose watch state could not be read, which is left
	// watched rather than unwatched.
	wasUnread
)

// watchBeforeWrite records a page mark is about to write to, and whether the
// authenticated user watched it before mark first wrote to it in this run, for
// UnwatchWritten. Only the first write counts: by the second, mark's own write
// has made them a watcher. Once a read is refused the credentials, the rest
// are recorded unread without asking.
func (api *API) watchBeforeWrite(ctx context.Context, pageID string) {
	if !api.Unwatch {
		return
	}

	api.watchMutex.Lock()
	_, known := api.watchedBefore[pageID]
	refused := api.watchRefused
	api.watchMutex.Unlock()
	if known {
		return
	}
	if refused {
		api.rememberWatch(pageID, wasUnread, nil)
		return
	}

	state := wasUnwatched
	watching, err := api.IsWatching(ctx, pageID, "")
	switch {
	case IsCancellation(err):
		// No answer, and no write follows it under a stopped run.
		return
	case err != nil:
		log.Debug().Err(err).Msgf("unable to read whether page %s is watched, so it is left watched", pageID)
		state = wasUnread
	case watching:
		state = wasWatched
	}

	api.rememberWatch(pageID, state, err)
}

// rememberWatch records a page's state, and err as the first read error,
// unless the page has one already: when two first writes race, the later read
// can see the watch the earlier write made.
func (api *API) rememberWatch(pageID string, state watchState, err error) {
	api.watchMutex.Lock()
	defer api.watchMutex.Unlock()
	if _, known := api.watchedBefore[pageID]; known {
		return
	}
	if api.watchedBefore == nil {
		api.watchedBefore = map[string]watchState{}
	}
	api.watchedBefore[pageID] = state
	if api.watchErr == nil {
		api.watchErr = err
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		api.watchRefused = true
	}
}

// UnwatchWritten stops the authenticated user watching each page mark wrote to
// under Unwatch, once, unless they watched it before. It goes after the last
// write, since any write -- content, a property, an attachment -- can make
// them a watcher again. A failure is only a warning: the writes have been
// made, and the page is no worse than without Unwatch. Once ctx is done, the
// pages not unwatched yet are left watched and named in that warning.
func (api *API) UnwatchWritten(ctx context.Context) {
	api.watchMutex.Lock()
	var pages, unread []string
	for pageID, state := range api.watchedBefore {
		switch state {
		case wasUnwatched:
			pages = append(pages, pageID)
		case wasUnread:
			unread = append(unread, pageID)
		}
	}
	unreadErr, total := api.watchErr, len(api.watchedBefore)
	// A library caller can reuse the API for another run.
	api.watchedBefore, api.watchErr, api.watchRefused = nil, nil, false
	api.watchMutex.Unlock()

	warnStayWatched("unable to read whether %d of %d page(s) mark wrote to were watched before",
		unread, total, unreadErr)

	slices.Sort(pages)
	var failed []string
	var first error
	for i, pageID := range pages {
		if ctx.Err() != nil {
			failed = append(failed, pages[i:]...)
			break
		}
		err := api.unwatchWritten(ctx, pageID)
		if err == nil {
			continue
		}
		log.Debug().Err(err).Msgf("unable to stop watching page %s", pageID)
		failed = append(failed, pageID)
		if first == nil {
			first = err
		}
		// Still unavailable after every retry: the rest would each wait as
		// long again, for a run whose writes are done.
		if isUnavailable(err) {
			failed = append(failed, pages[i+1:]...)
			break
		}
	}
	if err := ctx.Err(); err != nil && len(failed) > 0 && !errors.Is(first, err) {
		first = errors.Join(first, fmt.Errorf("the run was interrupted: %w", err))
	}

	warnStayWatched("unable to stop watching %d of %d page(s) mark wrote to",
		failed, len(pages), first)
}

// warnStayWatched logs one line for each kind of failure, covering every page:
// a refusal that holds for all of them would otherwise read as one page's
// trouble. format takes the count of pages and the total.
func warnStayWatched(format string, pages []string, total int, err error) {
	if len(pages) == 0 {
		return
	}
	slices.Sort(pages)
	log.Warn().Err(err).Msgf(format+", so they stay watched: %s",
		len(pages), total, strings.Join(pages, ", "))
}

// unwatchWritten unwatches a page, sending the request again when Confluence
// was unavailable for it. A page left watched now stays watched: the next run
// takes the watch for the user's own. An unwatch is safe to repeat, so whether
// one landed is not asked: it is sent again either way.
func (api *API) unwatchWritten(ctx context.Context, pageID string) error {
	send := func() (struct{}, error) {
		return struct{}{}, api.UnwatchPage(ctx, pageID, "")
	}
	resend := func() (struct{}, bool, error) {
		return struct{}{}, false, nil
	}

	_, err := retryUnavailable(ctx, api, "stop watching page "+pageID, send, resend)
	return err
}
