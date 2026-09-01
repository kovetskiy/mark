package transformer

import "testing"

// TestAnchorKey pins what the two conventions are reduced to before they are
// compared. mark keeps capitals and punctuation in an id; a Markdown author
// writes the lowercase-and-hyphens slug every other tool produces. Only the
// letters and digits are common to both.
func TestAnchorKey(t *testing.T) {
	same := [][2]string{
		{"My-Heading", "my-heading"},
		{"API/v2-Guide", "apiv2-guide"},    // a slug drops the slash, an id keeps it
		{"Release.Notes", "release-notes"}, // and the dot
		{"Heading_With_Underscores", "heading-with-underscores"},
		{"Ünïcödé", "ünïcödé"}, // case folding has to reach past ASCII
	}
	for _, pair := range same {
		if anchorKey(pair[0]) != anchorKey(pair[1]) {
			t.Errorf("%q and %q should reduce alike, got %q and %q",
				pair[0], pair[1], anchorKey(pair[0]), anchorKey(pair[1]))
		}
	}

	differ := [][2]string{
		{"Heading One", "Heading Two"},
		{"Release-Notes", "Release-Notes-1"}, // goldmark's dedupe suffix is a digit
	}
	for _, pair := range differ {
		if anchorKey(pair[0]) == anchorKey(pair[1]) {
			t.Errorf("%q and %q should not reduce alike, both gave %q",
				pair[0], pair[1], anchorKey(pair[0]))
		}
	}

	// Nothing to match on is not a match against everything.
	if anchorKey("---") != "" {
		t.Errorf("punctuation alone should reduce to nothing, got %q", anchorKey("---"))
	}
}
