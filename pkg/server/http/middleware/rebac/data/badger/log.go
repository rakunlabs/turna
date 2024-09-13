package badger

import (
	"fmt"
	"log/slog"
)

type Logger struct {
	logger *slog.Logger
}

func NewLogger() *Logger {
	return &Logger{
		logger: slog.Default().With(slog.String("db", "badger")),
	}
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l *Logger) Warningf(format string, v ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, v...))
}

func (l *Logger) Infof(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, v...))
}
