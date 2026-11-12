package syntax

// IsLiteralElement reports whether the storage format element called name
// holds a string Confluence reads as it stands -- a pattern, a query, a URL,
// an id -- so that the text between its tags is not Markdown. A macro's
// ac:rich-text-body is Markdown, and is not one.
func IsLiteralElement(name string) bool {
	switch name {
	case "ac:parameter", "ac:default-parameter", "ac:task-id", "ac:task-status":
		return true
	}

	return false
}
