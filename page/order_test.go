package page

import (
	"reflect"
	"testing"
)

// TestLongestIncreasingSubsequence pins the property the move count depends on:
// the pages left in place are the largest set already in the right order, so
// everything else has to move regardless and nothing else can be spared.
func TestLongestIncreasingSubsequence(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want []int
	}{
		{"already ordered", []int{0, 1, 2, 3}, []int{0, 1, 2, 3}},
		{"exactly reversed", []int{3, 2, 1, 0}, []int{3}},
		{"one out of place", []int{0, 3, 1, 2}, []int{0, 2, 3}},
		{"single", []int{7}, []int{0}},
		{"empty", nil, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := longestIncreasingSubsequence(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestLongestIncreasingSubsequenceIsMaximal checks the result really is the
// longest such run, by brute force over every subsequence of a small input.
// A merely-increasing answer would still pass the cases above.
func TestLongestIncreasingSubsequenceIsMaximal(t *testing.T) {
	in := []int{5, 1, 6, 2, 7, 3, 8, 0}

	best := 0
	for mask := 1; mask < 1<<len(in); mask++ {
		last, n, ok := -1, 0, true
		for i := range in {
			if mask&(1<<i) == 0 {
				continue
			}
			if in[i] <= last {
				ok = false
				break
			}
			last = in[i]
			n++
		}
		if ok && n > best {
			best = n
		}
	}

	if got := len(longestIncreasingSubsequence(in)); got != best {
		t.Errorf("kept %d in place, but %d could have been", got, best)
	}
}
