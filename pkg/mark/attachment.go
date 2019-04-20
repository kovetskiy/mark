package mark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/reconquest/karma-go"
)

const (
	AttachmentChecksumPrefix = `mark:checksum: `
)

type Attachment struct {
	ID       string
	Name     string
	Filename string
	Path     string
	Checksum string
	Link     string
}

func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	base string,
	names []string,
) ([]Attachment, error) {
	attaches := []Attachment{}
	for _, name := range names {
		attach := Attachment{
			Name:     name,
			Filename: strings.ReplaceAll(name, "/", "_"),
			Path:     filepath.Join(base, name),
		}

		checksum, err := getChecksum(attach.Path)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to get checksum for attachment: %q", attach.Name,
			)
		}

		attach.Checksum = checksum

		attaches = append(attaches, attach)
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
				attach.Link = remote.Links.Download

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
			attach.Path,
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to create attachment %q",
				attach.Name,
			)
		}

		attach.ID = info.ID
		attach.Link = info.Links.Download

		creating[i] = attach
	}

	for i, attach := range updating {
		log.Infof(nil, "updating attachment: %q", attach.Name)

		info, err := api.UpdateAttachment(
			page.ID,
			attach.ID,
			attach.Name,
			AttachmentChecksumPrefix+attach.Checksum,
			attach.Path,
		)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to update attachment %q",
				attach.Name,
			)
		}

		attach.Link = info.Links.Download

		updating[i] = attach
	}

	attaches = []Attachment{}
	attaches = append(attaches, existing...)
	attaches = append(attaches, creating...)
	attaches = append(attaches, updating...)

	return attaches, nil
}

func CompileAttachmentLinks(markdown []byte, attaches []Attachment) []byte {
	links := map[string]string{}
	names := []string{}

	for _, attach := range attaches {
		uri, err := url.ParseRequestURI(attach.Link)
		if err != nil {
			links[attach.Name] = strings.ReplaceAll("&", "&amp;", attach.Link)
		} else {
			links[attach.Name] = uri.Path +
				"?" + url.QueryEscape(uri.Query().Encode())
		}

		names = append(names, attach.Name)
	}

	// sort by length so first items will have bigger length
	// it's helpful for replacing in case of following names
	// attachments/a.jpg
	// attachments/a.jpg.jpg
	// so we replace longer and then shorter
	sort.SliceStable(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	for _, name := range names {
		from := `attachment://` + name
		to := links[name]

		log.Debugf(nil, "replacing: %q -> %q", from, to)

		markdown = bytes.ReplaceAll(
			markdown,
			[]byte(from),
			[]byte(to),
		)
	}

	return markdown
}

func getChecksum(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to open file",
		)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
