package page_test

import (
	"testing"

	"github.com/kovetskiy/mark/v17/page"
	"github.com/kovetskiy/mark/v17/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnOrderWithNoKnownParentIsAPlacementWarning: a page that asked for an
// Order and has no parent to be ordered under is told so, not only logged.
func TestAnOrderWithNoKnownParentIsAPlacementWarning(t *testing.T) {
	warnings := report.NewWarnings("docs/a.md")

	require.NoError(t, page.OrderChildren(t.Context(), nil, false, []page.Ordered{
		{PageID: "1", Title: "A", Order: 2, Warnings: warnings},
	}))

	list := warnings.List()
	require.Len(t, list, 1)
	assert.Equal(t, report.WarningPlacement, list[0].Kind)
	assert.Equal(t, `page "A" asked to be ordered 2 but its parent is not known, so it is left where it is`, list[0].Message)
}
