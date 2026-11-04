package export

import (
	"encoding/json"
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A version's author is a person, human:, unless Cloud says the account is an
// app, which OKF names a process so that it is not trusted as hand-authored.
func TestOKFActor(t *testing.T) {
	for name, test := range map[string]struct {
		by   string
		want string
	}{
		"app":               {`{"type":"known","accountId":"557058:abc","accountType":"app"}`, "process:557058:abc"},
		"cloud person":      {`{"type":"known","accountId":"557058:abc","accountType":"atlassian"}`, "human:557058:abc"},
		"customer":          {`{"accountId":"qm:abc","accountType":"customer"}`, "human:qm:abc"},
		"no account type":   {`{"accountId":"557058:abc"}`, "human:557058:abc"},
		"data center":       {`{"type":"known","username":"jdoe","userKey":"8a7f"}`, "human:jdoe"},
		"user key only":     {`{"userKey":"8a7f"}`, "human:8a7f"},
		"app without an id": {`{"username":"bot","accountType":"app"}`, "process:bot"},
		"nobody named":      {`{}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var user confluence.User
			require.NoError(t, json.Unmarshal([]byte(test.by), &user))
			assert.Equal(t, test.want, okfActor(&user))
		})
	}
	assert.Empty(t, okfActor(nil))
}
