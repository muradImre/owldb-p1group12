// Structured logger that allows you to set the log level
// and colorize the output.  You may choose to use this or
// not and you may modify it in any way.
//
// To use this, set up your logger as follows:
//
//	logOpts := &logger.PrettyHandlerOptions{
//	    Level:    slog.LevelDebug, // log level you want
//	    Colorize: true,            // true or false
//	}
//
// handler := logger.NewPrettyHandler(os.Stdout, logOpts)
// logger := slog.New(handler)
// slog.SetDefault(logger)
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

/*
 * Works on Linux, Windows 11, and MacOS 13
 *  (Does not work in Windows 10)
 */

// Escape codes for colorizing output.
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	Gray   = "\033[37m"
	White  = "\033[97m"
)

// PrettyHandlerOptions provides options for the PrettyHandler
type PrettyHandlerOptions struct {
	AddSource   bool
	Level       slog.Leveler
	ReplaceAttr func([]string, slog.Attr) slog.Attr
	Colorize    bool
}

// PrettyHandler is an slog.Handler that pretty-prints log records using color.
// Based upon:
//
//	https://github.com/golang/example/blob/master/slog-handler-guide/README.md
type PrettyHandler struct {
	pool *sync.Pool
	opts *PrettyHandlerOptions
	goas []groupOrAttrs
	mu   *sync.Mutex
	out  io.Writer
}

// groupOrAttrs holds either a group name or a list of slog.Attrs.
type groupOrAttrs struct {
	group string      // group name if non-empty
	attrs []slog.Attr // attrs if non-empty
}

// needsQuoting reports whether the string s needs quoting.
func needsQuoting(s string) bool {
	if len(s) == 0 {
		return true
	}
	for _, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune("!\"#$%&'()*+,-/:;<=>?@[\\]^`{|}~", r) {
			return true
		}
	}
	return false
}

// initPool initializes the pool of buffers for the PrettyHandler.
func (h *PrettyHandler) initPool() {
	h.pool = &sync.Pool{
		New: func() any {
			b := make([]byte, 0, 1024)
			return &b
		},
	}
}

// allocBuf returns a buffer from the pool.
func (h *PrettyHandler) allocBuf() *[]byte {
	if h.pool == nil {
		b := make([]byte, 0, 1024)
		return &b
	}

	return h.pool.Get().(*[]byte)
}

// freeBuf returns a buffer to the pool.
func (h *PrettyHandler) freeBuf(b *[]byte) {
	if h.pool == nil {
		return
	}

	// To reduce peak allocation, return only smaller buffers to the pool.
	const maxBufferSize = 16 << 10
	if cap(*b) <= maxBufferSize {
		*b = (*b)[:0]
		h.pool.Put(b)
	}
}

// qappendf appends a quoted string to the given buffer.
func qappendf(buf []byte, groups []string, val string) []byte {
	pstr := strings.Join(groups, ".")
	if len(groups) > 0 {
		pstr = fmt.Sprintf("%s.", pstr)
	}
	str := fmt.Sprintf("%s%s", pstr, val)
	if needsQuoting(str) {
		buf = fmt.Appendf(buf, "%q", str)
	} else {
		buf = fmt.Appendf(buf, "%s", str)
	}
	return buf
}

// appendAttr appends an attribute to the given buffer.
func (h *PrettyHandler) appendAttr(buf []byte, a slog.Attr, groups []string) []byte {
	// Resolve the Attr's value before doing anything else.
	a.Value = a.Value.Resolve()

	// Call ReplaceAttr, if provided.
	if rep := h.opts.ReplaceAttr; rep != nil && a.Value.Kind() != slog.KindGroup {
		gs := slices.Clone(groups)
		a = rep(gs, a)
		a.Value = a.Value.Resolve()
	}

	// Ignore empty Attrs.
	if a.Equal(slog.Attr{}) {
		return buf
	}

	switch a.Value.Kind() {
	case slog.KindString:
		// Prefix the attribute with a space
		buf = append(buf, " "...)

		// Output key=value
		buf = qappendf(buf, groups, a.Key)
		buf = append(buf, "="...)
		buf = qappendf(buf, []string{}, a.Value.String())
	case slog.KindTime:
		// Prefix the attribute with a space
		buf = append(buf, " "...)

		// Output key=value
		buf = qappendf(buf, groups, a.Key)
		buf = append(buf, "="...)

		// Write times in a standard way, without the monotonic time.
		buf = fmt.Appendf(buf, "%s", a.Value.Time().Format(time.RFC3339Nano))
	case slog.KindGroup:
		attrs := a.Value.Group()
		// Ignore empty groups.
		if len(attrs) == 0 {
			return buf
		}
		// If the key is non-empty, add group to groups.
		if a.Key != "" {
			groups = append(groups, a.Key)
		}

		// Output group's attributes.
		for _, ga := range attrs {
			buf = h.appendAttr(buf, ga, groups)
		}
	default:
		// Prefix the attribute with a space
		buf = append(buf, " "...)

		// Output key=value
		buf = qappendf(buf, groups, a.Key)
		buf = append(buf, "="...)
		buf = fmt.Appendf(buf, "%s", a.Value)
	}

	return buf
}

