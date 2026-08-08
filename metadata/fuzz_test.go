package metadata

import (
	"testing"
)

// FuzzExtractMeta drives the metadata parser with arbitrary input. ExtractMeta
// slices the document by byte offsets taken from the Goldmark AST, which is
// where issue #686 crashed ("slice bounds out of range [8:7]") on a file whose
// only line was a heading with no trailing newline.
//
// The contract asserted here is narrow on purpose: ExtractMeta may return an
// error, and may return nil metadata, but it must never panic, and the body it
// returns must be a suffix-compatible slice of the input rather than something
// longer than what it was given.
func FuzzExtractMeta(f *testing.F) {
	seeds := []string{
		"",
		"# Blogs",
		"# Blogs\n",
		"<!-- Space: DOCS -->",
		"<!-- Space: DOCS -->\n<!-- Title: X -->",
		"<!-- Space: DOCS -->\r\n<!-- Title: X -->\r\n",
		"<!-- Space: DOCS -->\n# Heading\n\nbody",
		"<!-- Include: foo.md -->",
		"<!-- -->",
		"<!--",
		"<!-->",
		"---\nspace: DOCS\ntitle: X\n---",
		"---\nspace: DOCS\n---\nbody",
		"---\n",
		"---",
		"<!-- Space: -->",
		"<!-- : value -->",
		"<!-- Attachment: a.png -->\n<!-- Attachment: b.png -->",
		"\n\n\n",
		"<!-- Space: DOCS --><!-- Title: X -->",
	}
	for _, s := range seeds {
		f.Add([]byte(s), "SPACE", true, false, true)
	}

	f.Fuzz(func(t *testing.T, data []byte, space string, titleFromH1, titleFromFilename, frontMatter bool) {
		meta, body, err := ExtractMeta(
			data, space, titleFromH1, titleFromFilename,
			"fuzz.md", nil, false, "", frontMatter,
		)
		if err != nil {
			// An error is a legitimate outcome; it just must not be a panic.
			return
		}
		if len(body) > len(data) {
			t.Fatalf("body (%d bytes) is longer than the input (%d bytes)", len(body), len(data))
		}
		if meta != nil && meta.Type == "" {
			t.Fatalf("metadata returned with an empty Type: %+v", meta)
		}
	})
}
