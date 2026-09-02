package attachment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
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

// ErrOutsideProject reports an attachment that resolves outside the directories
// mark is publishing from.
var ErrOutsideProject = errors.New("attachment is outside the project")

// checkAttachmentPath refuses an attachment that resolves outside both the
// document's own directory and the directory mark is running in.
//
// A document says which files to upload, and a document is content: on a
// repository that takes pull requests, a contributor could point an image at
// "../../../../home/runner/.aws/credentials" and have its contents published as
// a page attachment, under a flattened filename that looks like anything else.
//
// Upward paths themselves are ordinary -- "../images/logo.png" is how a docs
// directory refers to shared assets, and README documents it -- so the boundary
// is not the document's directory. It is that directory or the one mark was run
// in, which for a run at the root of a repository is the repository.
//
// Both sides are resolved before the comparison, since a link committed to the
// repository is as good as a path for reaching outside it -- and since the
// roots have to be resolved too, or a repository reached through a symlinked
// path would put every file in it outside itself.
func checkAttachmentPath(base, name string) error {
	path := resolveDeepest(filepath.Join(base, name))

	roots := []string{resolveDeepest(base)}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, resolveDeepest(cwd))
	}

	if withinAny(path, roots) {
		return nil
	}

	// Reported as the resolved absolute path: "docs/../../id_rsa" cleans down
	// to "../id_rsa", which says less about where the file actually is than the
	// reader needs in order to judge it.
	return fmt.Errorf(
		"%w: %q resolves to %s, which is outside both %q and the directory mark is "+
			"running in; publish from a directory that contains it",
		ErrOutsideProject, name, path, base,
	)
}

// resolveDeepest resolves the longest leading part of a path that exists and
// re-appends whatever is left.
//
// Both sides of the comparison have to be in the same terms or a directory is
// found not to contain the file sitting in it: with mark run in
// "/mnt/data/work" and the document named through a link as
// "~/work/docs/x.md", "/home/me/work/docs/logo.png" is not relative to
// "/mnt/data/work", however plainly it sits beside its own document. Symlinked
// checkouts, bind-mounted CI workspaces and every path under macOS's /tmp
// arrive this way.
//
// The deepest part rather than the whole path, because the file itself need not
// exist: a name that is merely misspelled should be refused by the open, which
// says so, rather than by the boundary, which would say something else.
func resolveDeepest(path string) string {
	path = filepath.Clean(path)

	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Join(resolved, rest)
		}

		parent := filepath.Dir(path)
		if parent == path {
			// The root itself did not resolve; nothing is left to walk up to.
			return filepath.Join(path, rest)
		}

		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// withinAny reports whether a path is inside any of the roots. It compares the
// paths as it is given them; resolving one side but not the other is the bug
// resolveDeepest exists to prevent.
func withinAny(path string, roots []string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, root := range roots {
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		relative, err := filepath.Rel(absoluteRoot, absolute)
		if err != nil {
			continue
		}

		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// prepareAttachement opens the file, reads its content and creates an attachement object
func prepareAttachment(opener vfs.Opener, base, name string) (Attachment, error) {
	attachmentPath := filepath.Join(base, name)

	if err := checkAttachmentPath(base, name); err != nil {
		return Attachment{}, err
	}

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
