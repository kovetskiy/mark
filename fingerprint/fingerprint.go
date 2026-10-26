// Package fingerprint is the content fingerprint behind --changes-only and
// --no-overwrite, and how it is carried in a page's version message.
package fingerprint

import (
	// SHA-1 is used only as a content fingerprint for --changes-only, never as
	// a security primitive. The digest is embedded in the page version message
	// and matched back with a 40-hex-character regex, so widening it would stop
	// mark from recognising pages published by earlier versions.
	"crypto/sha1" //nolint:gosec // G505: non-cryptographic content fingerprint
	"encoding/hex"
	"fmt"
	"regexp"
)

// SHA1 is the hex SHA-1 digest of input.
func SHA1(input string) string {
	h := sha1.New() //nolint:gosec // G401: see the crypto/sha1 import comment
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// Content is what --changes-only compares: the body, and the page
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
func Content(html, appearance, defaultAppearance, emoji string) string {
	if emoji == "" && (appearance == "" || appearance == defaultAppearance) {
		return SHA1(html)
	}

	return SHA1(html + "\x00mark:content-appearance=" + appearance + "\x00mark:emoji=" + emoji)
}

var (
	contentHashLeading  = regexp.MustCompile(`^\[v([a-f0-9]{40})]`)
	contentHashTrailing = regexp.MustCompile(`\[v([a-f0-9]{40})]$`)
)

// FromVersionMessage recovers the fingerprint VersionMessage wrote, or "" if
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
func FromVersionMessage(message string) string {
	if m := contentHashLeading.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	if m := contentHashTrailing.FindStringSubmatch(message); m != nil {
		return m[1]
	}
	return ""
}

// VersionMessage puts the content fingerprint in front of the operator's
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
func VersionMessage(message, contentHash string) string {
	tag := fmt.Sprintf("[v%s]", contentHash)
	if message == "" {
		return tag
	}
	return tag + " " + message
}
