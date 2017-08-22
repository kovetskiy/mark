package main

import (
	"bytes"
	"github.com/russross/blackfriday"
)
 

func appendIfMissing(slicePtr *[]string, data string) {
	slice := *slicePtr
    for _, ele := range slice {
        if ele == data {
            return
        }
    }
    *slicePtr = append(slice, data)
}


type ConfluenceRenderer struct {
	blackfriday.Renderer
	images * []string
}

func (renderer ConfluenceRenderer) BlockCode(
	out *bytes.Buffer,
	text []byte,
	lang string,
) {
	out.WriteString(MacroCode{lang, text}.Render())
}


func doubleSpace(out *bytes.Buffer) {
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
}



// Using if statements is a bit faster than a switch statement. As the compiler
// improves, this should be unnecessary this is only worthwhile because
// attrEscape is the single largest CPU user in normal use.
// Also tried using map, but that gave a ~3x slowdown.
func escapeSingleChar(char byte) (string, bool) {
	if char == '"' {
		return "&quot;", true
	}
	if char == '&' {
		return "&amp;", true
	}
	if char == '<' {
		return "&lt;", true
	}
	if char == '>' {
		return "&gt;", true
	}
	return "", false
}

func attrEscape(out *bytes.Buffer, src []byte) {
	org := 0
	for i, ch := range src {
		if entity, ok := escapeSingleChar(ch); ok {
			if i > org {
				// copy all the normal characters since the last escape
				out.Write(src[org:i])
			}
			org = i + 1
			out.WriteString(entity)
		}
	}
	if org < len(src) {
		out.Write(src[org:])
	}
}


func (renderer ConfluenceRenderer) Image(
	out *bytes.Buffer, 
	link []byte, 
	title []byte, 
	alt []byte,
) {
	

	if (!bytes.Contains(link, []byte(`/`)) ) {
		logger.Tracef("Local Image: %s (%s) -> %s\n", title, alt, link)
		appendIfMissing(renderer.images, string(link))
		out.WriteString(MacroImage{link, title, alt, 250 }.Render())
	} else {
		out.WriteString("<img src=\"")
		// options.maybeWriteAbsolutePrefix(out, link)
		attrEscape(out, link)
		out.WriteString("\" alt=\"")
		if len(alt) > 0 {
			attrEscape(out, alt)
		}
		if len(title) > 0 {
			out.WriteString("\" title=\"")
			attrEscape(out, title)
		}

		out.WriteString("\" />")
	}
	
}