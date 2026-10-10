package page_test

import (
	"testing"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/page"
	"github.com/stretchr/testify/assert"
)

// Confluence matches titles without regard to case, so a header that differs
// from the existing parent only in case names the page a real run leaves alone.
func TestWouldMoveIgnoresCaseOfDeclaredParentTitle(t *testing.T) {
	pg := &confluence.PageInfo{ID: "2"}
	pg.Ancestors = append(pg.Ancestors, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{ID: "1", Title: "My Parent"})
	parent := &confluence.PageInfo{ID: "1", Title: "My Parent"}

	assert.False(t, page.WouldMove(nil, pg, parent, []string{"my parent"}, false))
	assert.True(t, page.WouldMove(nil, pg, &confluence.PageInfo{ID: "9", Title: "My Parent"}, []string{"my parent"}, false),
		"a different page is still a move")
}
