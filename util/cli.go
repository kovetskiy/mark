package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	mark "github.com/kovetskiy/mark/v16"
	"github.com/urfave/cli/v3"
)

func RunMark(ctx context.Context, cmd *cli.Command) error {
	if err := SetLogLevel(cmd); err != nil {
		return err
	}

	zerolog.TimeFieldFormat = "2006-01-02 15:04:05.000"

	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "2006-01-02 15:04:05.000",
		FormatLevel: func(i interface{}) string {
			var l string
			if ll, ok := i.(string); ok {
				switch ll {
				case "trace":
					l = "TRACE"
				case "debug":
					l = "DEBUG"
				case "info":
					l = "INFO"
				case "warn":
					l = "WARNING"
				case "error":
					l = "ERROR"
				case "fatal":
					l = "FATAL"
				case "panic":
					l = "PANIC"
				default:
					l = strings.ToUpper(ll)
				}
			} else {
				l = strings.ToUpper(fmt.Sprintf("%s", i))
			}
			return l
		},
		FormatFieldName: func(i interface{}) string {
			return ""
		},
		FormatFieldValue: func(i interface{}) string {
			return fmt.Sprintf("%s", i)
		},
		FormatErrFieldName: func(i interface{}) string {
			return ""
		},
		FormatErrFieldValue: func(i interface{}) string {
			return fmt.Sprintf("%s", i)
		},
	}
	if cmd.String("color") == "never" {
		output.NoColor = true
	}
	log.Logger = zerolog.New(output).With().Timestamp().Logger()

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

	log.Debug().Msg("config:")
	for _, f := range cmd.Flags {
		flag := f.Names()
		if flag[0] == "password" {
			log.Debug().Msgf("%20s: %v", flag[0], "******")
		} else {
			log.Debug().Msgf("%20s: %v", flag[0], cmd.Value(flag[0]))
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
		log.Fatal().Err(err).Send()
	}
	return filepath.Join(fp, "mark.toml")
}

func SetLogLevel(cmd *cli.Command) error {
	logLevel := cmd.String("log-level")
	switch strings.ToUpper(logLevel) {
	case "TRACE":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "DEBUG":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "INFO":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "WARNING":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "ERROR":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "FATAL":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	default:
		return fmt.Errorf("unknown log level: %s", logLevel)
	}

	return nil
}
