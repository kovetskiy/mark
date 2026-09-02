package transformer

import "testing"

// TestAnchorKeyKeepsNonASCIILetters pins the other end of the non-ASCII
// heading id.
//
// anchorKey used to test for a-z and 0-9, so an id and a link target written
// in CJK or Cyrillic both reduced to nothing. The empty key is discarded by
// Transform, which means every such heading was invisible to anchor matching
// no matter what the author wrote.
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
