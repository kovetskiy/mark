package confluence_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttachmentUploadAnswers: the attachment comes from the listing form or,
// for an update, from the short form, and an answer naming no attachment is an
// error, since the caller links the page's attachments from it.
func TestAttachmentUploadAnswers(t *testing.T) {
	upload := map[string]func(*confluence.API) (confluence.AttachmentInfo, error){
		"create": func(api *confluence.API) (confluence.AttachmentInfo, error) {
			return api.CreateAttachment(context.Background(), "123", "diagram.png", "c", bytes.NewReader([]byte("v1")))
		},
		"update": func(api *confluence.API) (confluence.AttachmentInfo, error) {
			return api.UpdateAttachment(context.Background(), "123", "att456", "diagram.png", "c", bytes.NewReader([]byte("v2")))
		},
	}

	listing := `{"_links":{"context":"/wiki"},"results":[{"id":"att456","title":"diagram.png"}]}`
	short := `{"id":"att456","title":"diagram.png"}`

	for _, tc := range []struct {
		op, body, wantID, wantContext string
	}{
		{op: "create", body: listing, wantID: "att456", wantContext: "/wiki"},
		{op: "update", body: listing, wantID: "att456", wantContext: "/wiki"},
		{op: "update", body: short, wantID: "att456"},
		{op: "create", body: `{"results":[]}`},
		{op: "create", body: `{"results":[{}]}`},
		{op: "update", body: `null`},
		{op: "update", body: `{}`},
		{op: "update", body: `{"results":[]}`},
		{op: "update", body: `{"results":[{}]}`},
	} {
		t.Run(tc.op+" "+tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			info, err := upload[tc.op](confluence.NewAPI(server.URL, "user", "token", false))
			if tc.wantID == "" {
				require.Error(t, err)
				assert.Empty(t, info.ID)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantID, info.ID)
			assert.Equal(t, "diagram.png", info.Filename)
			assert.Equal(t, tc.wantContext, info.Links.Context)
		})
	}
}
