package parser

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

// ConfluenceIDs implements parser.IDs for Confluence-compatible header anchor IDs,
// preserving '/', '_', '.', and '-' characters in heading IDs.
type ConfluenceIDs struct {
	Values map[string]bool
}

// NewConfluenceIDs creates a new ConfluenceIDs instance.
func NewConfluenceIDs() *ConfluenceIDs {
	return &ConfluenceIDs{
		Values: make(map[string]bool),
	}
}
func (s *ConfluenceIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	if s.Values == nil {
		s.Values = make(map[string]bool)
	}
	value = util.TrimLeftSpace(value)
	value = util.TrimRightSpace(value)
	result := []byte{}
	for i := 0; i < len(value); {
		v := value[i]
		l := util.UTF8Len(v)
		i += int(l)
		if l != 1 {
			continue
		}
		if util.IsAlphaNumeric(v) || v == '/' || v == '_' || v == '.' {
			result = append(result, v)
		} else if util.IsSpace(v) || v == '-' {
			result = append(result, '-')
		}
	}
	if len(result) == 0 {
		if kind == ast.KindHeading {
			result = []byte("heading")
		} else {
			result = []byte("id")
		}
	}
	if _, ok := s.Values[util.BytesToReadOnlyString(result)]; !ok {
		s.Values[util.BytesToReadOnlyString(result)] = true
		return result
	}
	for i := 1; ; i++ {
		newResult := fmt.Sprintf("%s-%d", result, i)
		if _, ok := s.Values[newResult]; !ok {
			s.Values[newResult] = true
			return []byte(newResult)
		}
	}
}

func (s *ConfluenceIDs) Put(value []byte) {
	if s.Values == nil {
		s.Values = make(map[string]bool)
	}
	s.Values[util.BytesToReadOnlyString(value)] = true
}
