package mark

import (
	"context"
	"io"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/export"
)

// ExportConfig holds the options of Export: where Confluence is and how to
// sign in to it, as for Config, and which page to export to where.
type ExportConfig struct {
	Connection ConnectionConfig

	// PageID names the page to export. Without it, Space and Title do.
	PageID string
	Space  string
	Title  string

	// Output is the Markdown file to write, or "" (or "-") for Stdout. Its
	// name is kept from the attachments written beside it: one named like it
	// gets a number instead.
	Output string

	// AttachmentsDir is where the attachments the page refers to are
	// written; "" is the directory Output is in. With no Output, Export
	// refuses an empty one with export.ErrNoAttachmentsDir unless
	// NoAttachments is set: "." writes them to the working directory, as the
	// command line's mark export does beside standard output.
	AttachmentsDir string

	// Overwrite replaces files that are already there instead of failing.
	Overwrite bool

	// NoAttachments writes the Markdown only.
	NoAttachments bool

	// Format is export.FormatMark, or "", for documents mark publishes back,
	// or export.FormatOKF for an Open Knowledge Format concept or bundle.
	Format export.Format

	// Metadata is how documents of export.FormatMark spell the metadata that
	// names their page: export.MetadataComments, or "", for HTML comment
	// headers, or export.MetadataFrontMatter for YAML front matter, which
	// mark publishes only with the frontmatter feature, or
	// export.MetadataNone for the body alone, which ExportTree refuses with
	// export.ErrTreeWithoutMetadata.
	Metadata export.Metadata

	// Generator is who an OKF concept says generated it, as
	// <producer>/<version>; empty is "mark/unknown".
	Generator string

	// Stdout receives the Markdown when Output names no file. Nil discards
	// it, as PublishConfig.Output does for a library caller that sets nothing;
	// the Result carries the Markdown either way.
	Stdout io.Writer
}

// Export writes a Confluence page out as a Markdown document that mark can
// publish back to the same page. See the export package for what is converted
// and what is kept as storage format.
//
// A caller that wants the page's title, labels and body as values, and no
// files, sets Metadata to export.MetadataNone and NoAttachments, and leaves
// Output and Stdout unset: the Result's Title, Labels and Markdown are then
// all there is. With no Output, the attachments the body refers to need
// either NoAttachments or an AttachmentsDir to be written to; with neither,
// Export fails with export.ErrNoAttachmentsDir before asking for the page.
func Export(ctx context.Context, config ExportConfig) (*export.Result, error) {
	api := config.Connection.api(confluence.Options{})

	stdout := config.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	return export.Page(ctx, api, exportConfig(config, stdout))
}

// ExportTree writes the page config names and the pages beneath it, or with
// no page the whole of config.Space, into the directory config.Output names.
// See export.Tree for the layout. Every Result carries its page's Markdown,
// so the results of a large tree hold every document in memory until the
// caller lets them go.
func ExportTree(ctx context.Context, config ExportConfig, options export.TreeOptions) ([]*export.Result, error) {
	api := config.Connection.api(confluence.Options{})

	stdout := config.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	return export.Tree(ctx, api, exportConfig(config, stdout), options)
}

func exportConfig(config ExportConfig, stdout io.Writer) export.Config {
	return export.Config{
		PageID:         config.PageID,
		Space:          config.Space,
		Title:          config.Title,
		Output:         config.Output,
		AttachmentsDir: config.AttachmentsDir,
		Overwrite:      config.Overwrite,
		NoAttachments:  config.NoAttachments,
		Format:         config.Format,
		Metadata:       config.Metadata,
		Generator:      config.Generator,
		Stdout:         stdout,
	}
}
