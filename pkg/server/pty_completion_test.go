package server

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPTYCompletionReportsInheritedSlaveTimeout(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)
	if err := conn.SetDeadline(time.Now().Add(12 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// The shell keeps its terminal until the test has opened an extra slave.
	fmt.Fprintln(conn, `{"id":"pty-drain","cmd":"stty -echo; tty; read token; printf PTY_FINAL","session":"pty-drain","pty":true}`)
	var first strings.Builder
	for !strings.Contains(first.String(), "\n") {
		if !scanner.Scan() {
			t.Fatalf("missing slave path: %v", scanner.Err())
		}
		var response map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response["done"] == true {
			t.Fatalf("command finished before handshake: %v", response)
		}
		if data, ok := response["data"].(string); ok {
			first.WriteString(data)
		}
	}
	path := strings.TrimSpace(first.String())
	if !strings.HasPrefix(path, "/dev/pts/") {
		t.Fatalf("unexpected slave path: %q", path)
	}
	slave, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	fmt.Fprintln(conn, `{"input":"go\n","session":"pty-drain"}`)
	var output strings.Builder
	for scanner.Scan() {
		var response map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if data, ok := response["data"].(string); ok {
			output.WriteString(data)
		}
		if response["done"] != true {
			continue
		}
		if !strings.Contains(output.String(), "PTY_FINAL") {
			t.Fatalf("lost final output: %q", output.String())
		}
		if response["exit_code"] == float64(0) {
			t.Fatalf("truncated stream reported success: %v", response)
		}
		message, ok := response["error"].(string)
		if !ok || !strings.Contains(message, "PTY output drain timed out") {
			t.Fatalf("missing explicit completion error: %v", response)
		}
		return
	}
	t.Fatalf("missing bounded completion: %v", scanner.Err())
}

func TestPTYFinalOutputPrecedesCompletion(t *testing.T) {
	addr, _ := startTestServer(t)
	conn, scanner := dial(t, addr)
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	for n := 0; n < 50; n++ {
		fmt.Fprintln(conn, `{"id":"pty-final","cmd":"printf PTY_SENTINEL","session":"pty-final","pty":true}`)
		var output strings.Builder
		completed := false
		for scanner.Scan() {
			var response map[string]interface{}
			if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if data, ok := response["data"].(string); ok {
				output.WriteString(data)
			}
			if response["done"] == true {
				if output.String() != "PTY_SENTINEL" || response["exit_code"] != float64(0) || response["error"] != nil {
					t.Fatalf("completion before intact output: output=%q response=%v", output.String(), response)
				}
				completed = true
				break
			}
		}
		if !completed {
			t.Fatalf("missing completion: %v", scanner.Err())
		}
	}
}
