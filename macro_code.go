package main

import "fmt"

type MacroCode struct {
	lang string
	code []byte
}

func (code MacroCode) Render() string {
	lang := ""
	if code.lang != "" {
		lang = fmt.Sprintf(`<ac:parameter ac:name="language">%s</ac:parameter>`, code.lang)
	}

	return fmt.Sprintf(
		`<ac:structured-macro ac:schema-version="1" ac:name="code">`+
			`%s`+
			`<ac:parameter ac:name="collapse">false</ac:parameter>`+
			`<ac:plain-text-body><![CDATA[%s]]></ac:plain-text-body>`+
			`</ac:structured-macro>`,
		lang, code.code,
	)
}
