// bsa 是体积归因分析命令行入口。
//
// 子命令：
//
//	bsa analyze <manifest.json>                  体积归因报告
//	bsa estimate <file> [-alg gzip] [-level N]   传输体积估算
//	bsa diff <base.json> <next.json>             增量对比
//	bsa budget <manifest.json> <rules.json>      预算判定（超限退出码为 2）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/huchangjun118/bundle-size-analyzer-go/internal/attribution"
	"github.com/huchangjun118/bundle-size-analyzer-go/internal/budget"
	"github.com/huchangjun118/bundle-size-analyzer-go/internal/diff"
	"github.com/huchangjun118/bundle-size-analyzer-go/internal/estimate"
	"github.com/huchangjun118/bundle-size-analyzer-go/internal/manifest"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}
	var err error
	code := 0
	switch args[0] {
	case "analyze":
		err = cmdAnalyze(args[1:])
	case "estimate":
		err = cmdEstimate(args[1:])
	case "diff":
		err = cmdDiff(args[1:])
	case "budget":
		code, err = cmdBudget(args[1:])
	default:
		usage()
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if code == 0 {
			return 1
		}
	}
	return code
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: bsa <command> [args]
commands:
  analyze <manifest.json>                size attribution report
  estimate <file> [-alg a] [-level n]    transfer size estimation
  diff <base.json> <next.json>           incremental diff
  budget <manifest.json> <rules.json>    budget check (exit 2 on violation)`)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func loadManifest(path string) (*manifest.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return manifest.Parse(data)
}

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("analyze: want exactly one manifest path")
	}
	m, err := loadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	rep, err := attribution.Analyze(m)
	if err != nil {
		return err
	}
	return writeJSON(rep)
}

func cmdEstimate(args []string) error {
	fs := flag.NewFlagSet("estimate", flag.ContinueOnError)
	alg := fs.String("alg", "gzip", "compression algorithm: none|gzip|deflate")
	level := fs.Int("level", 0, "compression level 1..9, 0 = algorithm default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("estimate: want exactly one file path")
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	res, err := estimate.Estimate(f, estimate.Config{
		Algorithm: estimate.Algorithm(*alg),
		Level:     *level,
	})
	if err != nil {
		return err
	}
	return writeJSON(res)
}

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("diff: want base and next manifest paths")
	}
	base, err := loadManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	next, err := loadManifest(fs.Arg(1))
	if err != nil {
		return err
	}
	rep, err := diff.Compare(base, next)
	if err != nil {
		return err
	}
	return writeJSON(rep)
}

func cmdBudget(args []string) (int, error) {
	fs := flag.NewFlagSet("budget", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	if fs.NArg() != 2 {
		return 1, fmt.Errorf("budget: want manifest and rules paths")
	}
	m, err := loadManifest(fs.Arg(0))
	if err != nil {
		return 1, err
	}
	ruleData, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return 1, err
	}
	var rules []budget.Rule
	if err := json.Unmarshal(ruleData, &rules); err != nil {
		return 1, fmt.Errorf("budget: decode rules: %w", err)
	}
	rep, err := attribution.Analyze(m)
	if err != nil {
		return 1, err
	}
	res, err := budget.Evaluate(rep, rules)
	if err != nil {
		return 1, err
	}
	if err := writeJSON(res); err != nil {
		return 1, err
	}
	if !res.Passed {
		return 2, nil
	}
	return 0, nil
}
