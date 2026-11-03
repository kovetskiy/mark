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
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rs/zerolog/log"
)

// FlattenFilename returns the name an attachment called name is uploaded and
// referred to by. An attachment has no directory, so a "/" -- in a path, or in a
// diagram's title -- becomes "_".
func FlattenFilename(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

// UploadName returns the name the file a document refers to as reference is
// uploaded under. The ac:image and ac:link templates name it the same way,
// through this function, so what a page names is always what was uploaded.
func UploadName(reference string) string {
	return FlattenFilename(cleanReference(reference))
}

// cleanReference returns the one spelling of a file reference: "./x", "a//x"
// and "a/../x" are the file "x" names, and have to be one attachment with one
// name. A reference that cleans down to no name at all, "." or "/", is kept as
// written.
func cleanReference(reference string) string {
	// On Windows a backslash separates, as filepath.Join opening the file does.
	cleaned := path.Clean(filepath.ToSlash(reference))
	if cleaned == "." || cleaned == "/" {
		return reference
	}

	return cleaned
}

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

	// Matched marks an attachment that a pattern found rather than a document
	// naming it outright. Somebody who wrote "images/*.png" asked for the set,
	// so being able to say which of them the page never referred to is not
	// worth a warning per file on every run.
	Matched bool
}

type Attacher interface {
	Attach(Attachment)
}

// Filenames holds the attachment each upload name went to first.
type Filenames map[string]Attachment

// Claim gives attachment its Filename unless another holds it already, and
// reports whether one did. A different file holding it is an error: one upload
// would serve both.
func (f Filenames) Claim(attachment Attachment) (bool, error) {
	held, taken := f[attachment.Filename]
	if !taken {
		f[attachment.Filename] = attachment

		return false, nil
	}

	if !same(held, attachment) {
		return false, fmt.Errorf(
			"attachments %q and %q would both be uploaded as %q but differ",
			held.Name, attachment.Name, attachment.Filename,
		)
	}

	return true, nil
}

// same reports whether a and b are one file. A checksum a renderer set is
// taken over the diagram's source, not its bytes, so it decides where both
// have one.
func same(a, b Attachment) bool {
	if a.Checksum != "" && b.Checksum != "" {
		return a.Checksum == b.Checksum
	}

	return bytes.Equal(a.FileBytes, b.FileBytes)
}

func ResolveLocalAttachments(opener Opener, base string, replacements []string) ([]Attachment, error) {
	attachments, err := prepareAttachments(opener, base, replacements)
	if err != nil {
		return nil, err
	}

	for i := range attachments {
		if err := setChecksum(&attachments[i]); err != nil {
			return nil, err
		}
	}

	return attachments, nil
}

// ResolveLocalAttachment resolves one path exactly as it was written.
//
// Nothing in it is read as a pattern, which is what separates this from
// ResolveLocalAttachments: a declared attachment may name several files on
// purpose, while a link points at one. "report[2024].pdf" is an ordinary
// filename, and globbing it would publish whichever of report2.pdf,
// report0.pdf or report4.pdf happens to sit beside it -- a different file from
// the one the link names, under a link that reads as if it were right.
func ResolveLocalAttachment(opener Opener, base, name string) (Attachment, error) {
	attachment, err := prepareAttachment(opener, base, name)
	if err != nil {
		return Attachment{}, err
	}

	if err := setChecksum(&attachment); err != nil {
		return Attachment{}, err
	}

	return attachment, nil
}

// setChecksum records what the file hashes to, which is how a later run
// recognises an attachment it does not need to upload again.
func setChecksum(attachment *Attachment) error {
	checksum, err := GetChecksum(bytes.NewReader(attachment.FileBytes))
	if err != nil {
		return fmt.Errorf("unable to get checksum for attachment %q: %w", attachment.Name, err)
	}

	attachment.Checksum = checksum

	return nil
}

// prepareAttachements creates an array of attachement objects based on an array of filepaths
func prepareAttachments(opener Opener, base string, replacements []string) ([]Attachment, error) {
	attachments := []Attachment{}

	for _, name := range replacements {
		matches, matched, err := expand(base, name)
		if err != nil {
			return nil, err
		}

		for _, match := range matches {
			attachment, err := prepareAttachment(opener, base, match)
			if err != nil {
				return nil, err
			}

			attachment.Matched = matched
			attachments = append(attachments, attachment)
		}
	}

	return attachments, nil
}

// expand turns what a document declared into the files it names, and reports
// whether a pattern did the naming.
//
// A name with nothing to match on is left exactly as it was, so that every
// document that worked before this still works and still fails in the same
// words when the file is not there. That matters beyond compatibility: a file
// really called "report[2024].pdf" is an ordinary name, and reading it as a
// pattern would turn a document that publishes into one that does not.
//
// Which is also why a pattern matching nothing falls back to the literal name.
// The two cases cannot be told apart from the outside -- a bracket is a
// character and a syntax at once -- so the answer that keeps working is the
// right one, and the file that is genuinely missing is reported by the open
// that follows, naming the path the author wrote.
func expand(base, name string) (matches []string, matched bool, err error) {
	if !hasPattern(name) {
		return []string{name}, false, nil
	}

	found, err := doublestar.FilepathGlob(filepath.Join(base, name))
	if err != nil {
		return nil, false, fmt.Errorf("unable to expand attachment pattern %q: %w", name, err)
	}

	if len(found) == 0 {
		return []string{name}, false, nil
	}

	// Named the way the document would name them, since that is what a link in
	// it says and what the upload is keyed by.
	matches = make([]string, 0, len(found))

	for _, path := range found {
		relative, err := filepath.Rel(base, path)
		if err != nil {
			// Outside the base, which prepareAttachment refuses by itself. Kept
			// as it is so that it is refused by name rather than dropped here.
			relative = path
		}

		matches = append(matches, filepath.ToSlash(relative))
	}

	// The order the files are uploaded in should not depend on how the
	// directory happens to be laid out.
	slices.Sort(matches)

	return matches, true, nil
}

