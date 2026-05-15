package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tmuxDisplayMessage is the tmux subcommand used by every helper that
// reads a client- or session-scoped variable via `-p <fmt>`.
const tmuxDisplayMessage = "display-message"

// IsInsideTmux returns true if the current process is running inside a tmux session.
func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// Client wraps tmux CLI commands.
type Client struct {
	confPath string
	// execCommand lets tests inject a fake exec. Defaults to
	// exec.Command when nil, so callers outside tests don't need to
	// set it.
	execCommand func(name string, args ...string) *exec.Cmd
}

func (c *Client) cmd(name string, args ...string) *exec.Cmd {
	if c.execCommand != nil {
		return c.execCommand(name, args...)
	}
	return exec.Command(name, args...)
}

// NewClient creates a new Client using the given tmux config path.
func NewClient(confPath string) *Client {
	return &Client{confPath: confPath}
}

// HasSession returns true if a tmux session with the given name exists.
func (c *Client) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// SourceConfig loads the tmux config into the running server.
// This is needed because tmux -f only applies when starting a new server.
func (c *Client) SourceConfig() error {
	if c.confPath == "" {
		return nil
	}
	return exec.Command("tmux", "source-file", c.confPath).Run()
}

// NewSession creates a new detached tmux session.
func (c *Client) NewSession(name, workdir, shellCmd string) error {
	args := buildNewSessionArgs(name, workdir, c.confPath, shellCmd)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return err
	}
	// Source config into running server so mouse/scroll settings apply
	return c.SourceConfig()
}

