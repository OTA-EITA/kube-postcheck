package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// hardened returns a container that satisfies every setting the check looks at.
func hardened(name string) corev1.Container {
	return corev1.Container{
		Name: name,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
	}
}

func specPod(ns, name string, spec corev1.PodSpec) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       spec,
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestPodSecurityCheck(t *testing.T) {
	privileged := hardened("app")
	privileged.SecurityContext.Privileged = ptr.To(true)

	rootUser := hardened("app")
	rootUser.SecurityContext.RunAsUser = ptr.To[int64](0)

	podLevelNonRoot := corev1.Container{Name: "app", SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)}}

	fs := runCheck(t, PodSecurityCheck{}, testOptions(),
		specPod("app", "good", corev1.PodSpec{Containers: []corev1.Container{hardened("app")}}),
		specPod("app", "pod-level", corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true)},
			Containers:      []corev1.Container{podLevelNonRoot},
		}),
		specPod("app", "priv", corev1.PodSpec{Containers: []corev1.Container{privileged}}),
		specPod("app", "hostnet", corev1.PodSpec{HostNetwork: true, Containers: []corev1.Container{hardened("app")}}),
		specPod("app", "defaults", corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}),
		specPod("app", "root", corev1.PodSpec{Containers: []corev1.Container{rootUser}}),
		specPod("app", "hostpath", corev1.PodSpec{
			Containers: []corev1.Container{hardened("app")},
			Volumes:    []corev1.Volume{{Name: "docker", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/run"}}}},
		}),
		specPod("kube-system", "calico-node", corev1.PodSpec{HostNetwork: true, Containers: []corev1.Container{privileged}}),
	)

	if _, ok := byResource(fs)["pod/app/good"]; ok {
		t.Error("hardened pod should not be reported")
	}
	if _, ok := byResource(fs)["pod/app/pod-level"]; ok {
		t.Error("pod level runAsNonRoot should be honoured")
	}
	if f := expectStatus(t, fs, "pod/app/priv", StatusFail); !strings.Contains(f.Message, "privileged container app") {
		t.Errorf("message = %q", f.Message)
	}
	expectStatus(t, fs, "pod/app/hostnet", StatusFail)

	warns := findingsFor(fs, "pod/app/defaults")
	if len(warns) != 1 || warns[0].Status != StatusWarn ||
		!strings.Contains(warns[0].Message, "allowPrivilegeEscalation") || !strings.Contains(warns[0].Message, "may run as root") {
		t.Errorf("container with no securityContext: %+v", warns)
	}
	if f := expectStatus(t, fs, "pod/app/root", StatusWarn); !strings.Contains(f.Message, "may run as root") {
		t.Errorf("runAsUser 0 should be reported, got %q", f.Message)
	}
	expectStatus(t, fs, "pod/app/hostpath", StatusWarn)
	if _, ok := byResource(fs)["pod/kube-system/calico-node"]; ok {
		t.Error("kube-system is exempt by default")
	}
}

func TestPodSecurityCheckExemptionCanBeCleared(t *testing.T) {
	opt := testOptions()
	opt.SecurityExemptNamespaces = nil
	fs := runCheck(t, PodSecurityCheck{}, opt,
		specPod("kube-system", "calico-node", corev1.PodSpec{HostNetwork: true, Containers: []corev1.Container{hardened("app")}}),
	)
	expectStatus(t, fs, "pod/kube-system/calico-node", StatusFail)
}

func findingsFor(fs []Finding, resource string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Resource == resource {
			out = append(out, f)
		}
	}
	return out
}
