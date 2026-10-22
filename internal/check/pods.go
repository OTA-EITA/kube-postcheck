package check

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodCheck reports pods that are failing, stuck Pending, crash looping,
// unable to pull their image, or restarting too often.
// Healthy pods are summarised in a single PASS finding to keep the report short.
type PodCheck struct{}

func (PodCheck) Name() string { return "pods" }
func (PodCheck) Description() string {
	return "No pods are Failed, stuck Pending, crash looping or restarting excessively"
}

// badWaitingReasons are container states that never resolve on their own.
var badWaitingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"InvalidImageName":           true,
}

func (c PodCheck) Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error) {
	pods, err := cs.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	var out []Finding
	healthy := 0
	for _, p := range pods.Items {
		if !opt.inScope(p.Namespace) || p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		fs := c.inspect(p, opt)
		if len(fs) == 0 {
			healthy++
			continue
		}
		out = append(out, fs...)
	}
	out = append(out, pass(c.Name(), "cluster", fmt.Sprintf("%d pods healthy", healthy)))
	return out, nil
}

func (c PodCheck) inspect(p corev1.Pod, opt Options) []Finding {
	res := "pod/" + p.Namespace + "/" + p.Name
	switch p.Status.Phase {
	case corev1.PodFailed:
		return []Finding{fail(c.Name(), res, "phase Failed: "+p.Status.Reason)}
	case corev1.PodPending:
		age := opt.now().Sub(p.CreationTimestamp.Time)
		if age > opt.PendingGrace {
			return []Finding{fail(c.Name(), res, fmt.Sprintf("Pending for %s", age.Round(1e9)))}
		}
	}

	var out []Finding
	statuses := append(append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...)
	for _, s := range statuses {
		if w := s.State.Waiting; w != nil && badWaitingReasons[w.Reason] {
			out = append(out, fail(c.Name(), res, fmt.Sprintf("container %s: %s", s.Name, w.Reason)))
			continue
		}
		if opt.RestartThreshold > 0 && s.RestartCount >= opt.RestartThreshold {
			out = append(out, warn(c.Name(), res, fmt.Sprintf("container %s restarted %d times", s.Name, s.RestartCount)))
		}
	}
	return out
}
