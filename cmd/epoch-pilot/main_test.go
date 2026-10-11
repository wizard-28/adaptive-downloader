package main

import (
	"testing"
	"time"
)

func TestParseDeadlinesFormalDefault(t *testing.T) {
	p, err := parseDeadlines("none", "none")
	if err != nil {
		t.Fatalf("parseDeadlines(none, none) = %v", err)
	}
	if !p.watchdogOnly() {
		t.Fatalf("formal default is not watchdog-only: %+v", p)
	}
	if p.request != 0 || p.dial != 0 || !p.noRequest || !p.noDial {
		t.Fatalf("formal default = %+v, want explicit disabled deadlines", p)
	}
	if watchdogOnlySuffix(p) == "" {
		t.Fatal("watchdog-only banner suffix missing")
	}
}

func TestParseDeadlinesDiagnosticOverride(t *testing.T) {
	p, err := parseDeadlines("120s", "30s")
	if err != nil {
		t.Fatalf("parseDeadlines(120s, 30s) = %v", err)
	}
	if p.watchdogOnly() || p.request != 120*time.Second || p.dial != 30*time.Second || p.noRequest || p.noDial {
		t.Fatalf("diagnostic override = %+v", p)
	}
	if watchdogOnlySuffix(p) != "" {
		t.Fatal("numeric override must not be labelled watchdog-only")
	}
	if _, err := parseDeadlines("0s", "none"); err == nil {
		t.Fatal("zero duration accepted, want error")
	}
	if _, err := parseDeadlines("none", "bogus"); err == nil {
		t.Fatal("bogus dial deadline accepted, want error")
	}
	if _, err := parseDeadlines("none", "-5s"); err == nil {
		t.Fatal("negative dial deadline accepted, want error")
	}
}
