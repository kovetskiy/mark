package mark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/kovetskiy/mark/pkg/mark/vfs"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kovetskiy/mark/pkg/confluence"
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

func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	attaches []Attachment,
) ([]Attachment, error) {
	for i, _ := range attaches {
		checksum, err := getChecksum(bytes.NewReader(attaches[i].FileBytes))
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to get checksum for attachment: %q", attaches[i].Name,
			)
		}

		attaches[i].Checksum = checksum
	}

	remotes, err := api.GetAttachments(page.ID)
	if err != nil {
		panic(err)
	}

	existing := []Attachment{}
	creating := []Attachment{}
	updating := []Attachment{}
	for _, attach := range attaches {
		var found bool
		var same bool
		for _, remote := range remotes {
			if remote.Filename == attach.Filename {
				same = attach.Checksum == strings.TrimPrefix(
					remote.Metadata.Comment,
					AttachmentChecksumPrefix,
				)

				attach.ID = remote.ID
				attach.Link = path.Join(
					remote.Links.Context,
					remote.Links.Download,
				)

				found = true

				break
			}
		}

		if found {
			if same {
				existing = append(existing, attach)
			} else {
				updating = append(updating, attach)
			}
		} else {
			creating = append(creating, attach)
		}
	}

	for i, attach := range creating {
		log.Infof(nil, "creating attachment: %q", attach.Name)

		info, err := api.CreateAttachment(
			page.ID,
			attach.Filename,
			AttachmentChecksumPrefix+attach.Checksum,
			bytes.NewReader(attach.FileBytes),
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to create attachment %q",
				attach.Name,
			)
		}

		attach.ID = info.ID
		attach.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		creating[i] = attach
	}

	for i, attach := range updating {
		log.Infof(nil, "updating attachment: %q", attach.Name)

		info, err := api.UpdateAttachment(
			page.ID,
			attach.ID,
			attach.Name,
			AttachmentChecksumPrefix+attach.Checksum,
			bytes.NewReader(attach.FileBytes),
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to update attachment %q",
				attach.Name,
			)
		}

		attach.Link = path.Join(
			info.Links.Context,
			info.Links.Download,
		)

		updating[i] = attach
	}

	for i, _ := range existing {
		log.Infof(nil, "keeping unmodified attachment: %q", attaches[i].Name)
	}

	attaches = []Attachment{}
	attaches = append(attaches, existing...)
	attaches = append(attaches, creating...)
	attaches = append(attaches, updating...)

	return attaches, nil
}

func ResolveLocalAttachments(opener vfs.Opener, base string, replacements []string) ([]Attachment, error) {
	attaches, err := prepareAttachments(opener, base, replacements)
	if err != nil {
		return nil, err
	}

	for _, attach := range attaches {
		checksum, err := getChecksum(bytes.NewReader(attach.FileBytes))
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to get checksum for attachment: %q", attach.Name,
			)
		}

		attach.Checksum = checksum
	}
	return attaches, err
}

func prepareAttachments(opener vfs.Opener, base string, replacements []string) ([]Attachment, error) {
	attaches := []Attachment{}
	for _, name := range replacements {
		attachmentPath := filepath.Join(base, name)
		var (
			file      io.ReadWriteCloser
			fileBytes []byte
			err       error
		)
		if file, err = opener.Open(attachmentPath); err == nil {
			fileBytes, err = io.ReadAll(file)
			_ = file.Close()
		}
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to open file: %q",
				attachmentPath,
			)
		}

		attach := Attachment{
			Name:      name,
			Filename:  strings.ReplaceAll(name, "/", "_"),
			FileBytes: fileBytes,
			Replace:   name,
		}

		attaches = append(attaches, attach)
	}

	return attaches, nil
}

func CompileAttachmentLinks(markdown []byte, attaches []Attachment) []byte {
	links := map[string]string{}
	replaces := []string{}

	for _, attach := range attaches {
		links[attach.Replace] = parseAttachmentLink(attach.Link)
		replaces = append(replaces, attach.Replace)
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

func getChecksum(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func parseAttachmentLink(attachLink string) string {
	uri, err := url.ParseRequestURI(attachLink)
	if err != nil {
		return strings.ReplaceAll("&", "&amp;", attachLink)
	} else {
		return uri.Path +
			"?" + url.QueryEscape(uri.Query().Encode())
	}
}
