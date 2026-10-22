package check

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func testOptions() Options {
	o := DefaultOptions()
	o.Now = func() time.Time { return testNow }
	return o
}

func runCheck(t *testing.T, c Checker, opt Options, objs ...runtime.Object) []Finding {
	t.Helper()
	fs, err := c.Run(context.Background(), fake.NewClientset(objs...), opt)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", c.Name(), err)
	}
	return fs
}

// byResource indexes findings so tests can assert on a specific object.
func byResource(fs []Finding) map[string]Finding {
	m := make(map[string]Finding, len(fs))
	for _, f := range fs {
		m[f.Resource] = f
	}
	return m
}

func expectStatus(t *testing.T, fs []Finding, resource string, want Status) Finding {
	t.Helper()
	f, ok := byResource(fs)[resource]
	if !ok {
		t.Fatalf("no finding for %s in %+v", resource, fs)
	}
	if f.Status != want {
		t.Fatalf("%s: status = %s, want %s (%s)", resource, f.Status, want, f.Message)
	}
	return f
}

func TestStatusWorse(t *testing.T) {
	if !StatusFail.Worse(StatusWarn) || !StatusWarn.Worse(StatusPass) {
		t.Fatal("expected FAIL > WARN > PASS")
	}
	if StatusPass.Worse(StatusPass) {
		t.Fatal("a status is not worse than itself")
	}
}

func TestInScope(t *testing.T) {
	o := Options{Namespaces: []string{"app"}, ExcludeNamespaces: []string{"app-old"}}
	for ns, want := range map[string]bool{"app": true, "other": false, "app-old": false} {
		if got := o.inScope(ns); got != want {
			t.Errorf("inScope(%q) = %v, want %v", ns, got, want)
		}
	}
	all := Options{ExcludeNamespaces: []string{"kube-system"}}
	if !all.inScope("default") || all.inScope("kube-system") {
		t.Error("empty Namespaces should mean all except excluded")
	}
}

func TestSelect(t *testing.T) {
	got, err := Select(All(), nil, []string{"rbac", "pods"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Name() == "rbac" || c.Name() == "pods" {
			t.Fatalf("%s should have been skipped", c.Name())
		}
	}
	only, err := Select(All(), []string{"nodes"}, nil)
	if err != nil || len(only) != 1 || only[0].Name() != "nodes" {
		t.Fatalf("only=nodes: got %v, %v", only, err)
	}
	if _, err := Select(All(), nil, []string{"typo"}); err == nil {
		t.Fatal("unknown check names must be rejected")
	}
}

type stubCheck struct {
	name string
	fs   []Finding
	err  error
}

func (s stubCheck) Name() string        { return s.name }
func (s stubCheck) Description() string { return "stub" }
func (s stubCheck) Run(context.Context, kubernetes.Interface, Options) ([]Finding, error) {
	return s.fs, s.err
}

func TestRunAggregates(t *testing.T) {
	checks := []Checker{
		stubCheck{name: "a", fs: []Finding{pass("a", "x", ""), warn("a", "y", "")}},
		stubCheck{name: "b", fs: []Finding{pass("b", "z", "")}},
	}
	r := Run(context.Background(), fake.NewClientset(), checks, testOptions())
	if r.Overall != StatusWarn {
		t.Fatalf("overall = %s, want WARN", r.Overall)
	}
	if r.Summary[StatusPass] != 2 || r.Summary[StatusWarn] != 1 {
		t.Fatalf("summary = %v", r.Summary)
	}
	if r.Findings[0].Status != StatusWarn {
		t.Fatal("findings should be sorted worst first")
	}
}

func TestRunRecordsErrorsAsFail(t *testing.T) {
	checks := []Checker{
		stubCheck{name: "broken", err: errors.New("forbidden")},
		stubCheck{name: "ok", fs: []Finding{pass("ok", "x", "")}},
	}
	r := Run(context.Background(), fake.NewClientset(), checks, testOptions())
	if r.Overall != StatusFail || len(r.Errors) != 1 || len(r.Findings) != 1 {
		t.Fatalf("got overall=%s errors=%v findings=%v", r.Overall, r.Errors, r.Findings)
	}
}
