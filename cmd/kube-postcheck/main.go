// Command kube-postcheck runs health and security checks against a Kubernetes
// cluster right after it has been built or changed, and exits non-zero when
// something needs attention so it can gate CI pipelines and runbooks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/OTA-EITA/kube-postcheck/internal/check"
	"github.com/OTA-EITA/kube-postcheck/internal/report"
)

// Exit codes.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type config struct {
	kubeconfig   string
	kubeContext  string
	output       string
	failOn       string
	quiet        bool
	timeout      time.Duration
	namespaces   csvFlag
	excludeNS    csvFlag
	exemptNS     csvFlag
	only         csvFlag
	skip         csvFlag
	restarts     int
	pendingGrace time.Duration
	certDays     int
	list         bool
	showVersion  bool
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	def := check.DefaultOptions()
	cfg := config{exemptNS: csvFlag{vals: def.SecurityExemptNamespaces}}
	fs := flag.NewFlagSet("kube-postcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.kubeconfig, "kubeconfig", "", "path to kubeconfig (defaults to $KUBECONFIG or ~/.kube/config)")
	fs.StringVar(&cfg.kubeContext, "context", "", "kubeconfig context to use")
	fs.StringVar(&cfg.output, "output", "table", "output format: table, json or markdown")
	fs.StringVar(&cfg.failOn, "fail-on", "fail", "exit non-zero on: fail or warn")
	fs.BoolVar(&cfg.quiet, "quiet", false, "hide PASS findings")
	fs.DurationVar(&cfg.timeout, "timeout", 60*time.Second, "overall timeout for all API calls")
	fs.Var(&cfg.namespaces, "namespace", "comma separated namespaces to check (default: all)")
	fs.Var(&cfg.excludeNS, "exclude-namespace", "comma separated namespaces to skip")
	fs.Var(&cfg.exemptNS, "security-exempt-namespace", "namespaces skipped by pod-security (default: kube-system)")
	fs.Var(&cfg.only, "only", "comma separated checks to run")
	fs.Var(&cfg.skip, "skip", "comma separated checks to skip")
	fs.IntVar(&cfg.restarts, "restart-threshold", int(def.RestartThreshold), "container restarts that trigger a warning (0 disables)")
	fs.DurationVar(&cfg.pendingGrace, "pending-grace", def.PendingGrace, "how long a pod may stay Pending")
	fs.IntVar(&cfg.certDays, "cert-warn-days", def.CertWarnDays, "warn when a TLS certificate expires within this many days")
	fs.BoolVar(&cfg.list, "list", false, "list available checks and exit")
	fs.BoolVar(&cfg.showVersion, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.restarts < 0 || cfg.restarts > math.MaxInt32 {
		return cfg, fmt.Errorf("--restart-threshold must be between 0 and %d", math.MaxInt32)
	}
	if cfg.failOn != "fail" && cfg.failOn != "warn" {
		return cfg, fmt.Errorf("--fail-on must be fail or warn, got %q", cfg.failOn)
	}
	return cfg, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	if cfg.showVersion {
		fmt.Fprintln(stdout, "kube-postcheck", version)
		return exitOK
	}
	if cfg.list {
		if err := listChecks(stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return exitUsage
		}
		return exitOK
	}

	format, err := report.ParseFormat(cfg.output)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	checks, err := check.Select(check.All(), cfg.only.vals, cfg.skip.vals)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	cs, err := newClient(cfg.kubeconfig, cfg.kubeContext)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}

	opt := check.DefaultOptions()
	opt.Namespaces = cfg.namespaces.vals
	opt.ExcludeNamespaces = cfg.excludeNS.vals
	opt.SecurityExemptNamespaces = cfg.exemptNS.vals
	opt.RestartThreshold = int32(cfg.restarts) // #nosec G115 -- range checked in parseFlags
	opt.PendingGrace = cfg.pendingGrace
	opt.CertWarnDays = cfg.certDays

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	r := check.Run(ctx, cs, checks, opt)

	if err := report.Write(stdout, r, format, report.Options{Quiet: cfg.quiet}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	return exitCode(r.Overall, cfg.failOn)
}

func exitCode(overall check.Status, failOn string) int {
	switch {
	case overall == check.StatusFail:
		return exitFail
	case overall == check.StatusWarn && failOn == "warn":
		return exitFail
	default:
		return exitOK
	}
}

func newClient(kubeconfig, kubeContext string) (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	restCfg.UserAgent = "kube-postcheck/" + version
	return kubernetes.NewForConfig(restCfg)
}

func listChecks(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	for _, c := range check.All() {
		fmt.Fprintf(tw, "%s\t%s\n", c.Name(), c.Description())
	}
	return tw.Flush()
}

// csvFlag accepts a comma separated list and may be repeated.
// The first use replaces the default value; later uses append to it.
type csvFlag struct {
	vals    []string
	touched bool
}

func (c *csvFlag) String() string {
	if c == nil {
		return ""
	}
	return strings.Join(c.vals, ",")
}

func (c *csvFlag) Set(v string) error {
	if !c.touched {
		c.vals = nil
		c.touched = true
	}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			c.vals = append(c.vals, p)
		}
	}
	return nil
}
