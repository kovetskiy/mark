package syntax

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
		if AnchorKey(pair[0]) != AnchorKey(pair[1]) {
			t.Errorf("%q and %q should reduce alike, got %q and %q",
				pair[0], pair[1], AnchorKey(pair[0]), AnchorKey(pair[1]))
		}
	}

	differ := [][2]string{
		{"Heading One", "Heading Two"},
		{"Release-Notes", "Release-Notes-1"}, // goldmark's dedupe suffix is a digit
	}
	for _, pair := range differ {
		if AnchorKey(pair[0]) == AnchorKey(pair[1]) {
			t.Errorf("%q and %q should not reduce alike, both gave %q",
				pair[0], pair[1], AnchorKey(pair[0]))
		}
	}

	// Nothing to match on is not a match against everything.
	if AnchorKey("---") != "" {
		t.Errorf("punctuation alone should reduce to nothing, got %q", AnchorKey("---"))
	}
}

// Non-ASCII letters must survive, or CJK and Cyrillic ids reduce to the empty
// key and never match.
func TestAnchorKeyKeepsNonASCIILetters(t *testing.T) {
	kept := []string{"概要", "Обзор", "Über"}
	for _, value := range kept {
		if AnchorKey(value) == "" {
			t.Errorf("%q should reduce to a key, got nothing", value)
		}
	}

	differ := [][2]string{
		{"概要", "詳細"},
		{"Обзор", "Раздел"},
	}
	for _, pair := range differ {
		if AnchorKey(pair[0]) == AnchorKey(pair[1]) {
			t.Errorf("%q and %q should not reduce alike, both gave %q",
				pair[0], pair[1], AnchorKey(pair[0]))
		}
	}

	// The punctuation an id keeps and a slug drops is still dropped on both
	// sides, in every script.
	if AnchorKey("概要/詳細") != AnchorKey("概要-詳細") {
		t.Errorf("punctuation should not separate %q from %q", "概要/詳細", "概要-詳細")
	}
}
