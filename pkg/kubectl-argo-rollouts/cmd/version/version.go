package version

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	versionutils "github.com/argoproj/argo-rollouts/utils/version"
)

// NewCmdVersion returns a new instance of an `rollouts version` command
func NewCmdVersion(o *options.ArgoRolloutsOptions) *cobra.Command {
	var (
		short bool
	)
	var cmd = &cobra.Command{
		Use:          "version",
		Short:        "Print version",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			PrintVersion(o.Out, short)
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "print just the version number")
	return cmd
}

// PrintVersion prints the version to the output stream
func PrintVersion(out io.Writer, short bool) {
	version := versionutils.GetVersion()
	fmt.Fprintf(out, "%s: %s\n", "kubectl-argo-rollouts", version)
	if !short {
		fmt.Fprintf(out, "  BuildDate: %s\n", version.BuildDate)
		fmt.Fprintf(out, "  GitCommit: %s\n", version.GitCommit)
		fmt.Fprintf(out, "  GitTreeState: %s\n", version.GitTreeState)
		fmt.Fprintf(out, "  GoVersion: %s\n", version.GoVersion)
		fmt.Fprintf(out, "  Compiler: %s\n", version.Compiler)
		fmt.Fprintf(out, "  Platform: %s\n", version.Platform)
	}
}
