// Declared attachments and the preview of what would be uploaded.

package mark

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/vfs"
)

// attachmentPreview resolves the links a real run would write for a page's
// attachments, without uploading anything.
//
// The fingerprint covers those links, so comparing without them would call a
// page changed whenever it links a file. An attachment the page does not hold
// yet, or holds in other bytes, is uploaded by a real run and gets a link that
// does not exist until then: a page that links it has a changed body. The
// upload itself is a change to the page either way, which uploads reports.
type attachmentPreview struct {
	links *attachment.Resolver

	// remotes is what the page holds now, and declared whether a real run
	// would upload any of the attachments its headers declare.
	remotes  []confluence.AttachmentInfo
	declared bool

	// marker stands in for a link that does not exist until the attachment is
	// uploaded. Finding it in the compiled page is how a page that links one is
	// told from one that merely embeds it, which names the file and not the
	// link. Random per preview, so text in the document cannot be mistaken for it.
	marker string
}

// settle reports whether body links an attachment whose link a real run would
// change, and returns it with the stand-in links read as the file's name.
func (p *attachmentPreview) settle(body string) (string, bool) {
	if p == nil || !strings.Contains(body, p.marker) {
		return body, false
	}

	return strings.ReplaceAll(body, p.marker, ""), true
}

// uploads reports whether a real run would upload any attachment to the page:
// one its headers declare, or one found while compiling it -- an embedded
// image, a rendered diagram -- whose checksum the page does not hold. The
// latter are asked the way a real run decides them, so two of them that
// flatten to one name fail here as they fail there. A preview of a page that
// does not exist yet (p is nil) only checks that.
func (p *attachmentPreview) uploads(inline []attachment.Attachment) (bool, error) {
	var remotes []confluence.AttachmentInfo
	if p != nil {
		remotes = p.remotes
	}

	_, pending, err := attachment.Pending(inline, remotes)
	if err != nil {
		return false, fmt.Errorf("unable to resolve attachments: %w", err)
	}

	return p != nil && (p.declared || len(pending) > 0), nil
}

// declaredAttachments reads the files the document's headers declare.
func declaredAttachments(file string, meta *metadata.Meta) ([]attachment.Attachment, error) {
	if meta == nil {
		return nil, nil
	}

	declared, err := attachment.ResolveLocalAttachments(vfs.LocalOS, filepath.Dir(file), meta.Attachments)
	if err != nil {
		return nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	return declared, nil
}

func previewAttachmentLinks(
	api *confluence.API,
	target *confluence.PageInfo,
	local []attachment.Attachment,
) (*attachmentPreview, error) {
	if target == nil {
		// Nothing to link against on a page that does not exist yet, but two
		// files that flatten to one name still fail a real run before it
		// writes anything, so they fail the preview too.
		if _, _, err := attachment.Pending(local, nil); err != nil {
			return nil, fmt.Errorf("unable to resolve attachments: %w", err)
		}

		return nil, nil
	}

	remotes, err := api.GetAttachments(target.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	// Asked the way a real run decides it, so what it refuses -- two files
	// that flatten to one name -- is refused here too.
	existing, uploads, err := attachment.Pending(local, remotes)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve attachments: %w", err)
	}

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("unable to generate a placeholder for pending attachment links: %w", err)
	}
	marker := "/mark-dry-run-pending-" + hex.EncodeToString(nonce[:]) + "/"

	// By filename, so a file declared twice is linked under each spelling, as
	// a real run links it.
	links := make(map[string]string, len(local))
	for _, item := range existing {
		links[item.Filename] = item.Link
	}
	for _, item := range uploads {
		links[item.Filename] = marker + item.Filename
	}

	linked := make([]attachment.Attachment, 0, len(local))
	for _, item := range local {
		item.Link = links[item.Filename]
		linked = append(linked, item)
	}

	return &attachmentPreview{
		links:    attachment.NewResolver(linked),
		marker:   marker,
		remotes:  remotes,
		declared: len(uploads) > 0,
	}, nil
}

// withHeaderAttachments adds what the page header references to the document's
// own. One filename is one attachment on the page, so a file that shares a name
// with a different one -- declared, found while rendering, or the header's --
// would silently replace it, and is refused instead.
func withHeaderAttachments(file string, declared, inline, fromHeader []attachment.Attachment) ([]attachment.Attachment, error) {
	names := make(attachment.Filenames, len(declared)+len(inline)+len(fromHeader))
	for _, a := range slices.Concat(declared, inline) {
		if _, err := names.Claim(a); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	}

	for _, a := range fromHeader {
		taken, err := names.Claim(a)
		if err != nil {
			return nil, fmt.Errorf(
				"page header attachment %q is a different file from the one %s attaches under that name",
				a.Filename, file,
			)
		}
		if !taken {
			inline = append(inline, a)
		}
	}

	return inline, nil
}
