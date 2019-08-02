package log

import (
	"github.com/kovetskiy/lorg"
	"github.com/reconquest/cog"
)

var (
	log *cog.Logger
)

func Init(debug, trace bool) {
	stderr := lorg.NewLog()
	stderr.SetIndentLines(true)
	stderr.SetFormat(
		lorg.NewFormat("${time} ${level:[%s]:right:short} ${prefix}%s"),
	)

	log = cog.NewLogger(stderr)

	if debug {
		log.SetLevel(lorg.LevelDebug)
	}

	if trace {
		log.SetLevel(lorg.LevelTrace)
	}
}

func Fatalf(
	reason interface{},
	message string,
	args ...interface{},
) {
	log.Fatalf(reason, message, args...)
}

func Errorf(
	reason interface{},
	message string,
	args ...interface{},
) {
	log.Errorf(reason, message, args...)
}

func Warningf(
	reason interface{},
	message string,
	args ...interface{},
) {
	log.Warningf(reason, message, args...)
}

func Infof(
	context interface{},
	message string,
	args ...interface{},
) {
	log.Infof(context, message, args...)
}

func Debugf(
	context interface{},
	message string,
	args ...interface{},
) {
	log.Debugf(context, message, args...)
}

func Tracef(
	context interface{},
	message string,
	args ...interface{},
) {
	log.Tracef(context, message, args...)
}

func Fatal(values ...interface{}) {
	log.Fatal(values...)
}

func Error(values ...interface{}) {
	log.Error(values...)
}

func Warning(values ...interface{}) {
	log.Warning(values...)
}

func Info(values ...interface{}) {
	log.Info(values...)
}

func Debug(values ...interface{}) {
	log.Debug(values...)
}

func Trace(values ...interface{}) {
	log.Trace(values...)
}
