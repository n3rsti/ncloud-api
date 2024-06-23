package logger

import (
	"fmt"
	"github.com/felixge/httpsnoop"
	"io"
	"net/http"
)

const (
	green   = "\033[97;42m"
	white   = "\033[90;47m"
	yellow  = "\033[90;43m"
	red     = "\033[97;41m"
	blue    = "\033[97;44m"
	magenta = "\033[97;45m"
	cyan    = "\033[97;46m"
	reset   = "\033[0m"
)

// Logger struct is used for logging messages, requests, etc.
type Logger struct {
	output io.Writer
}

// HTTPReqInfo holds the data of HTTP request.
// Used for logging requests
type HTTPReqInfo struct {
	method    string
	uri       string
	referer   string
	code      int
	size      int64
	userAgent string
}

// NewLogger returns a new Logger struct with w output
func NewLogger(w io.Writer) Logger {
	return Logger{
		output: w,
	}
}

// StatusCodeColor formats color code to string
func StatusCodeColor(code int) string {
	switch {
	case code >= http.StatusContinue && code < http.StatusOK:
		return white
	case code >= http.StatusOK && code < http.StatusMultipleChoices:
		return green
	case code >= http.StatusMultipleChoices && code < http.StatusBadRequest:
		return white
	case code >= http.StatusBadRequest && code < http.StatusInternalServerError:
		return yellow
	default:
		return red
	}
}

// Log is the main function of logger. Logs the data in format: status uri method. Status is displayed in matching color.
func (l Logger) Log(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		m := httpsnoop.CaptureMetrics(h, w, r)

		uri := r.URL.String()
		method := r.Method
		status := m.Code

		fmt.Fprintf(l.output, "%v %d %v %s %s\n", StatusCodeColor(status), status, reset, uri, method)
	}

	return http.HandlerFunc(fn)
}
