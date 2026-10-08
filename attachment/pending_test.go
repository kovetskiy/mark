package attachment

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func remoteWith(name, id, checksum string) confluence.AttachmentInfo {
	r := confluence.AttachmentInfo{Filename: name, ID: id}
	r.Metadata.Comment = AttachmentChecksumPrefix + checksum
	r.Links.Context = "/wiki"
	r.Links.Download = "/download/attachments/1/" + name

	return r
}

// TestPendingSplitsWhatARealRunWouldKeepFromWhatItWouldSend: the dry run asks
// the same question a real run does, and gets the link of every file it keeps.
func TestPendingSplitsWhatARealRunWouldKeepFromWhatItWouldSend(t *testing.T) {
	remotes := []confluence.AttachmentInfo{
		remoteWith("same.png", "1", "aaa"),
		remoteWith("changed.png", "2", "old"),
	}
	local := []Attachment{
		{Filename: "same.png", Checksum: "aaa"},
		{Filename: "changed.png", Checksum: "new"},
		{Filename: "fresh.png", Checksum: "bbb"},
	}

	existing, pending, err := Pending(local, remotes)
	require.NoError(t, err)

	require.Len(t, existing, 1)
	assert.Equal(t, "same.png", existing[0].Filename)
	assert.Equal(t, "/wiki/download/attachments/1/same.png", existing[0].Link)

	var names []string
	for _, p := range pending {
		names = append(names, p.Filename)
	}
	assert.ElementsMatch(t, []string{"changed.png", "fresh.png"}, names)
}

func TestPendingRefusesWhatARealRunRefuses(t *testing.T) {
	_, _, err := Pending([]Attachment{
		{Name: "a/b_c.txt", Filename: "a_b_c.txt", Checksum: "one"},
		{Name: "a_b/c.txt", Filename: "a_b_c.txt", Checksum: "two"},
	}, nil)

	require.Error(t, err)
}
