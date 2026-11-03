// Declared attachments, uploading them to a page, and the preview of what
// would be uploaded.

package mark

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/rs/zerolog/log"
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

	_, pending, err := pendingAttachments(inline, remotes)
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

	declared, err := attachment.ResolveLocalAttachments(attachment.LocalOS, filepath.Dir(file), meta.Attachments)
	if err != nil {
		return nil, fmt.Errorf("unable to locate attachments: %w", err)
	}

	return declared, nil
}

func previewAttachmentLinks(
	ctx context.Context,
	api *confluence.API,
	target *confluence.PageInfo,
	local []attachment.Attachment,
) (*attachmentPreview, error) {
	if target == nil {
		// Nothing to link against on a page that does not exist yet, but two
		// files that flatten to one name still fail a real run before it
		// writes anything, so they fail the preview too.
		if _, _, err := pendingAttachments(local, nil); err != nil {
			return nil, fmt.Errorf("unable to resolve attachments: %w", err)
		}

		return nil, nil
	}

	remotes, err := api.GetAttachments(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to get attachments for page %s: %w", target.ID, err)
	}

	// Asked the way a real run decides it, so what it refuses -- two files
	// that flatten to one name -- is refused here too.
	existing, uploads, err := pendingAttachments(local, remotes)
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

// pendingAttachments returns the attachments that uploadAttachments would
// upload against remotes -- created, or updated because their checksum
// differs -- without uploading anything, and refuses what it would refuse.
// Alongside them come the ones it would keep as they are, with the link the
// remote they match already has.
//
// It is what a dry run asks: the same decision a real run makes, from the same
// list, so the two cannot disagree about which files would be sent.
func pendingAttachments(
	attachments []attachment.Attachment,
	remotes []confluence.AttachmentInfo,
) (existing, pending []attachment.Attachment, err error) {
	existing, creating, updating, _, err := classifyAttachments(attachments, remotes)
	if err != nil {
		return nil, nil, err
	}

	return existing, append(creating, updating...), nil
}

// classifyAttachments sorts attachments by what uploading them against
// remotes takes: nothing, a create, or an update. Repeats of a filename come
// back apart, as splitByFilename returns them. Checksums not already set are
// computed in place.
func classifyAttachments(
	attachments []attachment.Attachment,
	remotes []confluence.AttachmentInfo,
) (existing, creating, updating, duplicates []attachment.Attachment, err error) {
	for i := range attachments {
		// Rendered pictures set a checksum that holds across environments
		// (AGENTS.md invariant 7); keep it.
		if attachments[i].Checksum != "" {
			continue
		}

		checksum, err := attachment.GetChecksum(bytes.NewReader(attachments[i].FileBytes))
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("unable to get checksum for attachment %q: %w", attachments[i].Name, err)
		}

		attachments[i].Checksum = checksum
	}

	unique, duplicates, err := splitByFilename(attachments)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	existing = []attachment.Attachment{}
	creating = []attachment.Attachment{}
	updating = []attachment.Attachment{}
	for _, item := range unique {
		var found bool
		var same bool
		for _, remote := range remotes {
			if remote.Filename == item.Filename {
				same = item.Checksum == strings.TrimPrefix(
					remote.Metadata.Comment,
					attachment.AttachmentChecksumPrefix,
				)

				item.ID = remote.ID
				item.Link = path.Join(
					remote.Links.Context,
					remote.Links.Download,
				)

				found = true

				break
			}
		}

		if found {
			if same {
				existing = append(existing, item)
			} else {
				updating = append(updating, item)
			}
		} else {
			creating = append(creating, item)
		}
	}

	return existing, creating, updating, duplicates, nil
}

// UploadAttachments uploads attachments to page as publishing does: a file the
// page already holds under the same checksum is kept, and the rest are created
// or updated. It fetches the page's attachments itself and returns the
// attachments with their links.
func UploadAttachments(
	ctx context.Context,
	api *confluence.API,
	page *confluence.PageInfo,
	attachments []attachment.Attachment,
) ([]attachment.Attachment, error) {
	remotes, err := api.GetAttachments(ctx, page.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to get attachments for page %s: %w", page.ID, err)
	}

	resolved, _, err := uploadAttachments(ctx, api, page, attachments, remotes)

	return resolved, err
}

