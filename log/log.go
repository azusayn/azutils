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
	logger *PrettyLogger
	once   sync.Once
)

func init() {
	once.Do(func() {
		logger = NewPrettyLogger(os.Stdout, LevelDebug)
	})
}

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type PrettyLogger struct {
	out   io.Writer
	level Level
	mu    sync.Mutex
}

func NewPrettyLogger(out io.Writer, level Level) *PrettyLogger {
	return &PrettyLogger{
		out:   out,
		level: level,
	}
}

func (l *PrettyLogger) Enabled(level Level) bool {
	return level >= l.level
}

func (l *PrettyLogger) log(level Level, msg string, args ...any) {
	if !l.Enabled(level) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var levelStr string
	switch level {
	case LevelDebug:
		levelStr = color.HiCyanString("[DEBUG]")
	case LevelInfo:
		levelStr = color.HiGreenString("[INFO] ")
	case LevelWarn:
		levelStr = color.HiYellowString("[WARN] ")
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
