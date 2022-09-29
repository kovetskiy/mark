package mark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/url"
	"os"
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
	ID       string
	Name     string
	Filename string
	Path     string
	Checksum string
	Link     string
	Replace  string
}

func ResolveAttachments(
	api *confluence.API,
	attachmentBasename bool,
	page *confluence.PageInfo,
	base string,
	replacements []string,
) ([]Attachment, error) {
	attaches, err := prepareAttachments(base, replacements, attachmentBasename)
	if err != nil {
		return nil, err
	}

	for i, _ := range attaches {
		checksum, err := getChecksum(attaches[i].Path)
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
			attach.Path,
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

func prepareAttachments(base string, replacements []string, attachmentBasename bool) ([]Attachment, error) {
	attaches := []Attachment{}
	for _, name := range replacements {
		var filename string
		if attachmentBasename {
			filename = filepath.Base(name)
		} else {
			filename = strings.ReplaceAll(name, "/", "_")
		}
		attach := Attachment{
			Name:     name,
			Filename: filename,
			Path:     filepath.Join(base, name),
			Replace:  name,
		}

		attaches = append(attaches, attach)
	}

	return attaches, nil
}

func CompileAttachmentLinks(markdown []byte, attaches []Attachment) []byte {
	links := map[string]string{}
	replaces := []string{}

	for _, attach := range attaches {
		uri, err := url.ParseRequestURI(attach.Link)
		if err != nil {
			links[attach.Replace] = strings.ReplaceAll("&", "&amp;", attach.Link)
		} else {
			links[attach.Replace] = uri.Path +
				"?" + url.QueryEscape(uri.Query().Encode())
		}

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
