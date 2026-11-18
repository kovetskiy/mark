package confluence

import (
	"context"
	"fmt"
	"strings"
)

// ResolveContentID resolves a parsed reference, looking up titles and attachment filenames as needed.
// pageType selects pages or blog posts for references that name a space and title.
func (api *API) ResolveContentID(ctx context.Context, ref ContentRef, pageType ContentType) (string, error) {
	if ref.ID != "" {
		return ref.ID, nil
	}
	if ref.ContainerID != "" {
		attachments, err := api.GetAttachments(ctx, ref.ContainerID)
		if err != nil {
			return "", err
		}
		for _, attachment := range attachments {
			if attachment.Filename == ref.Title {
				return strings.TrimPrefix(attachment.ID, "att"), nil
			}
		}
		return "", fmt.Errorf("attachment %q in content %s: %w", ref.Title, ref.ContainerID, ErrNotFound)
	}
	if ref.Title == "" {
		return "", nil
	}
	found, err := api.FindPage(ctx, ref.Space, ref.Title, pageType)
	if err != nil {
		return "", err
	}
	if found == nil {
		return "", fmt.Errorf("no %s titled %q in space %s", pageType, ref.Title, ref.Space)
	}
	return found.ID, nil
}
