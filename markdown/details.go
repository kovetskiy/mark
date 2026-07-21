package mark

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// convertDetailsToExpand parses the html string, finds standard HTML <details> and <summary> tags,
// and maps them directly to native Confluence expand macro elements:
//
//	<ac:structured-macro ac:name="expand">
//	  <ac:parameter ac:name="title">Summary Title</ac:parameter>
//	  <ac:rich-text-body>
//	    Body Content
//	  </ac:rich-text-body>
//	</ac:structured-macro>
func convertDetailsToExpand(htmlStr string) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return "", err
	}

	var transform func(*html.Node)
	transform = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "details" {
			// Find summary node
			var summaryNode *html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "summary" {
					summaryNode = c
					break
				}
			}

			var summaryText string
			if summaryNode != nil {
				summaryText = strings.TrimSpace(extractText(summaryNode))
			}

			// Create replacement macro node
			macroNode := &html.Node{
				Type: html.ElementNode,
				Data: "ac:structured-macro",
				Attr: []html.Attribute{
					{Key: "ac:name", Val: "expand"},
				},
			}

			if summaryText != "" {
				paramNode := &html.Node{
					Type: html.ElementNode,
					Data: "ac:parameter",
					Attr: []html.Attribute{
						{Key: "ac:name", Val: "title"},
					},
				}
				paramNode.AppendChild(&html.Node{
					Type: html.TextNode,
					Data: summaryText,
				})
				macroNode.AppendChild(paramNode)
			}

			bodyNode := &html.Node{
				Type: html.ElementNode,
				Data: "ac:rich-text-body",
			}

			// Move all children of details except the summaryNode to the bodyNode
			var next *html.Node
			for c := n.FirstChild; c != nil; c = next {
				next = c.NextSibling
				if c != summaryNode {
					n.RemoveChild(c)
					bodyNode.AppendChild(c)
				}
			}

			macroNode.AppendChild(bodyNode)

			// Replace details node with macro node in the parent
			if n.Parent != nil {
				n.Parent.InsertBefore(macroNode, n)
				n.Parent.RemoveChild(n)
			}

			// Continue transforming the children of the newly created body node
			for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
				transform(c)
			}
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			transform(c)
		}
	}

	transform(doc)

	var buf bytes.Buffer
	err = html.Render(&buf, doc)
	if err != nil {
		return "", err
	}

	res := buf.String()
	if strings.Contains(htmlStr, "<html") {
		return res, nil
	}

	// html.Parse wraps nodes in <html><head></head><body>...</body></html>.
	// We extract only the inner HTML from the <body> tag to preserve fragment structure.
	if idx := strings.Index(res, "<body>"); idx != -1 {
		res = res[idx+len("<body>"):]
		if idxEnd := strings.LastIndex(res, "</body>"); idxEnd != -1 {
			res = res[:idxEnd]
		}
	}
	return res, nil
}

func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var buf strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		buf.WriteString(extractText(c))
	}
	return buf.String()
}
