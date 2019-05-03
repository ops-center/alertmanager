package logger

import (
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

var (
	Logger = log.NewNopLogger()
)

func InitLogger() {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = level.NewFilter(logger, level.AllowAll())
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	Logger = log.With(logger, "caller", log.Caller(3))
}

func WithUserID(userID string, l log.Logger) log.Logger {
	return log.With(l, "user_id", userID)
}
