package page_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/page"
)

// TestOrderChildrenReachesEveryPermutation orders four siblings into each of
// their 24 arrangements and reads the tree back. Pages that lead the wanted
// order and all have to move are the case that matters: each is placed
// against the next, so none of them is ever anchored to a page that stays.
func TestOrderChildrenReachesEveryPermutation(t *testing.T) {
	names := []string{"A", "B", "C", "D"}

	for _, perm := range permutations(names) {
		t.Run(strings.Join(perm, ""), func(t *testing.T) {
			api, server := newAPI(t)
			parent := server.AddPage("DOCS", "Parent", "page", "")

			ids := map[string]string{}
			for _, name := range names {
				ids[name] = server.AddPage("DOCS", name, "page", parent.ID).ID
			}

			wanted := make([]page.Ordered, 0, len(perm))
			want := make([]string, 0, len(perm))
			for i, name := range perm {
				wanted = append(wanted, page.Ordered{
					PageID: ids[name], ParentID: parent.ID, Title: name, Order: i + 1,
				})
				want = append(want, ids[name])
			}

			if err := page.OrderChildren(api, false, wanted); err != nil {
				t.Fatalf("OrderChildren: %v", err)
			}

			if got := server.ChildOrder(parent.ID); !reflect.DeepEqual(got, want) {
				t.Errorf("children are %v, want %v", got, want)
			}
		})
	}
}

func permutations(items []string) [][]string {
	if len(items) <= 1 {
		return [][]string{append([]string(nil), items...)}
	}

	var out [][]string
	for i := range items {
		rest := append(append([]string(nil), items[:i]...), items[i+1:]...)
		for _, tail := range permutations(rest) {
			out = append(out, append([]string{items[i]}, tail...))
		}
	}

	return out
}
