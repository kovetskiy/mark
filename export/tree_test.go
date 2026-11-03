package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A page id goes into the paths a tree is written to, so one that could
// leave the output directory is refused.
func TestCheckPageID(t *testing.T) {
	assert.NoError(t, checkPageID("123456"))
	for _, id := range []string{"", "..", "../..", "12/34", `12\34`, "12a", " 12"} {
		assert.Error(t, checkPageID(id), "%q", id)
	}
}
