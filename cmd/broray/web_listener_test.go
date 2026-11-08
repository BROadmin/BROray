//go:build !brorayfake

package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeWebShutdownDoesNotRebind(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	done := make(chan struct{})
	go func() { defer close(done); serveWeb(ctx, srv, ln, log.New(io.Discard, "", 0)) }()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("normal Shutdown restarted listener")
	}
	probe, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	probe.Close()
}

func TestServeWebCancellationDuringRecovery(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	done := make(chan struct{})
	go func() { defer close(done); serveWeb(ctx, srv, ln, log.New(io.Discard, "", 0)) }()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	_ = ln.Close()
	// Another owner holds the exact address while recovery is pending. BROray
	// must not take another address, restart VPN or ignore cancellation.
	owner, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	time.Sleep(1200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry ignored cancellation")
	}
	owner.Close()
	probe, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	probe.Close()
}
