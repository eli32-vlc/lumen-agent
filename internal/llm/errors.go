package llm

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
)

// IsTimeoutError reports whether err describes a deadline or timeout condition.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if os.IsTimeout(err) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"context deadline exceeded",
		"deadline exceeded",
		"client.timeout exceeded",
		"i/o timeout",
		"timed out",
		"timeout",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}

	return false
}

// IsRetriableError reports whether err is likely transient and safe to retry.
func IsRetriableError(err error) bool {
	if err == nil {
		return false
	}

	if IsTimeoutError(err) {
		return true
	}

	if code, ok := parseAPIErrorStatusCode(err.Error()); ok {
		switch code {
		case 408, 409, 425, 429, 500, 502, 503, 504, 520, 521, 522, 523, 524:
			return true
		}
	}

	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"connection reset by peer",
		"connection refused",
		"broken pipe",
		"temporarily unavailable",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}

	return false
}

func parseAPIErrorStatusCode(message string) (int, bool) {
	lower := strings.ToLower(message)
	marker := "api error ("
	start := strings.Index(lower, marker)
	if start < 0 {
		return 0, false
	}

	start += len(marker)
	end := strings.Index(lower[start:], ")")
	if end < 0 {
		return 0, false
	}

	code, err := strconv.Atoi(strings.TrimSpace(lower[start : start+end]))
	if err != nil {
		return 0, false
	}

	return code, true
}
