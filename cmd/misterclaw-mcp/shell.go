package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// readShellOutput requires an explicit completion record. EOF, invalid JSON,
// and timeouts must never turn an unfinished command into a successful result.
// Partial output is preserved so callers can display it alongside the error.
func readShellOutput(conn net.Conn, timeout time.Duration) (string, int, error) {
	var output strings.Builder
	dec := json.NewDecoder(conn)
	// Preserve exit-code number text so rounded floats cannot look successful.
	dec.UseNumber()
	for {
		// A peer-closed pipe can reject the deadline even when the JSON
		// decoder has already buffered the completion record. Drain that
		// buffer; Decode will report EOF if no completion record was received.
		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			return output.String(), 125, fmt.Errorf("setting shell read deadline: %w", err)
		}
		var resp map[string]interface{}
		if err := dec.Decode(&resp); err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return output.String(), 124, fmt.Errorf("shell output timed out after %s before completion (command may still be running): %w", timeout, err)
			}
			if errors.Is(err, io.EOF) {
				return output.String(), 125, fmt.Errorf("connection closed before shell command completed: %w", err)
			}
			return output.String(), 125, fmt.Errorf("reading shell command stream: %w", err)
		}
		if data, ok := resp["data"].(string); ok {
			output.WriteString(data)
		}
		if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
			return output.String(), 1, fmt.Errorf("%s", errMsg)
		}
		if done, ok := resp["done"].(bool); ok && done {
			code, ok := resp["exit_code"].(json.Number)
			if !ok {
				return output.String(), 125, fmt.Errorf("shell completion record has no valid integer exit_code")
			}
			value, err := strconv.Atoi(code.String())
			if err != nil {
				return output.String(), 125, fmt.Errorf("shell completion record has no valid integer exit_code: %w", err)
			}
			return output.String(), value, nil
		}
	}
}

// sanitizeShellOutput makes a plain-text MCP presentation. JSON encoding itself
// already escapes controls. Remove entire terminal sequences, not just ESC,
// while retaining printable Unicode and tab/newline/carriage-return formatting.
// As with encoding/json, invalid UTF-8 is represented by replacement characters.
func sanitizeShellOutput(s string) string {
	const (
		plain = iota
		escape
		intermediate
		csi
		osc
		controlString
		stringEscape
	)
	state, stringState := plain, plain
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if state != plain && (r == '\x18' || r == '\x1a') {
			state = plain // CAN/SUB cancel an incomplete terminal sequence.
			continue
		}
		if state == escape || state == intermediate || state == csi {
			if r == '\x1b' {
				state = escape // ESC restarts a non-string sequence.
				continue
			}
			if r == '\t' || r == '\n' || r == '\r' {
				buf.WriteRune(r)
				continue
			}
		}
		switch state {
		case plain:
			switch r {
			case '\x1b':
				state = escape
			case '\u009b':
				state = csi
			case '\u009d':
				state = osc
			case '\u0090', '\u0098', '\u009e', '\u009f':
				state = controlString
			default:
				if !unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r' {
					buf.WriteRune(r)
				}
			}
		case escape:
			switch r {
			case '[':
				state = csi
			case ']':
				state = osc
			case 'P', 'X', '^', '_':
				state = controlString
			default:
				if r >= 0x20 && r <= 0x2f {
					state = intermediate
				} else {
					state = plain
				}
			}
		case intermediate:
			if r < 0x20 || r > 0x2f {
				state = plain
			}
		case csi:
			if r == '\x1b' {
				state = escape
			} else if r >= 0x40 && r <= 0x7e {
				state = plain
			}
		case osc, controlString:
			if r == '\u009c' || (state == osc && r == '\a') {
				state = plain
			} else if r == '\x1b' {
				stringState, state = state, stringEscape
			}
		case stringEscape:
			if r == '\\' || r == '\u009c' || (stringState == osc && r == '\a') {
				state = plain
			} else if r != '\x1b' {
				state = stringState
			}
		}
	}
	return buf.String()
}
