package attachment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kovetskiy/mark/confluence"
	"github.com/kovetskiy/mark/vfs"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
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

func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	attachments []Attachment,
) ([]Attachment, error) {
	for i := range attachments {
		checksum, err := GetChecksum(bytes.NewReader(attachments[i].FileBytes))
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to get checksum for attachment: %q", attachments[i].Name,
			)
		}

		attachments[i].Checksum = checksum
	}

	remotes, err := api.GetAttachments(page.ID)
	if err != nil {
		panic(err)
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
		log.Infof(nil, "creating attachment: %q", attachment.Name)

		info, err := api.CreateAttachment(
			page.ID,
			attachment.Filename,
			AttachmentChecksumPrefix+attachment.Checksum,
			bytes.NewReader(attachment.FileBytes),
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to create attachment %q",
				attachment.Name,
			)
		}

		attachment.ID = info.ID
		attachment.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		creating[i] = attachment
	}

	for i, attachment := range updating {
		log.Infof(nil, "updating attachment: %q", attachment.Name)

		info, err := api.UpdateAttachment(
			page.ID,
			attachment.ID,
			attachment.Filename,
			AttachmentChecksumPrefix+attachment.Checksum,
			bytes.NewReader(attachment.FileBytes),
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to update attachment %q",
				attachment.Name,
			)
		}

		attachment.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		updating[i] = attachment
	}

	for i := range existing {
		log.Infof(nil, "keeping unmodified attachment: %q", attachments[i].Name)
	}

	attachments = []Attachment{}
	attachments = append(attachments, existing...)
	attachments = append(attachments, creating...)
	attachments = append(attachments, updating...)

	return attachments, nil
}

func ResolveLocalAttachments(opener vfs.Opener, base string, replacements []string) ([]Attachment, error) {
	attachments, err := prepareAttachments(opener, base, replacements)
	if err != nil {
		return nil, err
	}

	for _, attachment := range attachments {
		checksum, err := GetChecksum(bytes.NewReader(attachment.FileBytes))
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to get checksum for attachment: %q", attachment.Name,
			)
		}

		attachment.Checksum = checksum
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
		return Attachment{}, karma.Format(err, "unable to open file: %q", attachmentPath)
	}
	defer func() {
		_ = file.Close()
	}()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return Attachment{}, karma.Format(err, "unable to read file: %q", attachmentPath)
	}

	return Attachment{
		Name:      name,
		Filename:  strings.ReplaceAll(name, "/", "_"),
		FileBytes: fileBytes,
		Replace:   name,
	}, nil
}

func CompileAttachmentLinks(markdown []byte, attachments []Attachment) []byte {
	links := map[string]string{}
	replaces := []string{}

	for _, attachment := range attachments {
		links[attachment.Replace] = parseAttachmentLink(attachment.Link)
		replaces = append(replaces, attachment.Replace)
	}

	// sort by length so first items will have bigger length
	// it's helpful for replacing in case of following names
	// attachments/a.jpg
	// attachments/a.jpg.jpg
	// so we replace longer and then shorter
	sort.SliceStable(replaces, func(i, j int) bool {
		return len(replaces[i]) > len(replaces[j])
	})

	for _, replace := range replaces {
		to := links[replace]

		found := false
		if bytes.Contains(markdown, []byte("attachment://"+replace)) {
			from := "attachment://" + replace

			log.Debugf(nil, "replacing legacy link: %q -> %q", from, to)

			markdown = bytes.ReplaceAll(
				markdown,
				[]byte(from),
				[]byte(to),
			)

			found = true
		}

		if bytes.Contains(markdown, []byte(replace)) {
			from := replace

			log.Debugf(nil, "replacing link: %q -> %q", from, to)

			markdown = bytes.ReplaceAll(
				markdown,
				[]byte(from),
				[]byte(to),
			)

			found = true
		}

		if !found {
			log.Warningf(nil, "unused attachment: %s", replace)
		}
	}

	return markdown
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
		return strings.ReplaceAll(attachLink, "&", "&amp;")
	} else {
		return uri.Path +
			"?" + url.QueryEscape(uri.Query().Encode())
	}
}
