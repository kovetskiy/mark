package main

import (
	"testing"

	"github.com/kovetskiy/mark/util"
	"github.com/reconquest/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v3"
)

func Test_setLogLevel(t *testing.T) {
	type args struct {
		lvl string
	}
	tests := map[string]struct {
		args        args
		want        log.Level
		expectedErr string
	}{
		"invalid": {args: args{lvl: "INVALID"}, want: log.LevelInfo, expectedErr: "unknown log level: INVALID"},
		"empty":   {args: args{lvl: ""}, want: log.LevelInfo, expectedErr: "unknown log level: "},
		"info":    {args: args{lvl: log.LevelInfo.String()}, want: log.LevelInfo},
		"debug":   {args: args{lvl: log.LevelDebug.String()}, want: log.LevelDebug},
		"trace":   {args: args{lvl: log.LevelTrace.String()}, want: log.LevelTrace},
		"warning": {args: args{lvl: log.LevelWarning.String()}, want: log.LevelWarning},
		"error":   {args: args{lvl: log.LevelError.String()}, want: log.LevelError},
		"fatal":   {args: args{lvl: log.LevelFatal.String()}, want: log.LevelFatal},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cmd := &cli.Command{
				Name: "test",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "log-level",
						Value: tt.args.lvl,
						Usage: "set the log level. Possible values: TRACE, DEBUG, INFO, WARNING, ERROR, FATAL.",
					},
				},
			}
			err := util.SetLogLevel(cmd)
			if tt.expectedErr != "" {
				assert.EqualError(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, log.GetLevel())
			}
		})
	}
}
