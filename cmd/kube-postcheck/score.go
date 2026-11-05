package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/OTA-EITA/kube-postcheck/internal/readiness"
	rreport "github.com/OTA-EITA/kube-postcheck/internal/readiness/report"
	"github.com/OTA-EITA/kube-postcheck/internal/workload"
)

// scoreClient is replaced in tests.
var scoreClient = newClient

// runScore implements "kube-postcheck score": it scores workloads for
// production readiness (reliability, efficiency and security) from manifests
// (files, directories, `helm template` output) or from a live cluster.
func runScore(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("kube-postcheck score", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprintln(stderr, "usage: kube-postcheck score [flags] FILE|DIR|- ...")
		fmt.Fprintln(stderr, "       kube-postcheck score --cluster [flags]")
		fset.PrintDefaults()
	}
	cluster := fset.Bool("cluster", false, "read workloads from the current kubeconfig context instead of files")
	kubeconfig := fset.String("kubeconfig", "", "path to kubeconfig (with --cluster)")
	kubeContext := fset.String("context", "", "kubeconfig context (with --cluster)")
	namespace := fset.String("namespace", "", "only this namespace (with --cluster; default all)")
	exclude := fset.String("exclude-namespace", "kube-system,kube-public,kube-node-lease", "comma separated namespaces to skip")
	configPath := fset.String("config", "", "YAML config with severity overrides and exemptions")
	output := fset.String("output", "table", "output format: table, json, markdown or sarif")
	minScore := fset.Int("min-score", 0, "exit 1 when the overall score is below this (0-100)")
	failOnDanger := fset.Bool("fail-on-danger", false, "exit 1 when any danger check fails")
	timeout := fset.Duration("timeout", 60*time.Second, "overall timeout for API calls (with --cluster)")
	list := fset.Bool("list", false, "list scoring checks and exit")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *list {
		return listScoreChecks(stdout)
	}
	format, err := rreport.ParseFormat(*output)
	if err != nil {
		return usageErr(stderr, err)
	}
	if *minScore < 0 || *minScore > 100 {
		return usageErr(stderr, fmt.Errorf("--min-score must be between 0 and 100"))
	}
	cfg := readiness.Config{}
	if *configPath != "" {
		data, err := os.ReadFile(*configPath) // #nosec G304 -- config path chosen by the operator
		if err != nil {
			return usageErr(stderr, err)
		}
		if cfg, err = readiness.ParseConfig(data); err != nil {
			return usageErr(stderr, err)
		}
	}

	var set workload.Set
	switch {
	case *cluster && fset.NArg() > 0:
		return usageErr(stderr, errors.New("--cluster cannot be combined with files"))
	case *cluster:
		cs, err := scoreClient(*kubeconfig, *kubeContext)
		if err != nil {
			return usageErr(stderr, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if set, err = workload.LoadCluster(ctx, cs, *namespace); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return exitFail
		}
	case fset.NArg() == 0:
		fset.Usage()
		return exitUsage
	default:
		if set, err = loadPaths(fset.Args(), stdin); err != nil {
			return usageErr(stderr, err)
		}
	}
	set = withoutNamespaces(set, *exclude)

	rep := readiness.Evaluate(set, cfg)
	if err := rreport.Write(stdout, rep, format, version); err != nil {
		return usageErr(stderr, err)
	}
	if rep.Score < *minScore || (*failOnDanger && rep.Counts[readiness.Danger] > 0) {
		return exitFail
	}
	return exitOK
}

func usageErr(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", err)
	return exitUsage
}

// loadPaths reads files, recursing into directories for *.yaml and *.yml.
func loadPaths(paths []string, stdin io.Reader) (workload.Set, error) {
	var all workload.Set
	add := func(data []byte, name string) error {
		s, err := workload.LoadManifests(data, name)
		if err != nil {
			return err
		}
		all.Workloads = append(all.Workloads, s.Workloads...)
		all.PDBs = append(all.PDBs, s.PDBs...)
		return nil
	}
	for _, p := range paths {
		if p == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return all, err
			}
			if err := add(data, "stdin"); err != nil {
				return all, err
			}
			continue
		}
		err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if path != p && !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
				return nil
			}
			data, err := os.ReadFile(path) // #nosec G304 -- walking user-specified manifests is the purpose of the command
			if err != nil {
				return err
			}
			return add(data, filepath.ToSlash(path))
		})
		if err != nil {
			return all, err
		}
	}
	return all, nil
}

func withoutNamespaces(set workload.Set, csv string) workload.Set {
	skip := map[string]bool{}
	for _, ns := range strings.Split(csv, ",") {
		if ns = strings.TrimSpace(ns); ns != "" {
			skip[ns] = true
		}
	}
	out := workload.Set{PDBs: set.PDBs}
	for _, w := range set.Workloads {
		if !skip[w.Namespace] {
			out.Workloads = append(out.Workloads, w)
		}
	}
	return out
}

func listScoreChecks(w io.Writer) int {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CHECK\tCATEGORY\tSEVERITY\tTITLE")
	for _, c := range readiness.All() {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Category, c.Severity, c.Title)
	}
	if tw.Flush() != nil {
		return exitUsage
	}
	return exitOK
}
