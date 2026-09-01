package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kovetskiy/gopencils"
)

// Property is a key/value pair Confluence stores against a space or a page.
//
// The value is held as raw JSON because callers own its shape; this package
// stores and returns bytes without interpreting them.
//
// Version matters: Confluence versions properties, and an update must name the
// version it supersedes. A stale one draws a 409, which is how two concurrent
// writers find out about each other instead of silently overwriting.
type Property struct {
	ID      string          `json:"id"`
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

// propertyPageSize is requested when listing properties.
//
// One page is never enough on its own, whatever the caller holds. Properties
// belong to the object, not to the app that wrote them: on Server and Data
// Center every one of mark's manifest shards is a content property of the space
// homepage, sitting next to `editor`, `content-appearance-published`, the
// emoji-title keys and whatever else any installed app has put there. Once the
// total passes a page and a shard falls off the end, the caller sees no
// property, treats every mapping it held as absent, and its next write POSTs a
// key that already exists. Both listings therefore read to the end of the
// collection.
const propertyPageSize = 100

// ErrPropertyConflict reports that a property write lost a race with another
// writer. It is separate from a generic error so a caller can re-read and retry
// on exactly this case and nothing else.
var ErrPropertyConflict = errors.New("property version conflict")

// ErrPropertyUnseen reports that a create collided with a property the listing
// never showed.
//
// This is what an incomplete listing looks like from the write side: the key is
// there, this run did not see it, and everything it held has already been
// treated as absent. The diagnosis a plain conflict gets -- "updated by a
// concurrent run" -- is wrong here and worth saying differently.
//
// Deliberately not an ErrPropertyConflict: a caller that shrugs this off the
// way it shrugs off a lost race loses the same data again on every run
// afterwards. It is still not a reason to abandon the writes that would have
// succeeded -- see how the manifest handles it.
var ErrPropertyUnseen = errors.New("property already exists but was not listed")

// ListSpaceProperties returns every property stored against a space.
//
// Space properties exist only in the v2 API, which means Cloud only. Server and
// Data Center have no equivalent; see ListContentProperties for what stands in
// there.
//
// The whole collection is fetched rather than one key at a time because a
// caller holding several related properties would otherwise pay a request per
// key. A space with none is not an error: the first run of anything that stores
// state this way finds nothing, and that is the normal case.
func (api *API) ListSpaceProperties(spaceID string) ([]Property, error) {
	var all []Property
	var cursor string

	for {
		var result struct {
			Results []Property `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		query := map[string]string{"limit": strconv.Itoa(propertyPageSize)}
		if cursor != "" {
			query["cursor"] = cursor
		}

		request, err := api.v2().
			Res("spaces").
			Res(spaceID).
			Res("properties", &result).
			Get(query)
		if err != nil {
			return nil, newTransportError(request, "read properties of space "+spaceID, err)
		}

		// A space with no properties at all answers 404 rather than an empty
		// collection, so both shapes have to mean "none set". First page only:
		// a 404 partway through is a real failure, and reading it as "none"
		// would throw away the pages already in hand.
		if request.Raw.StatusCode == http.StatusNotFound && cursor == "" {
			return nil, nil
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Results...)

		next := nextCursor(result.Links.Next)
		// A server that hands back the cursor it was given would otherwise
		// keep this loop going for as long as it keeps answering.
		if next == "" || next == cursor {
			break
		}
		cursor = next
	}

	return all, nil
}

// nextCursor pulls the cursor out of a v2 _links.next.
//
// v2 paginates by an opaque cursor rather than by offset, and names the next
// page as a whole URL -- relative on some deployments, absolute on others -- of
// which the cursor is the only part worth reusing. Rebuilding the request
// around it rather than following the link keeps this client's base URL and
// headers, which is what makes the loop work through the scoped-token gateway
// as well as against a tenant directly.
func nextCursor(next string) string {
	if next == "" {
		return ""
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("cursor")
}

// SetSpaceProperty writes value to a space property, creating it if absent.
func (api *API) SetSpaceProperty(spaceID, key string, value []byte, existing *Property) error {
	payload := map[string]any{
		"key":   key,
		"value": json.RawMessage(value),
	}

	var (
		result  Property
		request *gopencils.Resource
		err     error
	)

	space := api.v2().Res("spaces").Res(spaceID)

	if existing == nil {
		// The result pointer goes on the collection resource itself: routing it
		// through a further Res("") would append a trailing slash, which this
		// API is not reliably forgiving about.
		request, err = space.Res("properties", &result).Post(payload)
	} else {
		// v2 addresses an update by property id.
		payload["version"] = map[string]any{"number": existing.Version.Number + 1}
		request, err = space.Res("properties").Res(existing.ID, &result).Put(payload)
	}
	if err != nil {
		return newTransportError(
			request, fmt.Sprintf("write property %q of space %s", key, spaceID), err,
		)
	}

	return propertyWriteResult(request, key, "space "+spaceID, existing == nil)
}

// ListContentProperties returns every property stored against a page.
//
// Unlike space properties this is a v1 endpoint, present on Server and Data
// Center as well as Cloud, which is what makes it usable as the storage of last
// resort on a non-Cloud instance.
func (api *API) ListContentProperties(contentID string) ([]Property, error) {
	var all []Property
	start := 0

	for {
		var result struct {
			Results []Property `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		request, err := api.v1().
			Res("content").
			Res(contentID).
			Res("property", &result).
			Get(map[string]string{
				"limit": strconv.Itoa(propertyPageSize),
				"start": strconv.Itoa(start),
			})
		if err != nil {
			return nil, newTransportError(request, "read properties of content "+contentID, err)
		}

		// Only the first page may read 404 as "none set"; one partway through
		// is a failure, and swallowing it would discard the pages already read.
		if request.Raw.StatusCode == http.StatusNotFound && start == 0 {
			return nil, nil
		}

		if request.Raw.StatusCode != http.StatusOK {
			return nil, newErrorStatusNotOK(request)
		}

		all = append(all, result.Results...)

		// Two ways a listing says it is finished, and a deployment may use
		// either. Confluence caps limit at its max-results setting and answers
		// a capped request with a short page *and* a next link, so a short page
		// alone does not mean the end -- that is how the tail of a listing was
		// being thrown away, silently, on exactly the instances configured to
		// hand out less than they were asked for. Other deployments omit the
		// link entirely and only ever go short.
		//
		// So: keep going while either says there is more, and stop on an empty
		// page whatever they say, which is what keeps a server that does
		// neither from being asked for the same offset forever.
		if len(result.Results) == 0 ||
			(result.Links.Next == "" && len(result.Results) < propertyPageSize) {
			break
		}
		start += len(result.Results)
	}

	return all, nil
}

// SetContentProperty writes value to a page property, creating it if absent.
func (api *API) SetContentProperty(contentID, key string, value []byte, existing *Property) error {
	payload := map[string]any{
		"key":   key,
		"value": json.RawMessage(value),
	}

	var (
		result  Property
		request *gopencils.Resource
		err     error
	)

	content := api.v1().Res("content").Res(contentID)

	if existing == nil {
		request, err = content.Res("property", &result).Post(payload)
	} else {
		// v1 addresses an update by key in the path, but still wants the
		// property id in the body, and the version being moved to rather than
		// the one being replaced. Atlassian documents both as required; omitting
		// the id is the kind of thing a fake will happily accept and a real
		// instance will not.
		payload["id"] = existing.ID
		payload["version"] = map[string]any{"number": existing.Version.Number + 1}
		request, err = content.Res("property").Res(key, &result).Put(payload)
	}
	if err != nil {
		return newTransportError(
			request, fmt.Sprintf("write property %q of content %s", key, contentID), err,
		)
	}

	return propertyWriteResult(request, key, "content "+contentID, existing == nil)
}

// propertyWriteResult turns a property write response into an error, singling
// out the version conflict so a caller can react to losing a race and nothing
// else.
//
// creating says whether the caller believed the key was absent, which is what
// tells the two ways a 409 arrives apart. Losing a race to another writer is
// ordinary and survivable. Colliding with a key that was there the whole time
// is neither: it means the listing this run worked from was incomplete, so
// whatever that property held has already been treated as missing.
func propertyWriteResult(request *gopencils.Resource, key, subject string, creating bool) error {
	switch request.Raw.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusConflict:
		if creating {
			return fmt.Errorf(
				"property %q of %s already exists but was not in the listing this run read, "+
					"so anything it held has been treated as absent: %w",
				key, subject, ErrPropertyUnseen,
			)
		}
		return fmt.Errorf(
			"property %q of %s was modified concurrently: %w",
			key, subject, ErrPropertyConflict,
		)
	default:
		return newErrorStatusNotOK(request)
	}
}
