package main

import "fmt"

type MacroCode struct {
	lang string
	code []byte
}

func (code MacroCode) Render() string {
	return fmt.Sprintf(
		`<ac:structured-macro ac:name="code">`+
			`<ac:parameter ac:name="language">%s</ac:parameter>`+
			`<ac:parameter ac:name="collapse">false</ac:parameter>`+
			`<ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body>`+
			`</ac:structured-macro>`,
		code.lang, code.code,
	)
}
