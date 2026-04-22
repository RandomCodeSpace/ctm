package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage the single-user credentials (V27)",
}

var authResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete the stored user credentials and force re-signup",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !auth.Exists() {
			fmt.Println("No credentials file present — nothing to reset.")
			return nil
		}
		fmt.Printf("About to delete %s\nContinue? [y/N] ", auth.UserPath())
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("Cancelled.")
			return nil
		}
		if err := auth.Delete(); err != nil {
			return err
		}
		fmt.Println("Credentials removed.")
		fmt.Println("The daemon will clear active sessions on its next incoming request.")
		fmt.Println("Visit the UI to sign up again.")
		return nil
	},
}

func init() {
	authCmd.AddCommand(authResetCmd)
	rootCmd.AddCommand(authCmd)
}
