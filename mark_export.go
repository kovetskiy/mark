package mark

import (
	"context"
	"io"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/export"
)

// ExportConfig holds the options of Export: where Confluence is and how to
// sign in to it, as for Config, and which page to export to where.
type ExportConfig struct {
	BaseURL               string
	Username              string
	Password              string
	InsecureSkipTLSVerify bool

	// PageID names the page to export. Without it, Space and Title do.
	PageID string
	Space  string
	Title  string

	// Output is the Markdown file to write, or "" (or "-") for Stdout.
	Output string

	// AttachmentsDir is where the attachments the page refers to are
	// written; "" is the directory Output is in.
	AttachmentsDir string

	// Overwrite replaces files that are already there instead of failing.
	Overwrite bool

	// NoAttachments writes the Markdown only.
	NoAttachments bool

	// Stdout receives the Markdown when Output names no file. Nil discards
	// it, as Config.Output does for a library caller that sets nothing.
	Stdout io.Writer
}

// Export writes a Confluence page out as a Markdown document that mark can
// publish back to the same page. See the export package for what is converted
// and what is kept as storage format.
func Export(ctx context.Context, config ExportConfig) (*export.Result, error) {
	api := confluence.NewAPI(config.BaseURL, config.Username, config.Password, config.InsecureSkipTLSVerify)

	stdout := config.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	return export.Page(ctx, api, export.Config{
		PageID:         config.PageID,
		Space:          config.Space,
		Title:          config.Title,
		Output:         config.Output,
		AttachmentsDir: config.AttachmentsDir,
		Overwrite:      config.Overwrite,
		NoAttachments:  config.NoAttachments,
		Stdout:         stdout,
	})
}
