package confluence

import (
	"context"
	"errors"
	"net/url"
)

// Operation is something Confluence lets the authenticated user do: "create"
// with a TargetType of "page", "administer" a "space".
type Operation struct {
	Operation  string `json:"operation"`
	TargetType string `json:"targetType"`
}

// ErrNoOperations reports a Confluence that answered for a space without
// saying what the user may do in it.
var ErrNoOperations = errors.New("this Confluence does not say what the account may do in a space")

// SpaceOperations returns a space's name, where the API gives it, and what the
// authenticated user may do in it. A space that is not there, or that the user
// cannot see, is ErrNotFound: Confluence answers both the same way.
func (api *API) SpaceOperations(ctx context.Context, key string) (string, []Operation, error) {
	if api.gateway {
		return api.spaceOperationsV2(ctx, key)
	}

	type spaceOperations struct {
		Name       string       `json:"name"`
		Operations *[]Operation `json:"operations"`
	}

	space, err := get[spaceOperations](ctx, api.v1, []string{"space", key}, url.Values{"expand": {"operations"}})
	if err != nil {
		return "", nil, requestError("read space "+key, err)
	}
	if space.Operations == nil {
		return space.Name, nil, ErrNoOperations
	}

	return space.Name, *space.Operations, nil
}

// spaceOperationsV2 is SpaceOperations through v2, which names no space: the
// operations endpoint returns them alone, so the name is left empty.
func (api *API) spaceOperationsV2(ctx context.Context, key string) (string, []Operation, error) {
	id, err := api.GetSpaceID(ctx, key)
	if err != nil {
		return "", nil, err
	}

	type spaceOperations struct {
		Operations *[]Operation `json:"operations"`
	}

	permitted, err := get[spaceOperations](ctx, api.v2, []string{"spaces", id, "operations"}, nil)
	if err != nil {
		return "", nil, requestError("read what may be done in space "+key, err)
	}
	if permitted.Operations == nil {
		return "", nil, ErrNoOperations
	}

	return "", *permitted.Operations, nil
}
