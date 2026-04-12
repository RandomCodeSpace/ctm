package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
)

func init() {
	overlayCmd.AddCommand(overlayInitCmd)
	overlayCmd.AddCommand(overlayEditCmd)
	overlayCmd.AddCommand(overlayPathCmd)
	rootCmd.AddCommand(overlayCmd)
}

var overlayCmd = &cobra.Command{
	Use:   "overlay",
	Short: "Manage the ctm-only claude settings overlay",
	Long: "The overlay file at ~/.config/ctm/claude-overlay.json contains claude " +
		"settings (statusline, theme, etc.) that apply ONLY when claude is launched " +
		"via ctm. Direct `claude` invocations are unaffected.",
	RunE: runOverlayStatus,
}

var overlayInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a sample overlay file with statusline + theme examples",
	RunE:  runOverlayInit,
}

var overlayEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the overlay file in $EDITOR (creates it first if missing)",
	RunE:  runOverlayEdit,
}

var overlayPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the overlay file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(config.ClaudeOverlayPath())
		return nil
	},
}

// sampleOverlay is the JSON we write on `ctm overlay init`. It deliberately
// uses commonly-supported claude settings keys: statusLine and theme.
const sampleOverlay = `{
  "statusLine": {
    "type": "command",
    "command": "echo \"[ctm] $(basename $PWD)\""
  },
  "theme": "dark"
}
`

func runOverlayStatus(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	if _, err := os.Stat(path); err == nil {
		out.Success("overlay active: %s", path)
	} else {
		out.Dim("no overlay file at %s", path)
		out.Dim("create one with: ctm overlay init")
	}
	return nil
}

func runOverlayInit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// O_EXCL is atomic against concurrent creators — no TOCTOU race.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("overlay already exists at %s — delete it first or use `ctm overlay edit`", path)
		}
		return fmt.Errorf("creating overlay: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(sampleOverlay); err != nil {
		return fmt.Errorf("writing overlay: %w", err)
	}

	out.Success("created %s", path)
	out.Dim("edit with: ctm overlay edit")
	out.Dim("applies to all NEW ctm sessions; restart existing ones to pick up changes")
	return nil
}

func runOverlayEdit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()

	// Resolve editor BEFORE touching the filesystem so a missing $EDITOR
	// doesn't leave a half-created sample file behind.
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editorBin, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("editor %q not found in PATH — set $EDITOR or install it; overlay path: %s", editor, path)
	}

	// Create with sample if missing, atomically.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("creating overlay: %w", err)
		}
		if err == nil {
			if _, werr := f.WriteString(sampleOverlay); werr != nil {
				f.Close()
				return fmt.Errorf("writing overlay: %w", werr)
			}
			f.Close()
			out.Dim("created sample overlay at %s", path)
		}
	}

	c := exec.Command(editorBin, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
