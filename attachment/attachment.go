package attachment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/vfs"
	"github.com/rs/zerolog/log"
)

const (
	AttachmentChecksumPrefix = `mark:checksum: `
)

type Attachment struct {
	ID        string
	Name      string
	Filename  string
	FileBytes []byte
	Checksum  string
	Link      string
	Width     string
	Height    string
	Replace   string
}

type Attacher interface {
	Attach(Attachment)
}

// ResolveAttachments uploads the given attachments to the page, skipping any
// whose checksum already matches what is stored remotely.
//
// It fetches the page's current attachment list itself. Callers that resolve
// attachments more than once for the same page -- as mark does, first for
// declared attachments and then for diagrams discovered while rendering --
// should use ResolveAttachmentsWithRemotes to avoid re-fetching a list they
// already hold.
func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	attachments []Attachment,
) ([]Attachment, error) {
	remotes, err := api.GetAttachments(page.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to get attachments for page %s: %w", page.ID, err)
	}

	resolved, _, err := ResolveAttachmentsWithRemotes(api, page, attachments, remotes)
	return resolved, err
}

// ResolveAttachmentsWithRemotes is ResolveAttachments against an
// already-fetched remote attachment list. It returns the resolved attachments
// and an updated remote list that includes anything created or updated by this
// call, so a subsequent call can be made without another round-trip.
func ResolveAttachmentsWithRemotes(
	api *confluence.API,
	page *confluence.PageInfo,
	attachments []Attachment,
	remotes []confluence.AttachmentInfo,
) ([]Attachment, []confluence.AttachmentInfo, error) {
	for i := range attachments {
		// Skip checksum computation if already set (e.g. by mermaid/d2 renderers
		// which use the source content as the stable checksum rather than the
		// rendered PNG bytes, which may be non-deterministic across environments).
		if attachments[i].Checksum != "" {
			continue
		}

		checksum, err := GetChecksum(bytes.NewReader(attachments[i].FileBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("unable to get checksum for attachment %q: %w", attachments[i].Name, err)
		}

		attachments[i].Checksum = checksum
	}

	existing := []Attachment{}
	creating := []Attachment{}
	updating := []Attachment{}
	for _, attachment := range attachments {
		var found bool
		var same bool
		for _, remote := range remotes {
			if remote.Filename == attachment.Filename {
				same = attachment.Checksum == strings.TrimPrefix(
					remote.Metadata.Comment,
					AttachmentChecksumPrefix,
				)

				attachment.ID = remote.ID
				attachment.Link = path.Join(
					remote.Links.Context,
					remote.Links.Download,
				)

				found = true

				break
			}
		}

		if found {
			if same {
				existing = append(existing, attachment)
			} else {
				updating = append(updating, attachment)
			}
		} else {
			creating = append(creating, attachment)
		}
	}

	for i, attachment := range creating {
		log.Info().Msgf("creating attachment: %q", attachment.Name)

		info, err := api.CreateAttachment(
			page.ID,
			attachment.Filename,
			AttachmentChecksumPrefix+attachment.Checksum,
			bytes.NewReader(attachment.FileBytes),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create attachment %q: %w", attachment.Name, err)
		}

		attachment.ID = info.ID
		attachment.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		creating[i] = attachment
		remotes = append(remotes, info)
	}

	for i, attachment := range updating {
		log.Info().Msgf("updating attachment: %q", attachment.Name)

		info, err := api.UpdateAttachment(
			page.ID,
			attachment.ID,
			attachment.Filename,
			AttachmentChecksumPrefix+attachment.Checksum,
			bytes.NewReader(attachment.FileBytes),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to update attachment %q: %w", attachment.Name, err)
		}

		attachment.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		updating[i] = attachment

		// Reflect the new checksum so a later call against this same list sees
		// the attachment as current rather than re-uploading it.
		for j := range remotes {
			if remotes[j].Filename == attachment.Filename {
				remotes[j].Metadata.Comment = AttachmentChecksumPrefix + attachment.Checksum
				break
			}
		}
	}

	for i := range existing {
		log.Info().Msgf("keeping unmodified attachment: %q", existing[i].Name)
	}

	attachments = []Attachment{}
	attachments = append(attachments, existing...)
	attachments = append(attachments, creating...)
	attachments = append(attachments, updating...)

	return attachments, remotes, nil
}

func ResolveLocalAttachments(opener vfs.Opener, base string, replacements []string) ([]Attachment, error) {
	attachments, err := prepareAttachments(opener, base, replacements)
	if err != nil {
		return nil, err
	}

	for i := range attachments {
		checksum, err := GetChecksum(bytes.NewReader(attachments[i].FileBytes))
		if err != nil {
			return nil, fmt.Errorf("unable to get checksum for attachment %q: %w", attachments[i].Name, err)
		}

		attachments[i].Checksum = checksum
	}
	return attachments, err
}

// prepareAttachements creates an array of attachement objects based on an array of filepaths
func prepareAttachments(opener vfs.Opener, base string, replacements []string) ([]Attachment, error) {
	attachments := []Attachment{}
	for _, name := range replacements {
		attachment, err := prepareAttachment(opener, base, name)
		if err != nil {
			return nil, err
		}

		attachments = append(attachments, attachment)
	}

	return attachments, nil
}

// prepareAttachement opens the file, reads its content and creates an attachement object
func prepareAttachment(opener vfs.Opener, base, name string) (Attachment, error) {
	attachmentPath := filepath.Join(base, name)
	file, err := opener.Open(attachmentPath)
	if err != nil {
		return Attachment{}, fmt.Errorf("unable to open file %q: %w", attachmentPath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return Attachment{}, fmt.Errorf("unable to read file %q: %w", attachmentPath, err)
	}

	attachment := Attachment{
		Name:      name,
		Filename:  strings.ReplaceAll(name, "/", "_"),
		FileBytes: fileBytes,
		Replace:   name,
	}

	// Try to detect image dimensions if it's an image attachment
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		if config, _, err := image.DecodeConfig(bytes.NewReader(fileBytes)); err == nil {
			attachment.Width = strconv.Itoa(config.Width)
			attachment.Height = strconv.Itoa(config.Height)
		}
	}

	return attachment, nil
}

// Resolver maps the path a document wrote onto the attachment uploaded for it.
type Resolver struct {
	links map[string]string
	used  map[string]bool
}

// NewResolver builds a resolver for the given attachments.
//
// Both spellings a document may use are accepted: the plain path, and the
// legacy "attachment://" form. They map to the same upload, and the legacy one
// is listed first so that Unused reports the pair once rather than twice.
func NewResolver(attachments []Attachment) *Resolver {
	links := make(map[string]string, len(attachments)*2)
	for _, attachment := range attachments {
		link := parseAttachmentLink(attachment.Link)
		links[attachment.Replace] = link
		links["attachment://"+attachment.Replace] = link
	}

	return &Resolver{links: links, used: map[string]bool{}}
}

// Resolve reports the URL for a link or image destination, or "" to leave it
// as the document wrote it.
func (r *Resolver) Resolve(target string) string {
	if r == nil {
		return ""
	}

	link, ok := r.links[target]
	if !ok {
		return ""
	}

	r.used[strings.TrimPrefix(target, "attachment://")] = true
	log.Debug().Msgf("replacing link: %q -> %q", target, link)

	return link
}

// Unused reports the attachments that were uploaded but never referred to.
//
// This is only known once the whole document has been walked, which is why it
// is reported here rather than as each attachment is resolved.
func (r *Resolver) Unused(attachments []Attachment) []string {
	if r == nil {
		return nil
	}

	var unused []string
	for _, attachment := range attachments {
		if !r.used[attachment.Replace] {
			unused = append(unused, attachment.Replace)
		}
	}

	return unused
}

func GetChecksum(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func parseAttachmentLink(attachLink string) string {
	uri, err := url.ParseRequestURI(attachLink)
	if err != nil {
		return attachLink
	} else {
		query := uri.Query().Encode()
		if query == "" {
			return uri.Path
		}
		return uri.Path + "?" + query
	}
}
