package mark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
) error {
	attachs := []Attachment{}
	for _, name := range names {
		attach := Attachment{
			Name:     name,
			Filename: strings.ReplaceAll(name, "/", "_"),
			Path:     filepath.Join(base, name),
		}

		checksum, err := getChecksum(attach.Path)
		if err != nil {
			return karma.Format(
				err,
				"unable to get checksum for attachment: %q", attach.Name,
			)
		}

		attach.Checksum = checksum

		attachs = append(attachs, attach)
	}

	remotes, err := api.GetAttachments(page.ID)
	if err != nil {
		panic(err)
	}

	existing := []Attachment{}
	creating := []Attachment{}
	updating := []Attachment{}
	for _, attach := range attachs {
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

	{
		marshaledXXX, _ := json.MarshalIndent(existing, "", "  ")
		fmt.Printf("existing: %s\n", string(marshaledXXX))
	}

	{
		marshaledXXX, _ := json.MarshalIndent(creating, "", "  ")
		fmt.Printf("creating: %s\n", string(marshaledXXX))
	}

	{
		marshaledXXX, _ := json.MarshalIndent(updating, "", "  ")
		fmt.Printf("updating: %s\n", string(marshaledXXX))
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
			return karma.Format(
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
			return karma.Format(
				err,
				"unable to update attachment %q",
				attach.Name,
			)
		}

		attach.Link = info.Links.Download

		updating[i] = attach
	}

	return nil
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