// Enabled returns true if the logging level is enabled.
func (h *PrettyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle writes the record to the output.
func (h *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	// Allocate a buffer for the record from the pool.
	bufp := h.allocBuf()
	buf := *bufp
	defer func() {
		*bufp = buf
		h.freeBuf(bufp)
	}()

	groups := make([]string, 0)

	if h.opts.Colorize {
		// Set the color based on the level of the record.
		if r.Level == slog.LevelError {
			buf = append(buf, Red...)
		} else if r.Level == slog.LevelWarn {
			buf = append(buf, Yellow...)
		} else if r.Level == slog.LevelDebug {
			buf = append(buf, Cyan...)
		} else {
			// Default to white.
			buf = append(buf, White...)
		}
	}

	var a slog.Attr

	// Time
	if !r.Time.IsZero() {
		if rep := h.opts.ReplaceAttr; rep != nil {
			// Call ReplaceAttr, if provided.
			gs := slices.Clone(groups)
			a = rep(gs, slog.Time(slog.TimeKey, r.Time))
			a.Value = a.Value.Resolve()
			if !a.Equal(slog.Attr{}) {
				if a.Key == slog.TimeKey {
					buf = qappendf(buf, groups, a.Value.String())
					buf = append(buf, " "...)
				} else {
					buf = h.appendAttr(buf, a, groups)
				}
			}
		} else {
			buf = fmt.Appendf(buf, "%s ", r.Time.Format("2006/01/02 15:04:05"))
		}
	}

	// Level
	if rep := h.opts.ReplaceAttr; rep != nil {
		// Call ReplaceAttr, if provided.
		gs := slices.Clone(groups)
		a = rep(gs, slog.Any(slog.LevelKey, r.Level))
		a.Value = a.Value.Resolve()
		if !a.Equal(slog.Attr{}) {
			if a.Key == slog.LevelKey {
				buf = qappendf(buf, groups, a.Value.String())
				buf = append(buf, " "...)
			} else {
				buf = h.appendAttr(buf, a, groups)
			}
		}
	} else {
		buf = fmt.Appendf(buf, "%s ", r.Level)
	}

	// Message
	if rep := h.opts.ReplaceAttr; rep != nil {
		// Call ReplaceAttr, if provided.
		gs := slices.Clone(groups)
		a = rep(gs, slog.Any(slog.MessageKey, r.Message))
		a.Value = a.Value.Resolve()
		if !a.Equal(slog.Attr{}) {
			if a.Key == slog.MessageKey {
				buf = fmt.Appendf(buf, "%s", a.Value.String())
			} else {
				buf = h.appendAttr(buf, a, groups)
			}
		}
	} else {
		buf = fmt.Appendf(buf, "%s", r.Message)
	}

	// Source
	if h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		buf = h.appendAttr(buf, slog.String(slog.SourceKey, fmt.Sprintf("%s:%d", f.File, f.Line)), groups)
	}

	// Handle state from WithGroup and WithAttrs.

	goas := h.goas
	if r.NumAttrs() == 0 {
		// If the record has no Attrs, remove groups at the end of the list; they are empty.
		for len(goas) > 0 && goas[len(goas)-1].group != "" {
			goas = goas[:len(goas)-1]
		}
	}
	for _, goa := range goas {
		if goa.group != "" {
			groups = append(groups, goa.group)
		} else {
			for _, a := range goa.attrs {
				buf = h.appendAttr(buf, a, groups)
			}
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		buf = h.appendAttr(buf, a, groups)
		return true
	})

	// Newline and reset color.
	if h.opts.Colorize {
		buf = append(buf, Reset...)
	}
	buf = append(buf, "\n"...)

	// Write to the output.
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(buf)
	return err
}

// withGroupOrAttrs returns a new PrettyHandler with the given group or attributes added.
func (h *PrettyHandler) withGroupOrAttrs(goa groupOrAttrs) *PrettyHandler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h2.goas)-1] = goa
	return &h2
}

// WithGroup returns a new PrettyHandler with the group name added.
func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// WithAttrs returns a new PrettyHandler with the attributes added.
func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

func NewPrettyHandler(w io.Writer, opts *PrettyHandlerOptions) *PrettyHandler {
	if opts == nil {
		opts = &PrettyHandlerOptions{
			Level: slog.LevelInfo,
		}
	}
	h := &PrettyHandler{nil, opts, nil, &sync.Mutex{}, w}
	h.initPool()

	return h
}
