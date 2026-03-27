package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
)

var (
	logger *prettyLogger
	once   sync.Once
)

func init() {
	once.Do(func() {
		logger = newPrettyLogger(os.Stdout, LevelDebug)
	})
}

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type prettyLogger struct {
	out   io.Writer
	level Level
	mu    sync.Mutex
}

func newPrettyLogger(out io.Writer, level Level) *prettyLogger {
	return &prettyLogger{
		out:   out,
		level: level,
	}
}

func (l *prettyLogger) log(level Level, msg string, args ...any) {
	if l.level > level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var levelStr string
	switch level {
	case LevelDebug:
		levelStr = color.HiCyanString("[DEBUG]")
	case LevelInfo:
		levelStr = color.HiGreenString("[INFO]")
	case LevelWarn:
		levelStr = color.HiYellowString("[WARN]")
	case LevelError:
		levelStr = color.HiRedString("[ERROR]")
	default:
		levelStr = "[UNKNOWN]"
	}
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	time := time.Now().Format("2006-01-02 15:04:05 -07:00")
	line := fmt.Sprintf("[%s] %s %s\n", time, levelStr, msg)
	_, _ = l.out.Write([]byte(line))
}

func SetLevel(level Level) {
	logger.level = level
}

func Debug(msg string, args ...any) {
	logger.log(LevelDebug, msg, args...)
}

func Info(msg string, args ...any) {
	logger.log(LevelInfo, msg, args...)
}

func Warn(msg string, args ...any) {
	logger.log(LevelWarn, msg, args...)
}

func Error(msg string, args ...any) {
	logger.log(LevelError, msg, args...)
}
