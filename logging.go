package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

const (
	defaultLogRetentionDays = 7
	logDateLayout           = "2006-01-02"
	logFilePrefix           = "uddns"
	logDirMode              = 0700
	logFileMode             = 0600
)

type logConfigValue struct {
	value  string
	source string
}

type logConfig struct {
	level         logConfigValue
	dir           logConfigValue
	retentionDays logConfigValue
}

type logConfigReader interface {
	GetString(key string) string
	IsSet(key string) bool
}

var activeLogFile *calendarRotatingWriter

func configureLogger() {
	configureLoggerFromConfig(nil)
}

func configureLoggerFromConfig(v logConfigReader) {
	config := resolveLogConfig(v)
	level, levelOK := parseLogLevel(config.level.value)
	handlers := []slog.Handler{
		tint.NewTextHandler(os.Stdout, &tint.Options{
			NoColor:    !isatty.IsTerminal(os.Stdout.Fd()),
			Level:      level,
			TimeFormat: time.DateTime,
		}),
	}

	logDir := strings.TrimSpace(config.dir.value)
	retentionDays, retentionOK := parseLogRetentionDays(config.retentionDays.value)
	var fileLogErr error
	var fileLogWriter *calendarRotatingWriter

	if logDir != "" {
		writer, err := newCalendarRotatingWriter(logDir, logFilePrefix, retentionDays)
		if err != nil {
			fileLogErr = err
		} else {
			fileLogWriter = writer
			handlers = append(handlers, slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level}))
		}
	}

	slog.SetDefault(slog.New(fanoutHandler(handlers)).With(logProcessAttrs()...))
	closeActiveLogFile(fileLogWriter)

	if !levelOK {
		slog.Warn("invalid log level, using info", "value", config.level.value, "source", config.level.source)
	}
	if !retentionOK {
		slog.Warn("invalid log retention days, using default", "value", config.retentionDays.value, "source", config.retentionDays.source, "default", defaultLogRetentionDays)
	}
	if fileLogErr != nil {
		slog.Warn("failed to enable file logging", "dir", logDir, "source", config.dir.source, "error", fileLogErr)
	} else if logDir != "" {
		slog.Info("file logging enabled", "dir", logDir, "retention_days", retentionDays, "source", config.dir.source)
	}
}

func logProcessAttrs() []any {
	return []any{
		"version", version,
		"pid", os.Getpid(),
	}
}

func resolveLogConfig(v logConfigReader) logConfig {
	config := logConfig{
		level:         logConfigValue{source: "default"},
		dir:           logConfigValue{source: "default"},
		retentionDays: logConfigValue{source: "default"},
	}

	if v != nil {
		if v.IsSet("logging.level") {
			config.level = logConfigValue{value: v.GetString("logging.level"), source: "config:logging.level"}
		}
		if v.IsSet("logging.dir") {
			config.dir = logConfigValue{value: v.GetString("logging.dir"), source: "config:logging.dir"}
		}
		if v.IsSet("logging.retention_days") {
			config.retentionDays = logConfigValue{value: v.GetString("logging.retention_days"), source: "config:logging.retention_days"}
		}
	}

	if value := strings.TrimSpace(os.Getenv("UDDNS_LOG_LEVEL")); value != "" {
		config.level = logConfigValue{value: value, source: "env:UDDNS_LOG_LEVEL"}
	}
	if value := strings.TrimSpace(os.Getenv("UDDNS_LOG_DIR")); value != "" {
		config.dir = logConfigValue{value: value, source: "env:UDDNS_LOG_DIR"}
	}
	if value := strings.TrimSpace(os.Getenv("UDDNS_LOG_RETENTION_DAYS")); value != "" {
		config.retentionDays = logConfigValue{value: value, source: "env:UDDNS_LOG_RETENTION_DAYS"}
	}

	return config
}

func closeActiveLogFile(next *calendarRotatingWriter) {
	if activeLogFile != nil && activeLogFile != next {
		_ = activeLogFile.Close()
	}
	activeLogFile = next
}

func parseLogLevel(value string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, true
	case "debug":
		return slog.LevelDebug, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

func parseLogRetentionDays(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultLogRetentionDays, true
	}

	days, err := strconv.Atoi(value)
	if err != nil || days < 1 {
		return defaultLogRetentionDays, false
	}

	return days, true
}

