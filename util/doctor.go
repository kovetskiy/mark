package util

import (
	"context"
	"errors"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/kovetskiy/mark/v16/doctor"
	"github.com/urfave/cli/v3"
)

// DoctorCommand is the name of the command that checks mark's connection.
const DoctorCommand = "doctor"

func (line *commandLine) doctorCommand() *cli.Command {
	return &cli.Command{
		Name:  DoctorCommand,
		Usage: "check that mark can reach Confluence, log in, and publish to a space",
		Description: "Log in with the global flags, say which platform answered, and for each space " +
			"named as an argument, or by --space, check that it exists, has a home page, and lets the " +
			"account create pages and upload attachments. Changes nothing. Fails if any check fails.",
		ArgsUsage: "[space]...",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "space",
				Value:   "",
				Usage:   "the space key to check when no space is named as an argument.",
				Sources: cli.NewValueSourceChain(cli.EnvVar("MARK_SPACE"), line.config.value("space")),
			},
		},
		Action: line.runDoctor,
	}
}

func (line *commandLine) runDoctor(ctx context.Context, cmd *cli.Command) error {
	if err := line.prepare(cmd); err != nil {
		return err
	}

	baseURL := cmd.String("base-url")
	if baseURL == "" {
		return errors.New("confluence base URL should be specified using --base-url")
	}

	creds, err := GetCredentials(
		ctx,
		cmd.String("username"),
		cmd.String("password"),
		cmd.String("password-command"),
		"",
		baseURL,
		false,
	)
	if err != nil {
		return err
	}

	spaces := cmd.Args().Slice()
	if len(spaces) == 0 && cmd.String("space") != "" {
		spaces = []string{cmd.String("space")}
	}

	logFlags(cmd)

	api := confluence.NewAPI(creds.BaseURL, creds.Username, creds.Password, cmd.Bool("insecure-skip-tls-verify"))
	return doctor.Run(api, creds.BaseURL, creds.Username, spaces, cmd.Root().Writer)
}
