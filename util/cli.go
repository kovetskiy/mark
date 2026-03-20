package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kovetskiy/lorg"
	mark "github.com/kovetskiy/mark/v16"
	"github.com/reconquest/pkg/log"
	"github.com/urfave/cli/v3"
)

func RunMark(ctx context.Context, cmd *cli.Command) error {
	if err := SetLogLevel(cmd); err != nil {
		return err
	}

	if cmd.String("color") == "never" {
		log.GetLogger().SetFormat(
			lorg.NewFormat(
				`${time:2006-01-02 15:04:05.000} ${level:%s:left:true} ${prefix}%s`,
			),
		)
		log.GetLogger().SetOutput(os.Stderr)
	}

	creds, err := GetCredentials(
		cmd.String("username"),
		cmd.String("password"),
		cmd.String("target-url"),
		cmd.String("base-url"),
		cmd.Bool("compile-only"),
	)
	if err != nil {
		return err
	}

	log.Debug("config:")
	for _, f := range cmd.Flags {
		flag := f.Names()
		if flag[0] == "password" {
			log.Debugf(nil, "%20s: %v", flag[0], "******")
		} else {
			log.Debugf(nil, "%20s: %v", flag[0], cmd.Value(flag[0]))
		}
	}

	parents := strings.Split(cmd.String("parents"), cmd.String("parents-delimiter"))

	config := mark.Config{
		BaseURL:               creds.BaseURL,
		Username:              creds.Username,
		Password:              creds.Password,
		PageID:                creds.PageID,
		InsecureSkipTLSVerify: cmd.Bool("insecure-skip-tls-verify"),

		Files: cmd.String("files"),

		CompileOnly:     cmd.Bool("compile-only"),
		DryRun:          cmd.Bool("dry-run"),
		ContinueOnError: cmd.Bool("continue-on-error"),
		CI:              cmd.Bool("ci"),

		Space:                    cmd.String("space"),
		Parents:                  parents,
		TitleFromH1:              cmd.Bool("title-from-h1"),
		TitleFromFilename:        cmd.Bool("title-from-filename"),
		TitleAppendGeneratedHash: cmd.Bool("title-append-generated-hash"),
		ContentAppearance:        cmd.String("content-appearance"),

		MinorEdit:      cmd.Bool("minor-edit"),
		VersionMessage: cmd.String("version-message"),
		EditLock:       cmd.Bool("edit-lock"),
		ChangesOnly:    cmd.Bool("changes-only"),

		DropH1:          cmd.Bool("drop-h1"),
		StripLinebreaks: cmd.Bool("strip-linebreaks"),
		MermaidScale:    cmd.Float("mermaid-scale"),
		D2Scale:         cmd.Float("d2-scale"),
		Features:        cmd.StringSlice("features"),
		ImageAlign:      cmd.String("image-align"),
		IncludePath:     cmd.String("include-path"),

		Output: os.Stdout,
	}

	return mark.Run(config)
}

func ConfigFilePath() string {
	fp, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(fp, "mark.toml")
}

func SetLogLevel(cmd *cli.Command) error {
	logLevel := cmd.String("log-level")
	switch strings.ToUpper(logLevel) {
	case lorg.LevelTrace.String():
		log.SetLevel(lorg.LevelTrace)
	case lorg.LevelDebug.String():
		log.SetLevel(lorg.LevelDebug)
	case lorg.LevelInfo.String():
		log.SetLevel(lorg.LevelInfo)
	case lorg.LevelWarning.String():
		log.SetLevel(lorg.LevelWarning)
	case lorg.LevelError.String():
		log.SetLevel(lorg.LevelError)
	case lorg.LevelFatal.String():
		log.SetLevel(lorg.LevelFatal)
	default:
		return fmt.Errorf("unknown log level: %s", logLevel)
	}

	return nil
}
