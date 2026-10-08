package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSanitizeShellOutput(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"plain Unicode", "Grüße 日本語 🎮\t\r\n", "Grüße 日本語 🎮\t\r\n"},
		{"C0 and DEL", "a\x00b\x01c\x7fd", "abcd"},
		{"CSI color", "\x1b[31mred\x1b[0m", "red"},
		{"repeated ESC", "a\x1b\x1b[31mred\x1b[0m", "ared"},
		{"embedded newline", "a\x1b[\n31mred", "a\nred"},
		{"embedded tab", "a\x1b[\t31mred", "a\tred"},
		{"CSI cursor", "a\x1b[2J\x1b[H\x1b[?25lb", "ab"},
		{"OSC BEL", "a\x1b]0;window title\ab", "ab"},
		{"OSC ST", "a\x1b]0;window title\x1b\\b", "ab"},
		{"OSC hyperlink", "\x1b]8;;https://example.invalid\x1b\\label\x1b]8;;\x1b\\", "label"},
		{"DCS", "a\x1bPpayload\x1b\\b", "ab"},
		{"APC", "a\x1b_payload\x1b\\b", "ab"},
		{"SOS", "a\x1bXpayload\x1b\\b", "ab"},
		{"PM", "a\x1b^payload\x1b\\b", "ab"},
		{"character set", "a\x1b(Bb", "ab"},
		{"simple escape", "a\x1b7b\x1b8c", "abc"},
		{"C1 CSI", "a\u009b31mb\u009b0mc", "abc"},
		{"C1 OSC", "a\u009dtitle\u009cb", "ab"},
		{"C1 DCS", "a\u0090payload\u009cb", "ab"},
		{"other C1", "a\u0085b", "ab"},
		{"invalid UTF-8", "a\xffb", "a\ufffdb"},
		{"incomplete CSI", "a\x1b[31", "a"},
		{"incomplete OSC", "a\x1b]title", "a"},
		{"cancel CSI", "a\x1b[31\x18b", "ab"},
		{"cancel OSC", "a\x1b]title\x1ab", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeShellOutput(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadShellOutput(t *testing.T) {
	cases := []struct {
		name, stream, wantOutput, wantError string
		wantCode                            int
		hold                                bool
	}{
		{"empty success", `{"done":true,"exit_code":0}`, "", "", 0, false},
		{"multiple chunks", "{\"data\":\"Grüße \\u001b[31m\"}\n{\"data\":\"日本語\\u001b[0m\"}\n{\"done\":true,\"exit_code\":0}", "Grüße \x1b[31m日本語\x1b[0m", "", 0, false},
		{"nonzero completion", `{"data":"failed","done":true,"exit_code":7}`, "failed", "", 7, false},
		{"EOF", "", "", "connection closed before", 125, false},
		{"partial EOF", `{"data":"partial"}`, "partial", "connection closed before", 125, false},
		{"malformed JSON", "{\"data\":\"partial\"}\nnot-json", "partial", "reading shell command stream", 125, false},
		{"truncated JSON", "{\"data\":\"partial\"}\n{\"done\":", "partial", "reading shell command stream", 125, false},
		{"server error", `{"data":"partial","error":"execution rejected"}`, "partial", "execution rejected", 1, false},
		{"error with done", `{"done":true,"exit_code":0,"error":"execution rejected"}`, "", "execution rejected", 1, false},
		{"missing exit code", `{"done":true}`, "", "valid integer exit_code", 125, false},
		{"string exit code", `{"done":true,"exit_code":"0"}`, "", "valid integer exit_code", 125, false},
		{"fractional exit code", `{"done":true,"exit_code":0.5}`, "", "valid integer exit_code", 125, false},
		{"rounded fractional exit code", `{"done":true,"exit_code":1.0000000000000001}`, "", "valid integer exit_code", 125, false},
		{"underflow exit code", `{"done":true,"exit_code":1e-400}`, "", "valid integer exit_code", 125, false},
		{"overflow exit code", `{"done":true,"exit_code":9223372036854775808}`, "", "valid integer exit_code", 125, false},
		{"cancelled completion", `{"done":true,"exit_code":-2}`, "", "", -2, false},
		{"timeout", "", "", "timed out", 124, true},
		{"partial timeout", `{"data":"partial"}`, "partial", "timed out", 124, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			stop := make(chan struct{})
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				defer server.Close()
				if tc.stream != "" {
					_, _ = io.WriteString(server, tc.stream+"\n")
				}
				if tc.hold {
					<-stop
				}
			}()
			timeout := 2 * time.Second
			if tc.hold {
				timeout = 50 * time.Millisecond
			}
			output, code, err := readShellOutput(client, timeout)
			close(stop)
			client.Close()
			<-finished
			if output != tc.wantOutput || code != tc.wantCode {
				t.Errorf("output=%q code=%d, want %q code=%d", output, code, tc.wantOutput, tc.wantCode)
			}
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error=%v, want substring %q", err, tc.wantError)
			}
		})
	}
}

func TestDoShellCommandIncompleteResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	oldHost, oldPort := host, port
	host = "127.0.0.1"
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ = strconv.Atoi(p)
	defer func() { host, port = oldHost, oldPort }()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		var req map[string]interface{}
		if json.NewDecoder(conn).Decode(&req) != nil {
			return
		}
		_ = json.NewEncoder(conn).Encode(map[string]interface{}{"data": "\x1b[31mpartial\x1b[0m"})
	}()
	result := doShellCommand("ignored-by-local-mock")
	<-finished
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("incomplete command not marked as MCP error: %+v", result)
	}
	text := result.Content[0].Text
	if !strings.HasPrefix(text, "partial\n[shell error:") || !strings.Contains(text, "connection closed before") {
		t.Fatal(fmt.Sprintf("partial output or error lost: %q", text))
	}
}
