package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTightDefinitionDescriptionKeepsItsLaterParagraphs: only the first
// paragraph of a tight description is a TextBlock. A second stays a <p>,
// rather than running on into the first as one run of text.
func TestTightDefinitionDescriptionKeepsItsLaterParagraphs(t *testing.T) {
	source := []byte("Term\n: first para\n\n    second para\n\n    third para\n")
	want := "<table>\n<tbody>\n<tr>\n<th>Term</th>\n<td>first para\n<p>second para</p>\n<p>third para</p>\n</td>\n</tr>\n</tbody>\n</table>\n"

	assert.Equal(t, want, compileDoc(t, string(source)))
}
