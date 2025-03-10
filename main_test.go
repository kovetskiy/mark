package main

import (
	"flag"
	"testing"

	"github.com/kovetskiy/mark/util"
	"github.com/reconquest/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
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
			set := flag.NewFlagSet("test", flag.ContinueOnError)
			set.String("log-level", tt.args.lvl, "")
			cliCtx := cli.NewContext(nil, set, nil)

			err := util.SetLogLevel(cliCtx)
			if tt.expectedErr != "" {
				assert.EqualError(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, log.GetLevel())
			}
		})
	}
}
