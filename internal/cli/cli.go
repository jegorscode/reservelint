// Package cli wires the cobra commands.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/jegorscode/reservelint/internal/meta"
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
				return err
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
	return &cobra.Command{
		Use:   "check",
		Short: "Run checks and write a report (not implemented yet)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return &ExitError{Code: 2, Msg: "check is not implemented yet"}
		},
	}
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, out, errOut io.Writer) int {
	root := NewRootCmd(out, errOut)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	fmt.Fprintln(errOut, "error:", err)
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}