// Attach attaches to an existing tmux session, connecting stdin/stdout/stderr.
func (c *Client) Attach(name string) error {
	args := buildAttachArgs(name, c.confPath)
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwitchClient switches the tmux client to the named session.
func (c *Client) SwitchClient(name string) error {
	args := buildSwitchArgs(name)
	return exec.Command("tmux", args...).Run()
}

// Go navigates to the named session, switching if inside tmux or attaching otherwise.
func (c *Client) Go(name string) error {
	if IsInsideTmux() {
		tty := clientTTY()
		setTitle(tty, name)
		return c.SwitchClient(name)
	}
	setTitleStdout(name)
	return c.Attach(name)
}

// KillSession kills the named tmux session.
func (c *Client) KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

// KillServer kills the tmux server. Returns nil if there is no server running.
func (c *Client) KillServer() error {
	out, err := exec.Command("tmux", "kill-server").CombinedOutput()
	if err != nil {
		// "no server running" is not an error for our purposes
		if strings.Contains(string(out), "no server") {
			return nil
		}
		return fmt.Errorf("tmux kill-server: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ListSessions returns a newline-separated list of sessions, or empty string if none.
func (c *Client) ListSessions() (string, error) {
	out, err := exec.Command("tmux", "list-sessions").Output()
	if err != nil {
		// tmux exits non-zero when there are no sessions; treat as empty
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// PaneCommand returns the start command of the first pane in the named session.
func (c *Client) PaneCommand(name string) (string, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", name, "-F", "#{pane_start_command}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PanePID returns the PID of the first pane in the named session.
func (c *Client) PanePID(name string) (string, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", name, "-F", "#{pane_pid}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CapturePane returns the current visible contents of the pane in the
// named session. `-e` preserves escape sequences (colour) so the
// consumer can render ANSI; `-p` prints to stdout instead of a buffer.
// Follows the same CLI-shell-out style as PaneCommand / PanePID.
//
// Visible-only: callers that need pane readiness (see pane_ready.go)
// must compare the current screen, not scrollback. For the live pane
// viewer, use CapturePaneHistory with a non-zero scrollback.
func (c *Client) CapturePane(name string) (string, error) {
	args := []string{}
	if c.confPath != "" {
		args = append(args, "-f", c.confPath)
	}
	args = append(args, "capture-pane", "-e", "-p", "-t", name)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneHistory returns the contents of the pane in the named
// session including up to `scrollback` lines of tmux history above
// the currently visible screen. `-e` preserves SGR escapes, `-p`
// prints to stdout, `-J` joins wrapped lines so the consumer sees
// logical lines instead of terminal-width fragments.
//
// scrollback <= 0 degrades to the visible-only behaviour of
// CapturePane. Callers that want the entire scrollback can pass a
// large bound (the tmux history-limit is ~2000 by default and can
// be raised per session).
func (c *Client) CapturePaneHistory(name string, scrollback int) (string, error) {
	args := []string{}
	if c.confPath != "" {
		args = append(args, "-f", c.confPath)
	}
	args = append(args, "capture-pane", "-e", "-p", "-J", "-t", name)
	if scrollback > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", scrollback))
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// RespawnPane respawns the pane in the named session with the given shell command.
// The command is wrapped in /bin/sh -c so shell operators (||, &&) are interpreted.
// Unlike new-session, respawn-pane does not invoke $SHELL -c automatically.
func (c *Client) RespawnPane(name, shellCmd string) error {
	args := buildRespawnPaneArgs(name, shellCmd)
	return exec.Command("tmux", args...).Run()
}

// CurrentSession returns the name of the current tmux session.
func (c *Client) CurrentSession() (string, error) {
	out, err := exec.Command("tmux", tmuxDisplayMessage, "-p", "#S").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PaneCurrentPath returns the current working directory of the pane in the named session.
func (c *Client) PaneCurrentPath(name string) (string, error) {
	out, err := exec.Command("tmux", tmuxDisplayMessage, "-t", name, "-p", "#{pane_current_path}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Detach detaches the current tmux client.
func (c *Client) Detach() error {
	return exec.Command("tmux", "detach-client").Run()
}

// RenameSession renames a tmux session.
func (c *Client) RenameSession(oldName, newName string) error {
	return exec.Command("tmux", "rename-session", "-t", oldName, newName).Run()
}

// ChooseSession opens the tmux session chooser, connecting stdin/stdout/stderr.
func (c *Client) ChooseSession() error {
	cmd := exec.Command("tmux", "choose-session")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SendKeys pipes keys to the given tmux target. The `-l` flag asks
// tmux to treat the text as a literal string (no keybinding
// translation), so `y\n` sends 'y' then newline, not the VI-mode key
// "Enter". Callers are responsible for appending a trailing `\n`
// when they want the remote shell / REPL to receive it as submission.
//
// target format: "<session>:<window>.<pane>" — e.g. "alpha:0.0".
// Empty target is rejected to guard against a caller bug that would
// otherwise default to tmux's "current client" and type into the
// wrong place.
func (c *Client) SendKeys(target, keys string) error {
	if target == "" {
		return fmt.Errorf("tmux send-keys: empty target")
	}
	out, err := c.cmd("tmux", "send-keys", "-t", target, "-l", keys).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys -t %q: %w: %s", target, err, string(out))
	}
	return nil
}

// SendEnter sends the tmux "Enter" key (without -l) to the given
// target. A literal `\n` via SendKeys is the LF character, which
// TUIs like codex interpret as "insert newline", not "submit".
// Sending the Enter keybind triggers the real submit path.
func (c *Client) SendEnter(target string) error {
	if target == "" {
		return fmt.Errorf("tmux send-keys Enter: empty target")
	}
	out, err := c.cmd("tmux", "send-keys", "-t", target, "Enter").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys Enter -t %q: %w: %s", target, err, string(out))
	}
	return nil
}

// --- unexported helpers ---

// buildNewSessionArgs constructs args for `tmux new-session`.
// shellCmd is passed as a bare positional arg; tmux new-session passes it to
// $SHELL -c internally, so shell operators (||, &&) work without explicit
// wrapping. Note: tmux respawn-pane does NOT do this — see RespawnPane.
// The UUID used in shellCmd is hex+hyphens only, so there is no metacharacter risk.
func buildNewSessionArgs(name, workdir, confPath, shellCmd string) []string {
	args := []string{}
	if confPath != "" {
		args = append(args, "-f", confPath)
	}
	args = append(args, "new-session", "-d", "-s", name, "-c", workdir, shellCmd)
	return args
}

func buildAttachArgs(name, confPath string) []string {
	args := []string{}
	if confPath != "" {
		args = append(args, "-f", confPath)
	}
	args = append(args, "attach-session", "-t", name)
	return args
}

func buildSwitchArgs(name string) []string {
	return []string{"switch-client", "-t", name}
}

func buildRespawnPaneArgs(name, shellCmd string) []string {
	return []string{"respawn-pane", "-t", name, "-k", "/bin/sh", "-c", shellCmd}
}

// clientTTY returns the tty of the current tmux client.
func clientTTY() string {
	out, err := exec.Command("tmux", tmuxDisplayMessage, "-p", "#{client_tty}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// setTitle writes an OSC title escape sequence to the given tty file path.
func setTitle(tty, name string) {
	if tty == "" {
		return
	}
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\033]0;%s\007", name)
}

// setTitleStdout writes an OSC title escape sequence to stdout.
func setTitleStdout(name string) {
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", name)
}
