package mdsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommentBody(t *testing.T) {
	for _, tc := range []struct {
		line, want string
		ok         bool
	}{
		{"<!-- Title: A -->", "Title: A", true},
		{"  <!--ac:ignore-->  ", "ac:ignore", true},
		{"<!---->", "", true},
		{"<!-->", "", false},
		{"<!-- open", "", false},
		{"text <!-- x -->", "", false},
	} {
		got, ok := CommentBody(tc.line)
		assert.Equal(t, tc.ok, ok, "%q", tc.line)
		assert.Equal(t, tc.want, got, "%q", tc.line)
	}
}

func TestInCode(t *testing.T) {
	data := []byte("a `b` c\n\n```\nd\n```\n")
	regions := CodeRegions(data)

	for offset, want := range map[int]bool{0: false, 3: true, 6: false, 13: true} {
		assert.Equal(t, want, InCode(regions, offset), "offset %d (%q)", offset, data[offset])
	}
}
