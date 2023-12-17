package logging

import (
	runtime "github.com/banzaicloud/logrus-runtime-formatter"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/sirupsen/logrus"
)

func NewLogger(logLevel string) *logrus.Logger {
	logger := logrus.New()

	switch types.LogLevel(logLevel) {
	case types.LogLevelDebug:
		logger.Level = logrus.DebugLevel
	case types.LogLevelTrace:
		logger.Level = logrus.TraceLevel
	default:
		logger.Level = logrus.InfoLevel
	}

	runtimeFormatter := &runtime.Formatter{
		ChildFormatter: &logrus.JSONFormatter{},
		File:           true,
		Line:           true,
		BaseNameOnly:   true,
	}

	logger.SetFormatter(runtimeFormatter)

	return logger
}
