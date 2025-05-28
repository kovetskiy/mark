package parser

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

type ConfluenceIDs struct {
	Values map[string]bool
}

// https://github.com/yuin/goldmark/blob/d9c03f07f08c2d36f23afe52dda865f05320ac86/parser/parser.go#L75
func (s *ConfluenceIDs) Generate(value []byte, kind ast.NodeKind) []byte {
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
	s.Values[util.BytesToReadOnlyString(value)] = true
}
