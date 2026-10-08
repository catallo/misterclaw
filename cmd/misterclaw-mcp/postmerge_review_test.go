package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Review-only tests in an isolated checkout; no MiSTer or external services.
func TestPostMergeReviewJSONAlreadyEscapesControls(t *testing.T) {
	original := "NUL:\x00 ESC:\x1b DEL:\x7f newline:\n Unicode:ä日本語"
	encoded, err := json.Marshal(map[string]string{"data": original})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("invalid JSON: %q", encoded)
	}
	var decoded map[string]string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["data"] != original {
		t.Fatalf("round-trip changed output: %q", decoded["data"])
	}
	t.Log("Existing encoding/json path handles these control characters without sanitization")
}

func TestPostMergeReviewUnicodePreserved(t *testing.T) {
	original := "Grüße 日本語 🎮\t\r\n"
	if got := sanitizeShellOutput(original); got != original {
		t.Fatalf("got %q, want %q", got, original)
	}
}

func TestPostMergeReviewANSIIsNotMutilated(t *testing.T) {
	original := "\x1b[31mred\x1b[0m"
	got := sanitizeShellOutput(original)
	if got != original && got != "red" {
		t.Fatalf("ANSI must be preserved or removed completely, not mutilated: got %q", got)
	}
}

func TestPostMergeReviewEOFIsNotSuccess(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%v", partial), func(t *testing.T) {
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
				if partial {
					_ = json.NewEncoder(conn).Encode(map[string]interface{}{"data": "partial output"})
				}
				// Intentionally disconnect before a done=true completion record.
			}()
			output, code, err := sendShellCommand("ignored-by-local-mock")
			<-finished
			if err == nil || code == 0 {
				t.Errorf("premature EOF reported success: output=%q code=%d error=%v", output, code, err)
			}
			if strings.Contains(output, "due to binary data") {
				t.Log("ordinary disconnect is misdiagnosed as binary output")
			}
		})
	}
}
