package log

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/illikainen/git-fence/internal/textutil"
)

type HandlerOptions struct {
	Name      string
	AddSource bool
	Level     slog.Leveler
	NoPrefix  bool
}

type SanitizedHandler struct {
	*HandlerOptions

	writer io.Writer
	attrs  []slog.Attr
}

func NewSanitizedHandler(w io.Writer, opts *HandlerOptions) *SanitizedHandler {
	return &SanitizedHandler{
		HandlerOptions: opts,
		writer:         w,
	}
}

func (h *SanitizedHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.Level.Level() <= level
}

// revive:disable-next-line
func (h *SanitizedHandler) Handle(_ context.Context, record slog.Record) error { //nolint
	prefix := ""
	if !h.NoPrefix {
		if h.Name != "" {
			prefix += "\033[35m" + h.Name + "\033[0m: "
		}

		switch record.Level {
		case slog.LevelDebug:
			prefix += "\033[36mdebug\033[0m: "
		case slog.LevelInfo:
			prefix += "\033[32minfo\033[0m: "
		case slog.LevelWarn:
			prefix += "\033[33mwarn\033[0m: "
		case slog.LevelError:
			prefix += "\033[31merror\033[0m: "
		default:
			prefix += "\033[31minvalid\033[0m: "
		}
	}

	var attrs []string
	for _, attr := range h.attrs {
		attrs = append(attrs, attr.String())
	}

	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, strings.Trim(attr.String(), " \t\r\n"))
		return true
	})

	if h.AddSource {
		frames := runtime.CallersFrames([]uintptr{record.PC})
		frame, _ := frames.Next()
		attrs = append(attrs, "func="+frame.Function)
	}

	msg := prefix + textutil.Sanitize(record.Message)
	if len(attrs) > 0 {
		msg += " | " + textutil.Sanitize(strings.Join(attrs, ", "))
	}
	msg += "\n"

	if n, err := h.writer.Write([]byte(msg)); err != nil || n != len(msg) {
		return fmt.Errorf("bad write (%d): %w", n, err)
	}

	return nil
}

func (h *SanitizedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SanitizedHandler{
		HandlerOptions: h.HandlerOptions,
		writer:         h.writer,
		attrs:          append(attrs, h.attrs...),
	}
}

func (h *SanitizedHandler) WithGroup(_ string) slog.Handler {
	panic("not implemented")
}

func ParseLevel(s string) slog.Level {
	level := new(slog.Level)
	if err := level.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		*level = slog.LevelDebug
	}
	return *level
}

func LogReader(r io.Reader) { // nosemgrep: go.lang.best-practice.hidden-goroutine.hidden-goroutine
	go func() {
		scan := bufio.NewScanner(r)
		scan.Split(bufio.ScanLines)

		for scan.Scan() {
			var obj map[string]any
			data := textutil.Sanitize(scan.Bytes())

			if err := json.Unmarshal(data, &obj); err != nil {
				slog.Error(string(data), "remote", true, "err", err)
			} else {
				lvl, ok := obj["level"].(string)
				if !ok {
					lvl = "ERROR"
				}

				level := new(slog.Level)
				if err := level.UnmarshalText([]byte(lvl)); err != nil {
					slog.Error(err.Error())
					*level = slog.LevelError
				}

				attrs := []any{"remote", true}
				for key, value := range obj {
					if !slices.Contains([]string{"time", "level", "msg"}, key) {
						attrs = append(attrs, key, value)
					}
				}

				msg, ok := obj["msg"].(string)
				if !ok {
					msg = "N/A"
				}
				slog.Log(context.Background(), *level, msg, attrs...)
			}
		}

		if err := scan.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
			slog.Error(err.Error())
		}
	}()
}
