package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/urfave/cli/v3"
)

// Metadata flag names are shared by flag declarations and command handling.
const (
	metadataSpaceFlag         = "space"
	metadataInSpaceFlag       = "in-space"
	metadataPageFlag          = "page"
	metadataBlogpostFlag      = "blogpost"
	metadataAttachmentFlag    = "attachment"
	metadataCustomContentFlag = "custom-content"
	metadataPersonalFlag      = "personal"
	metadataJSONFlag          = "json"
)

// metadataCommand groups commands for working with Confluence metadata.
// Selectors come only from the command line; a configured publishing space must not scope a metadata listing.
func (line *commandLine) metadataCommand() *cli.Command {
	return &cli.Command{
		Name:  "metadata",
		Usage: "work with Confluence metadata (experimental)",
		Commands: []*cli.Command{
			{
				Name:  "labels",
				Usage: "inspect labels",
				Commands: []*cli.Command{
					{
						Name:  "list",
						Usage: "list visible labels; without a selector, list site-wide labels",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: metadataInSpaceFlag, Usage: "labels used by content in this space key."},
							&cli.StringFlag{Name: metadataSpaceFlag, Aliases: []string{"s"}, Usage: "labels attached to the space itself, by space key."},
							&cli.StringFlag{Name: metadataPageFlag, Usage: "labels attached to this page, by ID or URL."},
							&cli.StringFlag{Name: metadataBlogpostFlag, Usage: "labels attached to this blog post, by ID or URL."},
							&cli.StringFlag{Name: metadataAttachmentFlag, Usage: "labels attached to this attachment, by ID or URL."},
							&cli.StringFlag{Name: metadataCustomContentFlag, Usage: "labels attached to this custom content item, by ID or URL."},
							line.metadataJSONFlag(),
						},
						Action: line.runMetadataLabels,
					},
				},
			},
			{
				Name:  "statuses",
				Usage: "inspect content statuses",
				Commands: []*cli.Command{
					{
						Name:  "list",
						Usage: "list statuses; choose one explicit selector",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: metadataSpaceFlag, Aliases: []string{"s"}, Usage: "suggested statuses configured for this space key."},
							&cli.BoolFlag{Name: metadataPersonalFlag, Usage: "custom statuses created by the authenticated user."},
							&cli.StringFlag{Name: metadataPageFlag, Usage: "status assigned to this page, by ID or URL, if any."},
							line.metadataJSONFlag(),
						},
						Action: line.runMetadataStatuses,
					},
				},
			},
		},
	}
}

// metadataJSONFlag selects machine-readable output instead of the default table.
func (line *commandLine) metadataJSONFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:    metadataJSONFlag,
		Usage:   "write results as JSON instead of a table.",
		Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_JSON"), line.config.value(metadataJSONFlag)),
	}
}

// metadataSelector checks explicit, mutually exclusive selectors before authentication.
func metadataSelector(cmd *cli.Command) (string, error) {
	if cmd.NArg() != 0 {
		return "", fmt.Errorf("unexpected argument %q", cmd.Args().First())
	}

	var selected string

	for _, flag := range cmd.Flags {
		name := flag.Names()[0]
		switch name {
		case metadataSpaceFlag, metadataInSpaceFlag, metadataPageFlag, metadataBlogpostFlag,
			metadataAttachmentFlag, metadataCustomContentFlag, metadataPersonalFlag:
		default:
			continue
		}

		if !cmd.IsSet(name) {
			continue
		}

		if selected != "" {
			return "", fmt.Errorf("specify only one metadata selector: --%s and --%s conflict", selected, name)
		}

		if name == metadataPersonalFlag {
			if !cmd.Bool(name) {
				return "", errors.New("--personal must be true when supplied")
			}
		} else if strings.TrimSpace(cmd.String(name)) == "" {
			return "", fmt.Errorf("--%s cannot be empty", name)
		}

		selected = name
	}

	return selected, nil
}

// metadataCollection maps object selectors to the Confluence label collections.
func metadataCollection(selector string) string {
	switch selector {
	case metadataSpaceFlag:
		return confluence.LabelCollectionSpaces
	case metadataPageFlag:
		return confluence.LabelCollectionPages
	case metadataBlogpostFlag:
		return confluence.LabelCollectionBlogposts
	case metadataAttachmentFlag:
		return confluence.LabelCollectionAttachments
	case metadataCustomContentFlag:
		return confluence.LabelCollectionCustomContent
	}
	return ""
}

// metadataReference reads content selectors before authentication.
func metadataReference(cmd *cli.Command, selector string) (confluence.ContentRef, error) {
	collection := metadataCollection(selector)
	if collection == "" || collection == confluence.LabelCollectionSpaces {
		return confluence.ContentRef{}, nil
	}
	value := cmd.String(selector)
	if strings.Contains(value, "://") {
		return confluence.ParseContentURL(value, collection)
	}
	if selector == metadataAttachmentFlag {
		value = strings.TrimPrefix(value, "att")
	}
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return confluence.ContentRef{}, fmt.Errorf("--%s expects an ID or Confluence URL", selector)
	}
	return confluence.ContentRef{ID: value}, nil
}

