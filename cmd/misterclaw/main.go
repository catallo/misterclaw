package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/catallo/misterclaw/pkg/mister"
	"github.com/catallo/misterclaw/pkg/server"
	"github.com/catallo/misterclaw/pkg/session"
)

const Version = "0.1.0"

func main() {
	port := flag.Int("port", 9900, "TCP port to listen on")
	host := flag.String("host", "0.0.0.0", "Host address to bind to")
	shell := flag.String("shell", "/bin/bash", "Shell to use for command execution")
	installFlag := flag.Bool("install", false, "Install binary and configure autostart, then exit")
	uninstallFlag := flag.Bool("uninstall", false, "Remove autostart entry, then exit")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("misterclaw v%s\n", Version)
		os.Exit(0)
	}

	if *installFlag {
		installServer(*port)
		os.Exit(0)
	}
	if *uninstallFlag {
		uninstallServer()
		os.Exit(0)
	}

	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[misterclaw] ")

	log.Printf("starting misterclaw on %s:%d (shell: %s)", *host, *port, *shell)

	// Start background system discovery (scans ROM folders, cores, extensions)
	mister.StartDiscovery()

	mgr := session.NewManager(*shell)
	srv := server.New(mgr)

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %v, shutting down", sig)
		srv.Close()
		os.Exit(0)
	}()

	addr := fmt.Sprintf("%s:%d", *host, *port)
	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
