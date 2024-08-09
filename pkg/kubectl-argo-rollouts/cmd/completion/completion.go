package completion

import (
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/spf13/cobra"
)

func NewCmdCompletion(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script",
		Long: `Loading completion is a two step process:
	First you need to ensure that the kubectl-argo-rollouts can be completed, depending on your shell you can do this:
	
	Bash:
	
	  $ source <(yourprogram completion bash)
	
	  # To load completions for each session, execute once:
	  # Linux:
	  $ yourprogram completion bash > /etc/bash_completion.d/yourprogram
	  # macOS:
	  $ yourprogram completion bash > /usr/local/etc/bash_completion.d/yourprogram
	
	Zsh:
	
	  # If shell completion is not already enabled in your environment,
	  # you will need to enable it.  You can execute the following once:
	
	  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
	
	  # To load completions for each session, execute once:
	  $ yourprogram completion zsh > "${fpath[1]}/_yourprogram"
	
	  # You will need to start a new shell for this setup to take effect.
	
	fish:
	
	  $ yourprogram completion fish | source
	
	  # To load completions for each session, execute once:
	  $ yourprogram completion fish > ~/.config/fish/completions/yourprogram.fish
	
	PowerShell:
	
	  PS> yourprogram completion powershell | Out-String | Invoke-Expression
	
	  # To load completions for every new session, run:
	  PS> yourprogram completion powershell > yourprogram.ps1
	  # and source this file from your PowerShell profile.

	Secondly in order to support completion with kubectl you need to create a file in your path called: kubectl_complete-argo-rollouts with:
	Bash:
	  #!/usr/bin/env bash
	  args=("$@")
	  kubectl-argo-rollouts __complete "$@"
	`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactValidArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				cmd.Root().GenBashCompletionV2(o.Out, false)
			case "zsh":
				cmd.Root().GenZshCompletion(o.Out)
			case "fish":
				cmd.Root().GenFishCompletion(o.Out, true)
			case "powershell":
				cmd.Root().GenPowerShellCompletionWithDesc(o.Out)
			}
		},
	}
	return cmd
}