type fanoutHandler []slog.Handler

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, handler := range h {
		if handler.Enabled(ctx, record.Level) {
			err = errors.Join(err, handler.Handle(ctx, record.Clone()))
		}
	}
	return err
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make(fanoutHandler, len(h))
	for i, handler := range h {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return handlers
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make(fanoutHandler, len(h))
	for i, handler := range h {
		handlers[i] = handler.WithGroup(name)
	}
	return handlers
}

type calendarRotatingWriter struct {
	mu            sync.Mutex
	dir           string
	prefix        string
	retentionDays int
	now           func() time.Time
	currentDate   string
	file          *os.File
	root          *os.Root
}

func newCalendarRotatingWriter(dir, prefix string, retentionDays int) (*calendarRotatingWriter, error) {
	return newCalendarRotatingWriterWithClock(dir, prefix, retentionDays, time.Now)
}

func newCalendarRotatingWriterWithClock(dir, prefix string, retentionDays int, now func() time.Time) (*calendarRotatingWriter, error) {
	if err := os.MkdirAll(dir, logDirMode); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to use symbolic link as log directory: %s", dir)
	}
	if err := os.Chmod(dir, logDirMode); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}

	writer := &calendarRotatingWriter{
		dir:           dir,
		prefix:        prefix,
		retentionDays: retentionDays,
		now:           now,
		root:          root,
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writer.rotateLocked(now()); err != nil {
		_ = root.Close()
		return nil, err
	}
	return writer, nil
}

func (w *calendarRotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.rotateLocked(w.now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *calendarRotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var err error
	if w.file != nil {
		err = w.file.Close()
		w.file = nil
	}
	if w.root != nil {
		err = errors.Join(err, w.root.Close())
		w.root = nil
	}
	return err
}

func (w *calendarRotatingWriter) rotateLocked(now time.Time) error {
	date := now.Format(logDateLayout)
	if w.file != nil && w.currentDate == date {
		return nil
	}

	if w.file != nil {
		_ = w.file.Close()
	}

	name := w.logName(date)
	info, err := w.root.Lstat(name)
	if err == nil && !info.Mode().IsRegular() {
		w.file = nil
		w.currentDate = ""
		return fmt.Errorf("refusing to open non-regular log file: %s", w.logPath(date))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		w.file = nil
		w.currentDate = ""
		return err
	}

	file, err := w.root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFileMode)
	if err != nil {
		w.file = nil
		w.currentDate = ""
		return err
	}

	w.file = file
	w.currentDate = date
	w.cleanupOldLogs(now)
	return nil
}

func (w *calendarRotatingWriter) logPath(date string) string {
	return filepath.Join(w.dir, w.logName(date))
}

func (w *calendarRotatingWriter) logName(date string) string {
	return w.prefix + "-" + date + ".log"
}

func (w *calendarRotatingWriter) cleanupOldLogs(now time.Time) {
	if w.retentionDays < 1 {
		return
	}

	cutoff := dateOnly(now).AddDate(0, 0, -w.retentionDays+1)
	entries, err := fs.ReadDir(w.root.FS(), ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "uddns: failed to read log directory for cleanup: dir=%s error=%v\n", w.dir, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		logDate, ok := parseRotatedLogDate(entry.Name(), w.prefix)
		if !ok || !logDate.Before(cutoff) {
			continue
		}

		path := filepath.Join(w.dir, entry.Name())
		if err := w.root.Remove(entry.Name()); err != nil {
			fmt.Fprintf(os.Stderr, "uddns: failed to remove expired log file: path=%s error=%v\n", path, err)
		}
	}
}

func parseRotatedLogDate(name, prefix string) (time.Time, bool) {
	dateText, ok := strings.CutPrefix(name, prefix+"-")
	if !ok {
		return time.Time{}, false
	}
	dateText, ok = strings.CutSuffix(dateText, ".log")
	if !ok {
		return time.Time{}, false
	}

	date, err := time.ParseInLocation(logDateLayout, dateText, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return date, true
}

func dateOnly(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}
