// Content fingerprints behind --changes-only and --no-overwrite.

package mark

import (
	// SHA-1 is used only as a content fingerprint for --changes-only, never as
	// a security primitive. The digest is embedded in the page version message
	// and matched back with a 40-hex-character regex, so widening it would stop
	// mark from recognising pages published by earlier versions.
	"crypto/sha1" //nolint:gosec // G505: non-cryptographic content fingerprint
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/manifest"
	"github.com/kovetskiy/mark/v16/metadata"
	"github.com/kovetskiy/mark/v16/report"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/rs/zerolog/log"
)

// previewChange says whether a real run would change the page, using the same
// comparison it does for the body, and logs the answer.
//
// others are what a real run would do to the page besides writing its body --
// moving it, say -- which it does whatever the body comparison finds, so any of
// them makes the page one that would change. They come back as the reason.
func previewChange(
	body string,
	existing *confluence.PageInfo,
	relinked bool,
	others []string,
	file string,
	config Config,
	meta *metadata.Meta,
	std *stdlib.Lib,
) (string, string, error) {
	if existing == nil {
		log.Info().Msgf("%s: page would be created", file)

		return report.StatusWouldCreate, "", nil
	}

	var layout, sidebar, appearance, emoji, title string
	if meta != nil {
		layout, sidebar, appearance, emoji, title = meta.Layout, meta.Sidebar, meta.ContentAppearance, meta.Emoji, meta.Title
	}

	wrapped, err := wrapLayout(std, layout, sidebar, body)
	if err != nil {
		return "", "", err
	}

	hash := contentFingerprint(wrapped, appearance, metadata.NormalizeContentAppearance(config.ContentAppearance), emoji)
	retitled := meta != nil && existing.Title != title
	reason := strings.Join(others, "; ")

	switch {
	case retitled:
		log.Info().Msgf("%s: page %q would be retitled to %q", file, existing.Title, title)
	case relinked:
		log.Info().Msgf("%s: page %q would be updated, with an attachment it does not have yet", file, existing.Title)
	case readContentHash(existing.Version.Message) == hash:
		if len(others) == 0 {
			log.Info().Msgf("%s: page %q is already up to date", file, existing.Title)

			return report.StatusUnchanged, "", nil
		}

		log.Info().Msgf("%s: page %q has an unchanged body but %s", file, existing.Title, reason)
	default:
		log.Info().Msgf("%s: page %q would be updated", file, existing.Title)
	}

	return report.StatusWouldUpdate, reason, nil
}

func sha1Hash(input string) string {
	h := sha1.New() //nolint:gosec // G401: see the crypto/sha1 import comment
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// contentFingerprint is what --changes-only compares: the body, and the page
// properties published alongside it.
//
// The body alone let a change to nothing but the Emoji or Content-Appearance
// header be skipped as "already up to date", although both are sent with the
// body and only take effect when it is.
//
// Each property is folded in only when it differs from what a page without
// the header gets -- no emoji, and the appearance --content-appearance gives,
// full width unless it says otherwise -- so that the fingerprint of such a page
// is the body's hash exactly as before, whatever that flag is set to. Every
// page already stamped by an earlier mark would otherwise look changed and be
// republished once, across every space, on the first run after upgrading.
// Pages that do carry one of the headers are republished once, which is the
// price of finding out. Changing --content-appearance itself is, as before,
// not something --changes-only notices.
func contentFingerprint(html, appearance, defaultAppearance, emoji string) string {
	if emoji == "" && (appearance == "" || appearance == defaultAppearance) {
		return sha1Hash(html)
	}

	return sha1Hash(html + "\x00mark:content-appearance=" + appearance + "\x00mark:emoji=" + emoji)
}

var (
	contentHashLeading  = regexp.MustCompile(`^\[v([a-f0-9]{40})]`)
	contentHashTrailing = regexp.MustCompile(`\[v([a-f0-9]{40})]$`)
)

// readContentHash recovers the fingerprint formatVersionMessage wrote, or "" if
// the message carries none.
//
// Both layouts have to be read: mark writes the tag first, but pages stamped
// before that change carry it last, and if those stopped matching then the
// first run after upgrading would rewrite every page in every space once --
// exactly what --changes-only exists to prevent.
//
// Both are anchored. Matching the tag anywhere would also read one the operator
// happened to put in --version-message -- someone quoting a previous message,
// say -- and prefer it over the real one. The consequence is mild, an update
// that was not needed rather than a change that was missed, but the position is
// free information and there is no reason to discard it.
//
// Leading wins, because that is what mark writes now; trailing is only
// consulted for pages it has not yet restamped.
func readContentHash(message string) string {
	if m := contentHashLeading.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	if m := contentHashTrailing.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	return ""
}

// formatVersionMessage puts the content fingerprint in front of the operator's
// message.
//
// Confluence bounds the version message, and it used to be appended -- so a
// long --version-message (a commit subject, say) could push the tag past the
// limit and have it truncated away. Nothing failed loudly: the tag simply never
// matched on the next run, and --changes-only quietly degraded to updating
// every page every time, which is precisely the failure nobody notices.
//
// Leading with the tag means truncation eats the operator's prose, which is
// recoverable and visible, instead of the machinery that has to round-trip.
func formatVersionMessage(message, contentHash string) string {
	tag := fmt.Sprintf("[v%s]", contentHash)
	if message == "" {
		return tag
	}
	return tag + " " + message
}

// hasDrifted reports whether a page has been changed by somebody other than
// mark since mark last published it, put the way the report says it: why
// --no-overwrite leaves the page alone, naming the version mark wrote, or ""
// when it has not drifted. Shared by a real run and the --changes-only
// preview, so the two cannot disagree about either.
//
// The comparison is against the version number rather than the page's content:
// Confluence rewrites storage markup on save often enough that comparing bodies
// would report a difference on pages nobody had touched.
//
// An entry with no recorded version cannot answer the question -- it was
// written before versions were tracked, or by a run without --no-overwrite --
// and a page is given the benefit of the doubt rather than frozen.
//
// A page this run has already moved on Server or Data Center is compared as it
// was before the move. The move is an update there, so it lands a version
// before this check is reached; that version is mark's, and taking it for an
// edit left every moved page unpublished and reported on every run after. An
// edit made before the move still shows, since the version the move started
// from is then not the one recorded either.
//
// A source file that moved is compared against the entry of the path it moved
// from, for the one run on which its own path has none.
func hasDrifted(
	api *confluence.API,
	tracker *manifest.Store,
	spaceKey, file string,
	target *confluence.PageInfo,
) (string, error) {
	entry, ok, err := tracker.Lookup(spaceKey, file)
	if err != nil {
		return "", fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
	}

	// A document moved with its title unchanged is found by that title, and
	// its new path has no entry yet: the baseline is still in the one it moved
	// from, which Record only carries across after this. Asking the new path
	// alone read that as a page with no baseline, and the very run that moved
	// the file overwrote an edit --no-overwrite exists to keep.
	if !ok {
		entry, ok, err = tracker.LookupMoved(spaceKey, file, target.ID)
		if err != nil {
			return "", fmt.Errorf("unable to look up page mapping for %q: %w", file, err)
		}
	}

	if !ok || entry.Version == 0 || entry.PageID != target.ID {
		return "", nil
	}

	current := target.Version.Number
	if before, moved := api.ReparentedFrom(target.ID, current); moved {
		current = before
	}

	if current == entry.Version {
		return "", nil
	}

	return fmt.Sprintf(
		"edited in Confluence since mark published it (version %d, mark wrote %d)",
		target.Version.Number, entry.Version,
	), nil
}
