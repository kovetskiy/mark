package main

import "fmt"

type MacroLayout struct {
	layout  string
	columns [][]byte
}

func (layout MacroLayout) Render() string {
	switch layout.layout {
	case "plain":
		return string(layout.columns[0])

	case "article":
		fallthrough

	default:
		return fmt.Sprintf(
			`<ac:layout>`+
				`<ac:layout-section ac:type="two_right_sidebar">`+
				`<ac:layout-cell>%s</ac:layout-cell>`+
				`<ac:layout-cell></ac:layout-cell>`+
				`</ac:layout-section>`+
				`</ac:layout>`,
			string(layout.columns[0]),
		)
	}
}
