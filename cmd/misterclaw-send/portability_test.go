package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Run the real entry point in a child process without requiring a local shell.
func TestPortableCLIProcess(t *testing.T) {
	if os.Getenv("MISTERCLAW_CLI_TEST_PROCESS") != "1" {
		t.Skip("child-process entry point")
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing child-process argument boundary")
}

func portableCLI(t *testing.T, args ...string) ([]byte, []byte, int) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestPortableCLIProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "MISTERCLAW_CLI_TEST_PROCESS=1")
	cmd.Dir = t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("client did not exit: %v", ctx.Err())
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.Bytes(), stderr.Bytes(), code
}

// Only loopback is used. An empty connection is the CLI's reachability probe.
func portableServer(t *testing.T, handle func(map[string]interface{}, *json.Encoder) error) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			var req map[string]interface{}
			err = json.NewDecoder(conn).Decode(&req)
			if errors.Is(err, io.EOF) {
				conn.Close()
				continue
			}
			if err == nil {
				err = handle(req, json.NewEncoder(conn))
			}
			conn.Close()
			done <- err
			return
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("loopback peer: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("loopback peer did not finish")
		}
	})
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return host, number
}

func TestPortableCLIVersion(t *testing.T) {
	stdout, stderr, code := portableCLI(t, "--version")
	if code != 0 || string(stdout) != "misterclaw-send v"+Version+"\n" || len(stderr) != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestPortableCLIShell(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprintf("complete=%t", complete), func(t *testing.T) {
			command := "printf 'Grüße 日本語\\n'"
			output := "Grüße 日本語\x00\x1b[31m\r\n"
			host, port := portableServer(t, func(req map[string]interface{}, enc *json.Encoder) error {
				if req["cmd"] != command || req["session"] != "portable-client" || req["pty"] != false {
					return fmt.Errorf("unexpected shell request: %v", req)
				}
				if err := enc.Encode(map[string]interface{}{"data": output}); err != nil {
					return err
				}
				if complete {
					return enc.Encode(map[string]interface{}{"done": true, "exit_code": 7})
				}
				return nil
			})
			stdout, stderr, code := portableCLI(t, "--host", host, "--port", strconv.Itoa(port), "--session", "portable-client", "shell", command)
			wantCode := 7
			if !complete {
				wantCode = 125
			}
			if code != wantCode || string(stdout) != output {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if complete && len(stderr) != 0 {
				t.Fatalf("unexpected stderr: %q", stderr)
			}
			if !complete && !strings.Contains(string(stderr), "before command completed") {
				t.Fatalf("missing incomplete-stream error: %q", stderr)
			}
		})
	}
}

func TestPortableCLIPullPaths(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupt=%t", corrupt), func(t *testing.T) {
			oldHost, oldPort, oldTimeout, oldJSON := hostFlag, portFlag, timeoutFlag, jsonFlag
			t.Cleanup(func() { hostFlag, portFlag, timeoutFlag, jsonFlag = oldHost, oldPort, oldTimeout, oldJSON })
			payload := []byte("binary\x00data\r\n日本語")
			hash := sha256.Sum256(payload)
			checksum := hex.EncodeToString(hash[:])
			if corrupt {
				checksum = strings.Repeat("0", 64)
			}
			remote := "/media/fat/games/SNES/Game Name.sfc"
			hostFlag, portFlag = portableServer(t, func(req map[string]interface{}, enc *json.Encoder) error {
				if req["pull"] != true || req["path"] != remote {
					return fmt.Errorf("remote POSIX path changed: %v", req)
				}
				if err := enc.Encode(map[string]interface{}{"pull_data": base64.StdEncoding.EncodeToString(payload)}); err != nil {
					return err
				}
				return enc.Encode(map[string]interface{}{"pull_done": true, "size": len(payload), "sha256": checksum})
			})
			timeoutFlag, jsonFlag = 5, false
			dir := filepath.Join(t.TempDir(), "local folder 日本語")
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			local := filepath.Join(dir, "Game Name.sfc")
			original := []byte("existing destination")
			if err := os.WriteFile(local, original, 0644); err != nil {
				t.Fatal(err)
			}
			err := cmdPull([]string{remote, local})
			if corrupt && (err == nil || !strings.Contains(err.Error(), "sha256 mismatch")) {
				t.Fatalf("expected checksum error, got %v", err)
			}
			if !corrupt && err != nil {
				t.Fatal(err)
			}
			want := payload
			if corrupt {
				want = original
			}
			got, err := os.ReadFile(local)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("destination=%q error=%v, want %q", got, err, want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(local) {
				t.Fatalf("unexpected transfer leftovers: %v, error=%v", entries, err)
			}
		})
	}
}
