package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(completionCmd)
}

var completionCmd = &cobra.Command{
	Use:                   "completion [bash|zsh|fish|powershell]",
	Short:                 "Generate shell completion scripts",
	Long: `Emit a shell completion script for ctm on stdout. Source the output from
your shell's rc file (or write it to the shell's completion directory) to
enable tab-completion for subcommands, session names, and flags.

Examples — one-liners by shell:

  bash       source <(ctm completion bash)
  bash (rc)  ctm completion bash > /etc/bash_completion.d/ctm
  zsh        ctm completion zsh > "${fpath[1]}/_ctm"
  fish       ctm completion fish > ~/.config/fish/completions/ctm.fish
  powershell ctm completion powershell | Out-String | Invoke-Expression

Completion for dynamic values (e.g. session names for 'ctm attach') is
driven by the live ctm state file at ~/.config/ctm/sessions.json.`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletionV2(out, true)
		case "zsh":
			return rootCmd.GenZshCompletion(out)
		case "fish":
			return rootCmd.GenFishCompletion(out, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(out)
		}
		return fmt.Errorf("unsupported shell %q", args[0])
	},
}
