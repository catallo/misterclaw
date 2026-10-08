package server

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// Review-only localhost integration tests. Assert the existing protocol contract.
func TestPostMergeReviewDaemonPreservesShellBytes(t *testing.T) {
	cases := []struct{ name, command, want string }{
		{"ANSI", "printf '\\033[31mred\\033[0m'", "\x1b[31mred\x1b[0m"},
		{"NUL-separated", "printf 'alpha\\000beta\\000'", "alpha\x00beta\x00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, _ := startTestServer(t)
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			if err := json.NewEncoder(conn).Encode(map[string]interface{}{"cmd": tc.command, "session": "review", "pty": false}); err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(conn)
			var output strings.Builder
			for {
				var resp map[string]interface{}
				if err := decoder.Decode(&resp); err != nil {
					t.Fatal(err)
				}
				if data, ok := resp["data"].(string); ok {
					output.WriteString(data)
				}
				if resp["done"] == true {
					break
				}
			}
			if got := output.String(); got != tc.want {
				t.Fatal(fmt.Sprintf("daemon changed shell bytes: got %q, want %q", got, tc.want))
			}
		})
	}
}
