// Package check implements post-build health and security checks for a Kubernetes cluster.
package check

import (
	"context"
	"fmt"
	"sort"
	"time"

	"k8s.io/client-go/kubernetes"
)

// Status is the outcome of a single finding.
type Status string

const (
	StatusPass Status = "PASS"
	StatusWarn Status = "WARN"
	StatusFail Status = "FAIL"
)

// severity orders statuses so the worst one can be picked.
func (s Status) severity() int {
	switch s {
	case StatusFail:
		return 2
	case StatusWarn:
		return 1
	default:
		return 0
	}
}

// Worse reports whether s is more severe than other.
func (s Status) Worse(other Status) bool { return s.severity() > other.severity() }

// Finding is one observation produced by a check.
type Finding struct {
	Check    string `json:"check"`
	Status   Status `json:"status"`
	Resource string `json:"resource"`
	Message  string `json:"message"`
}

// Options tune the thresholds used by the checks.
type Options struct {
	// Namespaces limits namespaced checks. Empty means all namespaces.
	Namespaces []string
	// ExcludeNamespaces are skipped even when Namespaces is empty.
	ExcludeNamespaces []string
	// RestartThreshold is the container restart count that triggers a warning.
	RestartThreshold int32
	// PendingGrace is how long a pod may stay Pending before it is reported.
	PendingGrace time.Duration
	// CertWarnDays warns when a TLS certificate expires within this many days.
	CertWarnDays int
	// SecurityExemptNamespaces are skipped by the pod security check, because
	// CNI, CSI and other system agents legitimately need host access.
	SecurityExemptNamespaces []string
	// Now is injectable for tests.
	Now func() time.Time
}

// DefaultOptions returns the thresholds used by the CLI unless overridden.
func DefaultOptions() Options {
	return Options{
		RestartThreshold:         5,
		PendingGrace:             5 * time.Minute,
		CertWarnDays:             30,
		SecurityExemptNamespaces: []string{"kube-system"},
		Now:                      time.Now,
	}
}

func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

// inScope reports whether a namespace should be inspected.
func (o Options) inScope(ns string) bool {
	for _, ex := range o.ExcludeNamespaces {
		if ex == ns {
			return false
		}
	}
	if len(o.Namespaces) == 0 {
		return true
	}
	for _, n := range o.Namespaces {
		if n == ns {
			return true
		}
	}
	return false
}

// Checker is a single named check.
type Checker interface {
	Name() string
	Description() string
	Run(ctx context.Context, cs kubernetes.Interface, opt Options) ([]Finding, error)
}

// Select returns the checks whose names are not in skip. Unknown names are an error
// so that typos in --skip do not silently disable nothing.
func Select(all []Checker, only, skip []string) ([]Checker, error) {
	known := map[string]bool{}
	for _, c := range all {
		known[c.Name()] = true
	}
	for _, n := range append(append([]string{}, only...), skip...) {
		if !known[n] {
			return nil, fmt.Errorf("unknown check %q", n)
		}
	}
	onlySet := toSet(only)
	skipSet := toSet(skip)
	var out []Checker
	for _, c := range all {
		if len(onlySet) > 0 && !onlySet[c.Name()] {
			continue
		}
		if skipSet[c.Name()] {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Report is the aggregated result of a run.
type Report struct {
	Findings []Finding      `json:"findings"`
	Errors   []CheckError   `json:"errors,omitempty"`
	Summary  map[Status]int `json:"summary"`
	Overall  Status         `json:"overall"`
	Duration time.Duration  `json:"-"`
}

// CheckError records a check that could not run, e.g. because of missing RBAC.
type CheckError struct {
	Check string `json:"check"`
	Error string `json:"error"`
}

// Run executes the checks and aggregates their findings.
// A check that errors is recorded and treated as FAIL, but does not stop the others.
func Run(ctx context.Context, cs kubernetes.Interface, checks []Checker, opt Options) Report {
	start := opt.now()
	r := Report{Summary: map[Status]int{StatusPass: 0, StatusWarn: 0, StatusFail: 0}, Overall: StatusPass}
	for _, c := range checks {
		fs, err := c.Run(ctx, cs, opt)
		if err != nil {
			r.Errors = append(r.Errors, CheckError{Check: c.Name(), Error: err.Error()})
			r.Overall = StatusFail
			continue
		}
		for _, f := range fs {
			r.Summary[f.Status]++
			if f.Status.Worse(r.Overall) {
				r.Overall = f.Status
			}
		}
		r.Findings = append(r.Findings, fs...)
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return r.Findings[i].Status.Worse(r.Findings[j].Status)
	})
	r.Duration = opt.now().Sub(start)
	return r
}

func pass(check, resource, msg string) Finding {
	return Finding{Check: check, Status: StatusPass, Resource: resource, Message: msg}
}

func warn(check, resource, msg string) Finding {
	return Finding{Check: check, Status: StatusWarn, Resource: resource, Message: msg}
}

func fail(check, resource, msg string) Finding {
	return Finding{Check: check, Status: StatusFail, Resource: resource, Message: msg}
}
