package mark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/fs"
	"github.com/reconquest/karma-go"
)

const (
	AttachmentChecksumPrefix = `mark:checksum: `
)

type Attachment struct {
	ID       string
	Name     string
	Path     string
	Checksum string
	Link     string
}

func ResolveAttachments(
	api *confluence.API,
	page *confluence.PageInfo,
	fs fs.FileSystem,
	names []string,
) ([]Attachment, error) {
	attaches := []Attachment{}
	for _, name := range names {
		attach := Attachment{
			Path: name,
			Name: strings.ReplaceAll(name, "/", "_"),
		}

		checksum, err := getChecksum(fs, attach.Name)
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
			if remote.Filename == attach.Name {
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
			attach.Name,
			AttachmentChecksumPrefix+attach.Checksum,
			fs,
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
			fs,
			attach.Name,
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

func getChecksum(fs FileSystem, filename string) (string, error) {
	file, err := fs.Open(filename)
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