// hasPattern reports whether a declared name is a pattern rather than a path.
func hasPattern(name string) bool {
	return strings.ContainsAny(name, "*?[{")
}

// ErrOutsideProject reports an attachment that resolves outside the directories
// mark is publishing from.
//
// Named for the boundary rather than for attachments: an include is held to it
// too, and a file a document may not read is a file a document may not read.
var ErrOutsideProject = errors.New("outside the project")

// CheckReadable reports whether a file a document points at may be read for it,
// by the boundary an attachment is held to: the document's own directory or the
// one mark is running in, with both sides resolved through their symlinks.
//
// Exported for the diagram renderers, which reach files the same way a document
// does -- a d2 diagram naming an icon beside itself -- and should not each grow
// their own idea of how far out of a repository a name may reach.
//
// An absolute reference is judged as itself rather than joined to the base,
// because that is how whatever reads it will read it. Joining one instead
// buries it under the directory it was supposed to be compared against --
// "/etc/id_rsa" becomes "<base>/etc/id_rsa", which is inside the project and
// does not exist, so the check passes and the read still happens somewhere
// else entirely.
func CheckReadable(base, reference string) error {
	path := reference
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	return checkPath(base, path, reference)
}

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
	return checkPath(base, filepath.Join(base, name), name)
}

// checkPath holds a resolved path to the project boundary. named is what the
// document actually wrote, which is what the reader has to go and look at.
func checkPath(base, resolved, named string) error {
	path := resolveDeepest(resolved)

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
		ErrOutsideProject, named, path, base,
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
func prepareAttachment(opener Opener, base, name string) (Attachment, error) {
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

	cleaned := cleanReference(name)
	attachment := Attachment{
		Name:      cleaned,
		Filename:  UploadName(name),
		FileBytes: fileBytes,
		Replace:   cleaned,
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
// Every way of writing a path maps to the same upload -- "./f.txt" is "f.txt"
// -- so names are looked up cleaned.
func NewResolver(attachments []Attachment) *Resolver {
	links := make(map[string]string, len(attachments))
	for _, attachment := range attachments {
		links[cleanReference(attachment.Replace)] = parseAttachmentLink(attachment.Link)
	}

	return &Resolver{links: links, used: map[string]bool{}}
}

// Resolve reports the URL for a link or image destination, or "" to leave it
// as the document wrote it.
func (r *Resolver) Resolve(target string) string {
	if r == nil {
		return ""
	}

	name := cleanReference(target)
	link, ok := r.links[name]
	if !ok {
		// A destination is a URL, so a file declared as a#b.png is linked as
		// a%23b.png. The spelling as written wins, which keeps a file really
		// called my%20file.png reachable.
		decoded, escaped := DecodeDestination(target)
		if !escaped {
			return ""
		}
		name = cleanReference(decoded)
		if link, ok = r.links[name]; !ok {
			return ""
		}
	}

	r.used[name] = true
	log.Debug().Msgf("replacing link: %q -> %q", name, link)

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
		// A pattern was asked for the set, not for each file in it: "upload
		// this directory" is a whole intention, and reporting every file the
		// page did not happen to link to would be a warning per file, on every
		// run, for doing what was asked.
		if attachment.Matched {
			continue
		}

		if !r.used[cleanReference(attachment.Replace)] {
			unused = append(unused, attachment.Replace)
		}
	}

	return unused
}

// AttachmentChecksumPrefix leads the comment in which mark keeps an uploaded
// attachment's checksum, so a later run can tell whether the file changed.
const AttachmentChecksumPrefix = `mark:checksum: `

func GetChecksum(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// parseAttachmentLink turns an attachment's download link into the URL a page
// links to.
//
// The path stays escaped. Confluence percent-encodes the filename in the
// link, and decoding it turned an attachment called a#b.png into a link to
// a, with b.png as its fragment; a?b.png and a%b.png broke the same way.
func parseAttachmentLink(attachLink string) string {
	uri, err := url.ParseRequestURI(attachLink)
	if err != nil {
		return attachLink
	} else {
		query := uri.Query().Encode()
		if query == "" {
			return uri.EscapedPath()
		}
		return uri.EscapedPath() + "?" + query
	}
}

// DecodeDestination reads a link or image destination as the URL path it is,
// and reports the file it names when that differs from what was written.
//
// A local file called a#b.png has to be written a%23b.png, since a bare "#"
// starts a fragment; "my%20file.png" is the usual spelling of "my file.png".
// Callers try the destination as written first and fall back to this, so a
// file whose name really contains "%20" still resolves to itself.
func DecodeDestination(destination string) (string, bool) {
	if !strings.Contains(destination, "%") {
		return "", false
	}

	decoded, err := url.PathUnescape(destination)
	if err != nil || decoded == destination {
		return "", false
	}

	return decoded, true
}