// metadataAPI resolves the shared connection settings and authentication.
func (line *commandLine) metadataAPI(ctx context.Context, cmd *cli.Command, contentBase string) (*confluence.API, error) {
	if err := line.prepare(cmd); err != nil {
		return nil, err
	}

	baseURL := cmd.String("base-url")
	if baseURL == "" {
		baseURL = contentBase
	} else if contentBase != "" {
		if err := confluence.SameInstance(baseURL, contentBase); err != nil {
			return nil, err
		}
	}
	creds, err := GetCredentials(ctx, cmd.String("username"), cmd.String("password"), cmd.String("password-command"), "", baseURL, false)
	if err != nil {
		return nil, err
	}

	return confluence.NewAPI(creds.BaseURL, confluence.Options{Username: creds.Username, Password: creds.Password, InsecureSkipVerify: cmd.Bool("insecure-skip-tls-verify")}), nil
}

// runMetadataLabels writes every matching label in the selected format.
func (line *commandLine) runMetadataLabels(ctx context.Context, cmd *cli.Command) error {
	selector, err := metadataSelector(cmd)
	if err != nil {
		return err
	}

	ref, err := metadataReference(cmd, selector)
	if err != nil {
		return err
	}
	api, err := line.metadataAPI(ctx, cmd, ref.BaseURL)
	if err != nil {
		return err
	}

	query := confluence.LabelQuery{Collection: metadataCollection(selector), ID: cmd.String(selector)}
	if selector == metadataInSpaceFlag {
		query.Space = cmd.String(selector)
	}
	if query.Collection == confluence.LabelCollectionSpaces {
		query.ID, err = api.GetSpaceID(ctx, cmd.String(selector))
	} else if query.Collection != "" {
		contentType := confluence.ContentPage
		if selector == metadataBlogpostFlag {
			contentType = confluence.ContentBlogPost
		}
		query.ID, err = api.ResolveContentID(ctx, ref, contentType)
	}
	if err != nil {
		return err
	}

	labels, err := api.ListLabels(ctx, query)
	if err != nil {
		return err
	}

	if err := writeMetadataLabels(cmd.Root().Writer, labels, cmd.Bool(metadataJSONFlag)); err != nil {
		return fmt.Errorf("unable to write labels: %w", err)
	}

	return nil
}

// runMetadataStatuses writes statuses from the selected source or shows usage help.
func (line *commandLine) runMetadataStatuses(ctx context.Context, cmd *cli.Command) error {
	selector, err := metadataSelector(cmd)
	if err != nil {
		return err
	}

	if selector == "" {
		return cli.ShowSubcommandHelp(cmd)
	}

	ref, err := metadataReference(cmd, selector)
	if err != nil {
		return err
	}
	api, err := line.metadataAPI(ctx, cmd, ref.BaseURL)
	if err != nil {
		return err
	}

	var statuses []confluence.ContentStatus

	switch selector {
	case metadataSpaceFlag:
		statuses, err = api.GetSpaceContentStatuses(ctx, cmd.String(selector))
	case metadataPersonalFlag:
		statuses, err = api.GetPersonalContentStatuses(ctx)
	case metadataPageFlag:
		var id string
		id, err = api.ResolveContentID(ctx, ref, confluence.ContentPage)
		if err == nil {
			statuses, err = api.GetPageContentStatus(ctx, id)
		}
	}

	if err != nil {
		return err
	}

	if err := writeMetadataStatuses(cmd.Root().Writer, statuses, cmd.Bool(metadataJSONFlag)); err != nil {
		return fmt.Errorf("unable to write statuses: %w", err)
	}

	return nil
}

// writeMetadataLabels prints a label table or a JSON array.
func writeMetadataLabels(out io.Writer, labels []confluence.Label, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(labels)
	}

	if len(labels) == 0 {
		_, err := fmt.Fprintln(out, "No labels found.")
		return err
	}

	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "NAME\tID\tPREFIX"); err != nil {
		return err
	}

	for _, label := range labels {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", label.Name, label.ID, label.Prefix); err != nil {
			return err
		}
	}

	return table.Flush()
}

// writeMetadataStatuses prints a status table or a JSON array.
func writeMetadataStatuses(out io.Writer, statuses []confluence.ContentStatus, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(statuses)
	}

	if len(statuses) == 0 {
		_, err := fmt.Fprintln(out, "No statuses found.")
		return err
	}

	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "NAME\tID\tCOLOR"); err != nil {
		return err
	}

	for _, status := range statuses {
		if _, err := fmt.Fprintf(table, "%s\t%d\t%s\n", status.Name, status.ID, status.Color); err != nil {
			return err
		}
	}

	return table.Flush()
}
