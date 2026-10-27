// Checking titles against the length Confluence accepts.

package mark

import (
	"fmt"
	"unicode/utf16"

	"github.com/kovetskiy/mark/v16/metadata"
)

// maxTitleLength is the longest title Confluence accepts for a page or a
// folder, counted as Java counts a string: in UTF-16 code units, so a
// character outside the Basic Multilingual Plane, an emoji, counts twice.
const maxTitleLength = 255

// checkTitleLengths refuses a document whose page, or a parent or folder mark
// would create for it, has a title Confluence will not take -- before anything
// is created, rather than with a 400 once its parents exist.
func checkTitleLengths(meta *metadata.Meta) error {
	check := func(what, title string) error {
		if n := len(utf16.Encode([]rune(title))); n > maxTitleLength {
			return fmt.Errorf(
				"%s title %q is %d characters long, and Confluence takes at most %d",
				what, title, n, maxTitleLength,
			)
		}
		return nil
	}

	if err := check("page", meta.Title); err != nil {
		return err
	}
	for _, parent := range meta.Parents {
		if err := check("parent", parent); err != nil {
			return err
		}
	}
	for _, folder := range meta.Folders {
		if err := check("folder", folder); err != nil {
			return err
		}
	}

	return nil
}
