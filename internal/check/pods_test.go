package check

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(ns, name string, phase corev1.PodPhase, created time.Time, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, CreationTimestamp: metav1.NewTime(created)},
		Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: statuses},
	}
}

func waiting(name, reason string) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}
}

func TestPodCheck(t *testing.T) {
	old := testNow.Add(-time.Hour)
	fresh := testNow.Add(-time.Minute)
	restarting := corev1.ContainerStatus{Name: "app", RestartCount: 7}

	fs := runCheck(t, PodCheck{}, testOptions(),
		pod("app", "ok", corev1.PodRunning, old),
		pod("app", "crash", corev1.PodRunning, old, waiting("app", "CrashLoopBackOff")),
		pod("app", "pull", corev1.PodPending, fresh, waiting("app", "ImagePullBackOff")),
		pod("app", "stuck", corev1.PodPending, old),
		pod("app", "starting", corev1.PodPending, fresh),
		pod("app", "flaky", corev1.PodRunning, old, restarting),
		pod("app", "failed", corev1.PodFailed, old),
		pod("app", "job-done", corev1.PodSucceeded, old),
	)
	expectStatus(t, fs, "pod/app/crash", StatusFail)
	expectStatus(t, fs, "pod/app/pull", StatusFail)
	expectStatus(t, fs, "pod/app/stuck", StatusFail)
	expectStatus(t, fs, "pod/app/flaky", StatusWarn)
	expectStatus(t, fs, "pod/app/failed", StatusFail)
	if f := expectStatus(t, fs, "cluster", StatusPass); f.Message != "2 pods healthy" {
		t.Errorf("healthy summary = %q, want ok and starting counted", f.Message)
	}
	if _, ok := byResource(fs)["pod/app/job-done"]; ok {
		t.Error("Succeeded pods should be ignored")
	}
}

func TestPodCheckRestartThresholdDisabled(t *testing.T) {
	opt := testOptions()
	opt.RestartThreshold = 0
	fs := runCheck(t, PodCheck{}, opt,
		pod("app", "flaky", corev1.PodRunning, testNow, corev1.ContainerStatus{Name: "app", RestartCount: 100}),
	)
	if len(fs) != 1 || fs[0].Status != StatusPass {
		t.Fatalf("threshold 0 should disable restart warnings, got %+v", fs)
	}
}
