package server

import "testing"

func TestLocationRescanReturnsPendingInsteadOfBlocking(t *testing.T) {
	addr, srv := startTestServer(t)
	calls := make(chan string, 1)
	srv.startLocationRescan = func(location string) bool { calls <- location; return true }
	conn, scanner := dial(t, addr)
	response := sendAndRead(t, conn, scanner, `{"mister":"rescan","location":"sd"}`)
	if response["success"] != true || response["status"] != "pending" || response["location"] != "sd" {
		t.Fatalf("rescan acceptance=%+v", response)
	}
	if location := <-calls; location != "sd" {
		t.Fatalf("rescan location=%q", location)
	}
	if _, exists := response["systems_found"]; exists {
		t.Fatal("pending scan must not invent a completed count")
	}
}

func TestBusyOrInvalidLocationRescanReportsFailure(t *testing.T) {
	addr, srv := startTestServer(t)
	srv.startLocationRescan = func(string) bool { return false }
	conn, scanner := dial(t, addr)
	response := sendAndRead(t, conn, scanner, `{"mister":"rescan","location":"usb99"}`)
	if response["success"] != false || response["error"] == nil {
		t.Fatalf("rejected rescan=%+v", response)
	}
}

func TestBusyFullRescanDoesNotInvalidateActiveJob(t *testing.T) {
	addr, srv := startTestServer(t)
	srv.startFullRescan = func() bool { return false }
	conn, scanner := dial(t, addr)
	response := sendAndRead(t, conn, scanner, `{"mister":"rescan"}`)
	if response["success"] != false || response["error"] == nil {
		t.Fatalf("busy full rescan=%+v", response)
	}
}
