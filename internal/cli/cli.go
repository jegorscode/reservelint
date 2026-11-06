// Package cli wires the cobra commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jegorscode/reservelint/internal/meta"
	"github.com/jegorscode/reservelint/internal/report"
	"github.com/jegorscode/reservelint/internal/runner"
	"github.com/jegorscode/reservelint/internal/synthetic"
	"github.com/spf13/cobra"
)

// ExitError asks main to exit with a specific status code.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

// NewRootCmd builds the command tree. out and errOut receive normal and error output.
func NewRootCmd(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           meta.Name,
		Short:         "Reserve data checks for payment stablecoin issuers. " + meta.Disclaimer,
		Long:          meta.Name + " checks issuer reserve data against rules in rules/rules.yaml.\n\n" + meta.Disclaimer,
		Version:       meta.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(newGenerateSyntheticCmd(), newCheckCmd())
	return root
}

func newGenerateSyntheticCmd() *cobra.Command {
	var (
		outDir string
		seed   int64
		format string
	)
	cmd := &cobra.Command{
		Use:   "generate-synthetic",
		Short: "Write synthetic sample data (a clean and a deliberately violating scenario)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, err := synthetic.Generate(outDir, seed, synthetic.Format(format))
			if err != nil {
				return &ExitError{Code: report.ExitInput, Msg: err.Error()}
			}
			for _, p := range paths {
				fmt.Fprintln(cmd.OutOrStdout(), p)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Synthetic data only. "+meta.Disclaimer)
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "data/synthetic", "output directory")
	cmd.Flags().Int64Var(&seed, "seed", 42, "seed; the same seed gives identical files")
	cmd.Flags().StringVar(&format, "format", "csv", "file format: csv or json")
	return cmd
}

func newCheckCmd() *cobra.Command {
	var (
		opts    runner.Options
		outDir  string
		asOfStr string
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check reserve and redemption data against the rules and write a report",
		Long: "Check reserve and redemption data against the rules and write report.md, findings.csv,\n" +
			"report.json and a new audit-<timestamp>.json into --out.\n\n" +
			"Exit codes: 0 pass or warning, 1 at least one FAIL finding, 2 input, parsing or usage error.\n\n" + meta.Disclaimer,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asOfStr != "" {
				t, err := time.Parse(time.RFC3339, asOfStr)
				if err != nil {
					return &ExitError{Code: report.ExitInput, Msg: fmt.Sprintf("--as-of must be an RFC 3339 timestamp with offset: %v", err)}
				}
				opts.AsOf = &t
			}
			rep, err := runner.Run(opts)
			if err != nil {
				return &ExitError{Code: report.ExitInput, Msg: err.Error()}
			}
			paths, err := report.Write(outDir, rep)
			if err != nil {
				return &ExitError{Code: report.ExitInput, Msg: err.Error()}
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s: %d fail, %d warning, %d pass\n", rep.Status, rep.Counts.Fail, rep.Counts.Warning, rep.Counts.Pass)
			if rep.Partial {
				fmt.Fprintln(out, "PARTIAL RUN: invalid rows were excluded.")
			}
			for _, p := range []string{paths.Markdown, paths.CSV, paths.JSON, paths.Audit} {
				fmt.Fprintln(out, p)
			}
			fmt.Fprintln(out, meta.Disclaimer)
			if rep.ExitCode != 0 {
				return &ExitError{Code: rep.ExitCode}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.RulesPath, "rules", "rules/rules.yaml", "rules file")
	f.StringVar(&opts.ReservesPath, "reserves", "", "reserves snapshot (.csv or .json)")
	f.StringVar(&opts.SupplyPath, "supply", "", "circulating supply (.csv or .json)")
	f.StringVar(&opts.RedemptionsPath, "redemptions", "", "redemption log (.csv or .json)")
	f.StringVar(&opts.HolidaysPath, "holidays", "", "holiday calendar; defaults to the rule's holiday_calendar")
	f.StringVar(&outDir, "out", "report", "report output directory")
	f.StringVar(&asOfStr, "as-of", "", "snapshot time (RFC 3339); defaults to the latest supply as_of")
	f.BoolVar(&opts.AllowInvalidRows, "allow-invalid-rows", false, "continue past invalid rows and report a PARTIAL run (debugging)")
	for _, name := range []string{"reserves", "supply", "redemptions"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	root := NewRootCmd(out, errOut)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		if exit.Msg != "" {
			fmt.Fprintln(errOut, "error:", exit.Msg)
		}
		return exit.Code
	}
	fmt.Fprintln(errOut, "error:", err) // usage errors from cobra
	return report.ExitInput
}
