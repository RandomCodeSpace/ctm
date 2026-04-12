package tmux

import (
	"reflect"
	"testing"
)

func TestBuildNewSessionArgs(t *testing.T) {
	args := buildNewSessionArgs("myproject", "/home/dev/projects", "/tmp/ctm.conf", "claude --session-id abc")
	expected := []string{"-f", "/tmp/ctm.conf", "new-session", "-d", "-s", "myproject", "-c", "/home/dev/projects", "claude --session-id abc"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("got %v, want %v", args, expected)
	}
}

func TestBuildNewSessionArgsEmptyConfPath(t *testing.T) {
	args := buildNewSessionArgs("myproject", "/home/dev/projects", "", "claude --session-id abc")
	expected := []string{"new-session", "-d", "-s", "myproject", "-c", "/home/dev/projects", "claude --session-id abc"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("got %v, want %v", args, expected)
	}
	for _, a := range args {
		if a == "-f" {
			t.Error("expected no -f flag when confPath is empty")
		}
	}
}

func TestBuildAttachArgs(t *testing.T) {
	args := buildAttachArgs("myproject", "/tmp/ctm.conf")
	expected := []string{"-f", "/tmp/ctm.conf", "attach-session", "-t", "myproject"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("got %v, want %v", args, expected)
	}
}

func TestBuildAttachArgsEmptyConfPath(t *testing.T) {
	args := buildAttachArgs("myproject", "")
	expected := []string{"attach-session", "-t", "myproject"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("got %v, want %v", args, expected)
	}
	for _, a := range args {
		if a == "-f" {
			t.Error("expected no -f flag when confPath is empty")
		}
	}
}

func TestBuildSwitchArgs(t *testing.T) {
	args := buildSwitchArgs("myproject")
	if args[0] != "switch-client" || args[2] != "myproject" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestBuildRespawnPaneArgs(t *testing.T) {
	shellCmd := "claude --resume abc-123 || claude --session-id abc-123"
	args := buildRespawnPaneArgs("myproject", shellCmd)
	expected := []string{"respawn-pane", "-t", "myproject", "-k", "/bin/sh", "-c", shellCmd}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("got %v, want %v", args, expected)
	}
}

func TestBuildRespawnPaneArgsShellCmdIsSingleArg(t *testing.T) {
	// Verify the || fallback is passed as one argument to /bin/sh -c, not split
	shellCmd := "claude --resume xyz || claude --session-id xyz"
	args := buildRespawnPaneArgs("sess", shellCmd)
	// args[6] should be the entire shellCmd as one string
	if args[6] != shellCmd {
		t.Errorf("shellCmd should be a single arg, got args: %v", args)
	}
}
