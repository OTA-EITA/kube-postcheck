package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestScoreManifestsAndThresholds(t *testing.T) {
	var out bytes.Buffer
	if code := runScore([]string{"../../examples/good.yaml"}, nil, &out, io.Discard); code != exitOK || !strings.Contains(out.String(), "Score 100 (A)") {
		t.Fatalf("good: %d\n%s", code, out.String())
	}
	if code := runScore([]string{"--min-score", "80", "../../examples/"}, nil, io.Discard, io.Discard); code != exitFail {
		t.Fatalf("bad examples are below 80, got %d", code)
	}
	if code := runScore([]string{"--fail-on-danger", "../../examples/bad.yaml"}, nil, io.Discard, io.Discard); code != exitFail {
		t.Fatalf("danger checks fail, got %d", code)
	}
	if code := runScore([]string{"--min-score", "100", "--fail-on-danger", "../../examples/good.yaml"}, nil, io.Discard, io.Discard); code != exitOK {
		t.Fatalf("good example must pass strict thresholds, got %d", code)
	}
}

func TestScoreStdinAndConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("exemptions:\n  - namespaces: [readiness-bad]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../examples/bad.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runScore([]string{"--config", cfg, "--fail-on-danger", "-"}, bytes.NewReader(data), &out, io.Discard); code != exitOK {
		t.Fatalf("an exempt namespace has no failing checks, got %d\n%s", code, out.String())
	}
}

func TestScoreExcludeNamespace(t *testing.T) {
	var out bytes.Buffer
	runScore([]string{"--exclude-namespace", "readiness-bad", "../../examples/"}, nil, &out, io.Discard)
	if strings.Contains(out.String(), "readiness-bad") {
		t.Fatalf("excluded namespace reported:\n%s", out.String())
	}
}

func TestScoreClusterMode(t *testing.T) {
	cs := fake.NewClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "api"},
			Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1), Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "nginx"}}},
			}},
		},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "kube-proxy"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "debug"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "d", Image: "busybox:1.36"}}}},
	)
	orig := scoreClient
	scoreClient = func(string, string) (kubernetes.Interface, error) { return cs, nil }
	t.Cleanup(func() { scoreClient = orig })

	var out bytes.Buffer
	if code := run([]string{"score", "--cluster", "--output", "json"}, &out, io.Discard); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Deployment/app/api", "Pod/app/debug"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(out.String(), "kube-proxy") {
		t.Error("kube-system is excluded by default")
	}
	if code := runScore([]string{"--cluster", "x.yaml"}, nil, io.Discard, io.Discard); code != exitUsage {
		t.Error("--cluster with files is a usage error")
	}
}

func TestScoreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"--output", "html", "x"}, {"--min-score", "120", "x"}, {"missing.yaml"}, {"--config", "missing.yaml", "x"}} {
		if code := runScore(args, nil, io.Discard, io.Discard); code != exitUsage {
			t.Errorf("runScore(%v) = %d", args, code)
		}
	}
	var out bytes.Buffer
	if code := run([]string{"score", "--list"}, &out, io.Discard); code != exitOK || !strings.Contains(out.String(), "SEC-010") {
		t.Errorf("score --list: %s", out.String())
	}
}

func TestScoreSARIFNamesThisTool(t *testing.T) {
	var out bytes.Buffer
	runScore([]string{"--output", "sarif", "../../examples/bad.yaml"}, nil, &out, io.Discard)
	if !strings.Contains(out.String(), `"name": "kube-postcheck"`) || !strings.Contains(out.String(), "../../examples/bad.yaml") {
		t.Errorf("sarif:\n%s", out.String())
	}
}
