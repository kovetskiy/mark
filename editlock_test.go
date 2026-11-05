package mark

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/confluence/confluencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublisherEditLockUsesAPILogin: a Publisher is handed its api, so the
// edit lock leaves the page to whoever that api logs in as, not to a username
// given anywhere else.
func TestPublisherEditLockUsesAPILogin(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dataCenter bool
		want       string
	}{
		{name: "server", dataCenter: true, want: "publisher"},
		{name: "cloud", want: "acct-current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := confluencetest.New(t)
			if tc.dataCenter {
				server.SetDataCenter()
			}
			home := server.AddPage("DOCS", "Home", "page", "")
			server.SetHomepage("DOCS", home.ID)
			server.AddPage("DOCS", "Parent", "page", home.ID)

			file := writeFile(t, t.TempDir(), "doc.md", markdownWithTitle("Locked"))

			api := confluence.NewAPI(server.URL, confluence.Options{Username: "publisher", Password: "token"})
			config := publishConfig(server.URL, filepath.Dir(file)).PublishConfig
			config.Update.EditLock = true

			publisher, err := NewPublisher(context.Background(), api, config)
			require.NoError(t, err)
			defer publisher.Close()

			target, err := publisher.Publish(context.Background(), file)
			require.NoError(t, err)
			require.NotNil(t, target)

			assert.Equal(t, []string{tc.want}, server.Page(target.ID).UpdateRestrictedTo)
		})
	}
}
