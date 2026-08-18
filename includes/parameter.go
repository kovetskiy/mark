package includes

import "bytes"

const (
	parameterOpen  = "<ac:parameter"
	parameterClose = "</ac:parameter>"
)

// TrimElementParameters removes the whitespace a template put inside an
// <ac:parameter> whose value is a storage-format element.
//
// A template is written to be read, so a macro parameter holding an attachment
// tends to be written across lines and indented:
//
//	<ac:parameter ac:name="name">
//	  <ri:attachment ri:filename="diagram.drawio"/>
//	</ac:parameter>
//
// Those newlines are published along with the element, which leaves the
// parameter holding text as well as an element. Confluence resolves that by
// writing the element out as a string, so the page ends up carrying
// AttachmentResourceIdentifier[...,filename=diagram.drawio] where the
// attachment should be -- and nothing is reported, because as far as mark is
// concerned it published exactly what it was given.
//
// Only a parameter whose content is an element is touched, which is decided by
// the trimmed content beginning with < and ending with >. A parameter holding a
// string is left exactly as written, spaces and all, because there the spacing
// is the value.
//
// Whitespace elsewhere is left alone, and has to be: the blank lines inside
// <ac:rich-text-body> are what let a macro's body be read as Markdown blocks,
// and a body ending in a list would otherwise swallow the closing tags.
func TrimElementParameters(content []byte) []byte {
	if !bytes.Contains(content, []byte(parameterOpen)) {
		return content
	}

	var out bytes.Buffer
	rest := content

	for {
		start := bytes.Index(rest, []byte(parameterOpen))
		if start == -1 {
			break
		}

		// The end of the opening tag, after any attributes.
		tagEnd := bytes.IndexByte(rest[start:], '>')
		if tagEnd == -1 {
			break
		}
		tagEnd += start + 1

		// A parameter that closes itself holds nothing to trim.
		if rest[tagEnd-2] == '/' {
			out.Write(rest[:tagEnd])
			rest = rest[tagEnd:]

			continue
		}

		closeAt := bytes.Index(rest[tagEnd:], []byte(parameterClose))
		if closeAt == -1 {
			break
		}
		closeAt += tagEnd

		inner := rest[tagEnd:closeAt]
		trimmed := bytes.TrimSpace(inner)

		out.Write(rest[:tagEnd])
		if len(trimmed) > 1 && trimmed[0] == '<' && trimmed[len(trimmed)-1] == '>' {
			out.Write(trimmed)
		} else {
			out.Write(inner)
		}
		out.WriteString(parameterClose)

		rest = rest[closeAt+len(parameterClose):]
	}

	out.Write(rest)

	return out.Bytes()
}
