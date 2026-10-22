package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/OTA-EITA/kube-postcheck/internal/check"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		overall check.Status
		failOn  string
		want    int
	}{
		{check.StatusPass, "fail", exitOK},
		{check.StatusWarn, "fail", exitOK},
		{check.StatusWarn, "warn", exitFail},
		{check.StatusFail, "fail", exitFail},
		{check.StatusFail, "warn", exitFail},
	}
	for _, c := range cases {
		if got := exitCode(c.overall, c.failOn); got != c.want {
			t.Errorf("exitCode(%s, %s) = %d, want %d", c.overall, c.failOn, got, c.want)
		}
	}
}

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{
		"--namespace", "app,db", "--namespace", "web",
		"--security-exempt-namespace", "kube-system,rook-ceph",
		"--skip", "rbac", "--fail-on", "warn",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.namespaces.String(); got != "app,db,web" {
		t.Errorf("namespaces = %q", got)
	}
	if got := cfg.exemptNS.String(); got != "kube-system,rook-ceph" {
		t.Errorf("exempt namespaces = %q, the default should be replaced", got)
	}
	if cfg.failOn != "warn" || cfg.skip.String() != "rbac" {
		t.Errorf("unexpected config %+v", cfg)
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.exemptNS.String() != "kube-system" || cfg.output != "table" || cfg.failOn != "fail" {
		t.Errorf("unexpected defaults %+v", cfg)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{
		{"--fail-on", "maybe"},
		{"--restart-threshold", "-1"},
		{"--output", "yaml"},
		{"--skip", "typo"},
	} {
		var stderr bytes.Buffer
		if code := run(args, io.Discard, &stderr); code != exitUsage {
			t.Errorf("run(%v) = %d, want %d (stderr: %s)", args, code, exitUsage, stderr.String())
		}
	}
}

func TestRunList(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--list"}, &out, io.Discard); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, c := range check.All() {
		if !strings.Contains(out.String(), c.Name()) {
			t.Errorf("--list is missing %s", c.Name())
		}
	}
}
