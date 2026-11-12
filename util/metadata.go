package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/urfave/cli/v3"
)

// Metadata selector flags are shared by flag declarations and listing dispatch.
const (
	metadataSpaceFlag           = "space"
	metadataSpaceIDFlag         = "space-id"
	metadataPageIDFlag          = "page-id"
	metadataBlogpostIDFlag      = "blogpost-id"
	metadataAttachmentIDFlag    = "attachment-id"
	metadataCustomContentIDFlag = "custom-content-id"
	metadataPersonalFlag        = "personal"
)

// metadataCommand groups commands for working with Confluence metadata.
func (line *commandLine) metadataCommand() *cli.Command {
	return &cli.Command{
		Name: "metadata", Usage: "work with Confluence metadata",
		Commands: []*cli.Command{
			{Name: "labels", Usage: "inspect labels", Commands: []*cli.Command{
				{Name: "list", Usage: "list visible labels as JSON; without a selector, list site-wide labels",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: metadataSpaceFlag, Aliases: []string{"s"}, Usage: "labels used by content in this space key."},
						&cli.StringFlag{Name: metadataSpaceIDFlag, Usage: "labels attached to the space itself."},
						&cli.StringFlag{Name: metadataPageIDFlag, Usage: "labels attached to this page."},
						&cli.StringFlag{Name: metadataBlogpostIDFlag, Usage: "labels attached to this blog post."},
						&cli.StringFlag{Name: metadataAttachmentIDFlag, Usage: "labels attached to this attachment."},
						&cli.StringFlag{Name: metadataCustomContentIDFlag, Usage: "labels attached to this custom content item."},
					}, Action: line.runMetadataLabels},
			}},
			{Name: "statuses", Usage: "inspect content statuses", Commands: []*cli.Command{
				{Name: "list", Usage: "list statuses as JSON; choose one explicit selector",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: metadataSpaceFlag, Aliases: []string{"s"}, Usage: "suggested statuses configured for this space key."},
						&cli.BoolFlag{Name: metadataPersonalFlag, Usage: "custom statuses created by the authenticated user."},
						&cli.StringFlag{Name: metadataPageIDFlag, Usage: "status assigned to this page, or an empty array."},
					}, Action: line.runMetadataStatuses},
			}},
		},
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
		if !cmd.IsSet(name) {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("specify only one metadata selector: --%s and --%s conflict", selected, name)
		}
		if name == metadataPersonalFlag {
			if !cmd.Bool(name) {
				return "", fmt.Errorf("--personal must be true when supplied")
			}
		} else if strings.TrimSpace(cmd.String(name)) == "" {
			return "", fmt.Errorf("--%s cannot be empty", name)
		}
		selected = name
	}
	return selected, nil
}

// metadataAPI resolves the shared connection settings and authentication.
func (line *commandLine) metadataAPI(ctx context.Context, cmd *cli.Command) (*confluence.API, error) {
	if err := line.prepare(cmd); err != nil {
		return nil, err
	}
	creds, err := GetCredentials(ctx, cmd.String("username"), cmd.String("password"), cmd.String("password-command"), "", cmd.String("base-url"), false)
	if err != nil {
		return nil, err
	}
	return confluence.NewAPI(creds.BaseURL, confluence.Options{Username: creds.Username, Password: creds.Password, InsecureSkipVerify: cmd.Bool("insecure-skip-tls-verify")}), nil
}

// runMetadataLabels writes every matching label as one JSON array.
func (line *commandLine) runMetadataLabels(ctx context.Context, cmd *cli.Command) error {
	selector, err := metadataSelector(cmd)
	if err != nil {
		return err
	}
	api, err := line.metadataAPI(ctx, cmd)
	if err != nil {
		return err
	}
	query := confluence.LabelQuery{}
	switch selector {
	case metadataSpaceFlag:
		query.Space = cmd.String(selector)
	case metadataSpaceIDFlag:
		query.Collection = confluence.LabelCollectionSpaces
	case metadataPageIDFlag:
		query.Collection = confluence.LabelCollectionPages
	case metadataBlogpostIDFlag:
		query.Collection = confluence.LabelCollectionBlogposts
	case metadataAttachmentIDFlag:
		query.Collection = confluence.LabelCollectionAttachments
	case metadataCustomContentIDFlag:
		query.Collection = confluence.LabelCollectionCustomContent
	}
	if query.Collection != "" {
		query.ID = cmd.String(selector)
	}
	labels, err := api.ListLabels(ctx, query)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(cmd.Root().Writer).Encode(labels); err != nil {
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
	api, err := line.metadataAPI(ctx, cmd)
	if err != nil {
		return err
	}
	var statuses []confluence.ContentStatus
	switch selector {
	case metadataSpaceFlag:
		statuses, err = api.GetSpaceContentStatuses(ctx, cmd.String(selector))
	case metadataPersonalFlag:
		statuses, err = api.GetPersonalContentStatuses(ctx)
	case metadataPageIDFlag:
		statuses, err = api.GetPageContentStatus(ctx, cmd.String(selector))
	}
	if err != nil {
		return err
	}
	if err := json.NewEncoder(cmd.Root().Writer).Encode(statuses); err != nil {
		return fmt.Errorf("unable to write statuses: %w", err)
	}
	return nil
}
