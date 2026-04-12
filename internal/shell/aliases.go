package shell

import (
	"fmt"
	"os"
	"strings"
)

const startMarker = "# --- ctm aliases START ---"
const endMarker = "# --- ctm aliases END ---"

// AliasBlock returns the alias block to inject into shell rc files.
func AliasBlock() string {
	lines := []string{
		startMarker,
		"alias ct='ctm'",
		"alias ctl='ctm ls'",
		"alias ctn='ctm new'",
		"alias ctk='ctm kill'",
		"alias ctka='ctm killall'",
		"alias cts='ctm switch'",
		"alias cty='ctm yolo'",
		"alias ctyf='ctm yolo!'",
		"alias ctf='ctm safe'",
		"alias ctc='ctm check'",
		endMarker,
	}
	return strings.Join(lines, "\n") + "\n"
}

// InjectAliases adds the alias block to rcPath. Idempotent (replaces existing block).
func InjectAliases(rcPath string) error {
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read rc file: %w", err)
	}

	content := string(existing)
	content = removeBlock(content)
	if !strings.HasSuffix(content, "\n") && len(content) > 0 {
		content += "\n"
	}
	content += AliasBlock()

	if err := os.WriteFile(rcPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write rc file: %w", err)
	}
	return nil
}

// RemoveAliases removes the alias block from rcPath.
func RemoveAliases(rcPath string) error {
	data, err := os.ReadFile(rcPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read rc file: %w", err)
	}

	result := removeBlock(string(data))
	if err := os.WriteFile(rcPath, []byte(result), 0644); err != nil {
		return fmt.Errorf("write rc file: %w", err)
	}
	return nil
}

// removeBlock strips the ctm alias block (including markers) from content.
func removeBlock(content string) string {
	startIdx := strings.Index(content, startMarker)
	if startIdx == -1 {
		return content
	}
	endIdx := strings.Index(content, endMarker)
	if endIdx == -1 {
		return content
	}
	endIdx += len(endMarker)
	// consume trailing newline if present
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}
	return content[:startIdx] + content[endIdx:]
}
