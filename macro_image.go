package main

import (
	"fmt"
	"os"
	"bytes"
	"path/filepath"
	"image"
	_ "image/gif"
	_ "image/png"
	_ "image/jpeg"
)

type MacroImage struct {
	Path string
	Title string
	Alt string
	Height int
	filename string
	Data []byte
	// border bool
}




func newMacroImage(path, title, alt string ) (*MacroImage, error) {
	m := new (MacroImage)
	m.Path = path

	m.filename = filepath.Base(path)
	if title == "" {
		extension := filepath.Ext(path)
		title = m.filename[0:len(m.filename)-len(extension)]
	} else {
		m.Title = title
	}
	m.Alt = alt

	reader, err := os.Open(path)
  	 if err != nil {
		return nil, err
  	 }
  	  
	buf := new(bytes.Buffer)
	buf.ReadFrom(reader)
	m.Data = buf.Bytes()
	reader.Close()

	img, _, err := image.Decode(bytes.NewReader(m.Data))
	  
  	if err != nil {
  		return nil, err
  	}
	bounds := img.Bounds()

	if bounds.Max.Y > 250 {
		m.Height = 250
	} else {
		m.Height = bounds.Max.Y
	}
	  
	return m, nil
}

func (image MacroImage) Render() string {
	return fmt.Sprintf(
		`<ac:image ac:alt="%s" ac:height="%d"  ac:title="%s" ><ri:attachment ri:filename="%s" /></ac:image>`,
		// ac:border="%s" 
		image.Alt,
		image.Height, 
		image.Title,
	//	image.border,
		filepath.Base(image.Path),
	)
}
