package confluence_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateAttachmentRejectsEmptyResponse: a 2xx whose body is `null` or `{}`
// decodes cleanly into the short single-attachment form, and UpdateAttachment
// used to hand back that zero AttachmentInfo with a nil error. The caller then
// recorded an attachment with no id or download link and nothing reported it.
// CreateAttachment already rejects a response with no results.
func TestUpdateAttachmentRejectsEmptyResponse(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"results":[]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			api := confluence.NewAPI(server.URL, "user", "token", false)

			info, err := api.UpdateAttachment(
				"123", "att456", "diagram.png", "mark:checksum: abc", bytes.NewReader([]byte("v2")),
			)
			require.Error(t, err)
			assert.Empty(t, info.ID)
		})
	}
}

func TestUpdateAttachmentAcceptsShortResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"att456","title":"diagram.png","_links":{"download":"/download/attachments/123/diagram.png"}}`))
	}))
	defer server.Close()

	api := confluence.NewAPI(server.URL, "user", "token", false)

	info, err := api.UpdateAttachment(
		"123", "att456", "diagram.png", "mark:checksum: abc", bytes.NewReader([]byte("v2")),
	)
	require.NoError(t, err)
	assert.Equal(t, "att456", info.ID)
	assert.Equal(t, "diagram.png", info.Filename)
}
