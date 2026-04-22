package api

// V25 — session input: POST /api/sessions/{name}/input
// Spec: docs/superpowers/specs/2026-04-22-V25-session-input-design.md

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// InputSessionSource is the narrow slice of the sessions projection
// the Input handler needs. Get returns the snapshot; TmuxAlive is
// sourced from the attention engine / live reconcile layer (see
// internal/serve/attention/engine.go SessionSource for prior art).
type InputSessionSource interface {
	Get(name string) (session.Session, bool)
	TmuxAlive(name string) bool
}

// InputTmux is the narrow slice of *tmux.Client the Input handler needs.
type InputTmux interface {
	SendKeys(target, keys string) error
}

type inputReq struct {
	Text   string `json:"text,omitempty"`
	Preset string `json:"preset,omitempty"`
}

const inputTextMax = 256

var (
	errInputBothFields = errors.New("invalid_body")
	errInputEmpty      = errors.New("invalid_body")
	errInputText       = errors.New("invalid_text")
	errInputPreset     = errors.New("invalid_preset")
)

// Input returns POST /api/sessions/{name}/input. Gated on
// mode=yolo + tmux_alive; refuses otherwise with a structured error.
func Input(src InputSessionSource, tmux InputTmux) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		name := r.PathValue("name")
		if name == "" {
			writeInputErr(w, http.StatusBadRequest, "invalid_body", "missing session name")
			return
		}

		sess, ok := src.Get(name)
		if !ok {
			writeInputErr(w, http.StatusNotFound, "session_not_found", "no session named "+name)
			return
		}
		if sess.Mode != "yolo" {
			writeInputErr(w, http.StatusForbidden, "not_yolo",
				"input is only available on yolo-mode sessions")
			return
		}
		if !src.TmuxAlive(name) {
			writeInputErr(w, http.StatusConflict, "tmux_dead",
				"session tmux has exited")
			return
		}

		var body inputReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeInputErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		keys, herr := expandInput(body)
		if herr != nil {
			writeInputErr(w, http.StatusBadRequest, herr.Error(), herr.Error())
			return
		}

		target := fmt.Sprintf("%s:0.0", sess.Name)
		if err := tmux.SendKeys(target, keys); err != nil {
			writeInputErr(w, http.StatusInternalServerError, "send_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// expandInput validates the body and returns the bytes to send.
// A trailing \n is always appended — the handler enforces
// "one line per POST" as a hard invariant.
func expandInput(b inputReq) (string, error) {
	hasText := b.Text != ""
	hasPreset := b.Preset != ""
	if hasText && hasPreset {
		return "", errInputBothFields
	}
	if !hasText && !hasPreset {
		return "", errInputEmpty
	}
	if hasPreset {
		switch b.Preset {
		case "yes":
			return "y\n", nil
		case "no":
			return "n\n", nil
		case "continue":
			return "\n", nil
		default:
			return "", errInputPreset
		}
	}
	if len(b.Text) > inputTextMax {
		return "", errInputText
	}
	if strings.TrimSpace(b.Text) == "" {
		return "", errInputText
	}
	if strings.ContainsAny(b.Text, "\n\r") {
		return "", errInputText
	}
	for _, r := range b.Text {
		if r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return "", errInputText
		}
	}
	return b.Text + "\n", nil
}

// writeInputErr writes a structured JSON error: {"error":"<code>", "message":"..."}.
// Named distinctly to avoid collision with writeJSONError in revert.go.
func writeInputErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
