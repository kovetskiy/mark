package main

import "fmt"

type MacroImage struct {
	link []byte
	title []byte
	alt []byte
	height int
	// border bool
}

func (image MacroImage) Render() string {
	return fmt.Sprintf(
		`<ac:image ac:height="%d" ac:alt="%s"  ac:title="%s" ><ri:attachment ri:filename="%s" /></ac:image>`,
		// ac:border="%s" 
		image.height, 
		image.alt,
		image.title,
	//	image.border,
		image.link,
	)
}
