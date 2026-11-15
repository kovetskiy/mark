package confluence_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListLabelsRejectsInvalidCollection keeps invalid queries off the network.
func TestListLabelsRejectsInvalidCollection(t *testing.T) {
	api, server := newAPI(t)

	labels, err := api.ListLabels(context.Background(), confluence.LabelQuery{
		Collection: "unsupported",
		ID:         "123",
	})

	require.ErrorContains(t, err, `unsupported label collection "unsupported"`)
	assert.Nil(t, labels)
	assert.Empty(t, server.Requests())
}

// TestListLabelsRequiresCollectionID keeps incomplete object queries off the network.
func TestListLabelsRequiresCollectionID(t *testing.T) {
	for _, collection := range []string{
		confluence.LabelCollectionSpaces,
		confluence.LabelCollectionPages,
		confluence.LabelCollectionBlogposts,
		confluence.LabelCollectionAttachments,
		confluence.LabelCollectionCustomContent,
	} {
		t.Run(collection, func(t *testing.T) {
			api, server := newAPI(t)

			labels, err := api.ListLabels(context.Background(), confluence.LabelQuery{
				Collection: collection,
			})

			require.ErrorContains(t, err, fmt.Sprintf("label collection %q requires an id", collection))
			assert.Nil(t, labels)
			assert.Empty(t, server.Requests())
		})
	}
}
