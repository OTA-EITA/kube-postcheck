package check

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodSecurityCheck flags pods that break the Pod Security Standards "restricted"
// baseline in ways that matter most in practice: privileged containers,
// host namespaces, hostPath volumes, privilege escalation and running as root.
type PodSecurityCheck struct{}

func (PodSecurityCheck) Name() string { return "pod-security" }
func (PodSecurityCheck) Description() string {
	return "Pods avoid privileged mode, host namespaces, hostPath volumes and running as root"
}

func (c PodSecurityCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	pods, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	exempt := toSet(opt.SecurityExemptNamespaces)
	var out []Finding
	clean := 0
	for _, p := range pods.Items {
		if !opt.inScope(p.Namespace) || exempt[p.Namespace] || p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		fs := c.inspect(p)
		if len(fs) == 0 {
			clean++
			continue
		}
		out = append(out, fs...)
	}
	out = append(out, pass(c.Name(), "cluster", fmt.Sprintf("%d pods meet the checked security settings", clean)))
	return out, nil
}

func (c PodSecurityCheck) inspect(p corev1.Pod) []Finding {
	res := "pod/" + p.Namespace + "/" + p.Name
	var failures, warnings []string

	if p.Spec.HostNetwork {
		failures = append(failures, "hostNetwork")
	}
	if p.Spec.HostPID {
		failures = append(failures, "hostPID")
	}
	if p.Spec.HostIPC {
		failures = append(failures, "hostIPC")
	}
	for _, v := range p.Spec.Volumes {
		if v.HostPath != nil {
			warnings = append(warnings, "hostPath volume "+v.Name)
		}
	}

	podNonRoot := p.Spec.SecurityContext != nil && isTrue(p.Spec.SecurityContext.RunAsNonRoot)
	containers := append(append([]corev1.Container{}, p.Spec.InitContainers...), p.Spec.Containers...)
	for _, ct := range containers {
		sc := ct.SecurityContext
		if sc != nil && isTrue(sc.Privileged) {
			failures = append(failures, "privileged container "+ct.Name)
		}
		// allowPrivilegeEscalation defaults to true unless explicitly disabled.
		if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
			warnings = append(warnings, "allowPrivilegeEscalation not false in "+ct.Name)
		}
		nonRoot := podNonRoot
		if sc != nil && sc.RunAsNonRoot != nil {
			nonRoot = *sc.RunAsNonRoot
		}
		if sc != nil && sc.RunAsUser != nil && *sc.RunAsUser == 0 {
			nonRoot = false
		}
		if !nonRoot {
			warnings = append(warnings, "may run as root: "+ct.Name)
		}
	}

	var out []Finding
	if len(failures) > 0 {
		out = append(out, fail(c.Name(), res, strings.Join(failures, ", ")))
	}
	if len(warnings) > 0 {
		out = append(out, warn(c.Name(), res, strings.Join(warnings, ", ")))
	}
	return out
}

func isTrue(b *bool) bool { return b != nil && *b }
