package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"testing"
)

// W4.5 MJ-2: classifySanitizeError должен опираться на errors.Is/As,
// а не на strings.Contains — иначе локализованные и обёрнутые ошибки
// сваливаются в "unknown" и теряют диагностическую ценность.
func TestClassifySanitizeErrorTypeBased(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil → unknown", nil, "unknown"},
		{"context.DeadlineExceeded → timeout",
			context.DeadlineExceeded, "timeout"},
		{"context.Canceled → timeout",
			context.Canceled, "timeout"},
		{"wrapped DeadlineExceeded → timeout",
			fmt.Errorf("sanitize: %w", context.DeadlineExceeded), "timeout"},
		{"url.Error timeout=true → timeout",
			&url.Error{Op: "Post", URL: "http://x",
				Err: timeoutErr{}}, "timeout"},
		{"net.OpError → network",
			&net.OpError{Op: "dial", Net: "tcp",
				Err: errors.New("blocked")}, "network"},
		{"io.EOF → network",
			io.EOF, "network"},
		{"io.ErrUnexpectedEOF → network",
			io.ErrUnexpectedEOF, "network"},
		// String-fallback для систем без типизированных ошибок.
		{"string deadline → timeout (fallback)",
			errors.New("server deadline exceeded"), "timeout"},
		{"string connection refused → network (fallback)",
			errors.New("connection refused by peer"), "network"},
		{"unknown msg → unknown",
			errors.New("что-то странное"), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySanitizeError(tc.err)
			if got != tc.want {
				t.Errorf("classifySanitizeError(%v) = %q, ожидалось %q",
					tc.err, got, tc.want)
			}
		})
	}
}

// timeoutErr — реализует interface{ Timeout() bool } для теста
// `errors.As(&t)` пути классификации.
type timeoutErr struct{}

func (timeoutErr) Error() string { return "test: i/o timeout" }
func (timeoutErr) Timeout() bool { return true }