// uploadAttachments creates or updates attachments on page against remotes,
// the page's attachment list as already fetched. It returns the resolved
// attachments and an updated remote list that includes anything created or
// updated, so a later call needs no other round-trip.
func uploadAttachments(
	ctx context.Context,
	api *confluence.API,
	page *confluence.PageInfo,
	attachments []attachment.Attachment,
	remotes []confluence.AttachmentInfo,
) ([]attachment.Attachment, []confluence.AttachmentInfo, error) {
	existing, creating, updating, duplicates, err := classifyAttachments(attachments, remotes)
	if err != nil {
		return nil, nil, err
	}

	for i, item := range creating {
		log.Info().Msgf("creating attachment: %q", item.Name)

		info, err := api.CreateAttachment(ctx,
			page.ID,
			item.Filename,
			attachment.AttachmentChecksumPrefix+item.Checksum,
			bytes.NewReader(item.FileBytes),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create attachment %q: %w", item.Name, err)
		}

		item.ID = info.ID
		item.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		creating[i] = item
		remotes = append(remotes, info)
	}

	for i, item := range updating {
		log.Info().Msgf("updating attachment: %q", item.Name)

		info, err := api.UpdateAttachment(ctx,
			page.ID,
			item.ID,
			item.Filename,
			attachment.AttachmentChecksumPrefix+item.Checksum,
			bytes.NewReader(item.FileBytes),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to update attachment %q: %w", item.Name, err)
		}

		item.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		updating[i] = item

		// Reflect the new checksum so a later call against this same list sees
		// the attachment as current rather than re-uploading it.
		for j := range remotes {
			if remotes[j].Filename == item.Filename {
				remotes[j].Metadata.Comment = attachment.AttachmentChecksumPrefix + item.Checksum
				break
			}
		}
	}

	for i := range existing {
		log.Info().Msgf("keeping unmodified attachment: %q", existing[i].Name)
	}

	attachments = []attachment.Attachment{}
	attachments = append(attachments, existing...)
	attachments = append(attachments, creating...)
	attachments = append(attachments, updating...)

	// A repeat is the file that was just uploaded or kept under the same name,
	// so it points where the first one does. It stays in the list because its
	// Replace may be what a link in the document is resolved by.
	byFilename := make(map[string]attachment.Attachment, len(attachments))
	for _, item := range attachments {
		byFilename[item.Filename] = item
	}
	for _, duplicate := range duplicates {
		resolved := byFilename[duplicate.Filename]
		duplicate.ID = resolved.ID
		duplicate.Link = resolved.Link
		attachments = append(attachments, duplicate)
	}

	return attachments, remotes, nil
}

// splitByFilename separates the first attachment for each Filename from the
// ones after it.
//
// A document that links or embeds the same file twice hands it over twice, and
// the remotes are compared against once, before anything is uploaded: on a page
// that does not have the file yet, both copies would be created, and Confluence
// refuses the second because the first now holds the name. Only the first is
// uploaded; every link renders the same ri:filename, so it serves them all.
//
// Two different files can land on one name too -- "a/b_c.txt" and "a_b/c.txt"
// both flatten to "a_b_c.txt" -- and one upload would then serve the wrong one
// under a link that reads as right. That is refused rather than guessed at.
func splitByFilename(attachments []attachment.Attachment) ([]attachment.Attachment, []attachment.Attachment, error) {
	names := make(attachment.Filenames, len(attachments))
	var unique, duplicates []attachment.Attachment
	for _, item := range attachments {
		taken, err := names.Claim(item)
		if err != nil {
			return nil, nil, err
		}

		if taken {
			duplicates = append(duplicates, item)
		} else {
			unique = append(unique, item)
		}
	}

	return unique, duplicates, nil
}
