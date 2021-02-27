package make

import (
	"context"
	"fmt"
	"os"

	"github.com/argoproj/argo-rollouts/server"
	"github.com/spf13/cobra"
)

const (
	// CLIName is the name of the CLI
	cliName = "argo-rollouts-server"
)

func newCommand() *cobra.Command {
	var (
		listenPort int
	)

	var command = &cobra.Command{
		Use:   cliName,
		Short: "argo-rollouts-server is an API server that provides UI assets and Rollout data",
		Run: func(c *cobra.Command, args []string) {
			for {
				ctx := context.Background()
				ctx, cancel := context.WithCancel(ctx)
				argorollouts := server.NewServer(ctx)
				argorollouts.Run(ctx, listenPort)
				cancel()
			}
		},
	}
	command.Flags().IntVar(&listenPort, "port", 3100, "Listen on given port")
	return command;
}

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}