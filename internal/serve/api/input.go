package api

// V25 — session input: POST /api/sessions/{name}/input
// Spec: docs/superpowers/specs/2026-04-22-V25-session-input-design.md

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	SendEnter(target string) error
}

type inputReq struct {
	Text   string `json:"text,omitempty"`
	Preset string `json:"preset,omitempty"`
}

const inputTextMax = 256

// inputLogReject is the slog message used for every reject branch in
// the Input handler so structured-log consumers can grep one literal.
const inputLogReject = "input reject"

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
		name := r.PathValue("name")
		slog.Info("input request", "session", name, "origin", r.Header.Get("Origin"), "ua", r.Header.Get("User-Agent"))
		if r.Method != http.MethodPost {
			slog.Info(inputLogReject, "session", name, "reason", "method_not_allowed")
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		if name == "" {
			slog.Info(inputLogReject, "reason", "missing_name")
			writeInputErr(w, http.StatusBadRequest, "invalid_body", "missing session name")
			return
		}

		sess, ok := src.Get(name)
		if !ok {
			slog.Info(inputLogReject, "session", name, "reason", "session_not_found")
			writeInputErr(w, http.StatusNotFound, "session_not_found", "no session named "+name)
			return
		}
		if sess.Mode != "yolo" {
			slog.Info(inputLogReject, "session", name, "reason", "not_yolo", "mode", sess.Mode)
			writeInputErr(w, http.StatusForbidden, "not_yolo",
				"input is only available on yolo-mode sessions")
			return
		}
		if !src.TmuxAlive(name) {
			slog.Info(inputLogReject, "session", name, "reason", "tmux_dead")
			writeInputErr(w, http.StatusConflict, "tmux_dead",
				"session tmux has exited")
			return
		}

		var body inputReq
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			slog.Info(inputLogReject, "session", name, "reason", "invalid_body", "err", err.Error())
			writeInputErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}

		keys, herr := expandInput(body)
		if herr != nil {
			slog.Info(inputLogReject, "session", name, "reason", herr.Error())
			writeInputErr(w, http.StatusBadRequest, herr.Error(), herr.Error())
			return
		}

		target := fmt.Sprintf("%s:0.0", sess.Name)
		// `keys` always ends with "\n" by construction (see expandInput).
		// We split: literal text via -l, then a real tmux Enter key so
		// claude's TUI treats it as submit rather than "add newline".
		literal := strings.TrimRight(keys, "\n")
		if literal != "" {
			if err := tmux.SendKeys(target, literal); err != nil {
				slog.Error("input send_failed", "session", name, "err", err.Error())
				writeInputErr(w, http.StatusInternalServerError, "send_failed", err.Error())
				return
			}
		}
		if err := tmux.SendEnter(target); err != nil {
			slog.Error("input send_failed", "session", name, "err", err.Error())
			writeInputErr(w, http.StatusInternalServerError, "send_failed", err.Error())
			return
		}
		slog.Info("input ok", "session", name, "preset", body.Preset, "text_len", len(body.Text))
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
			return "Approve\n", nil
		case "no":
			return "Deny\n", nil
		case "continue":
			return "\n", nil
		case "follow":
			return "Follow recommended\n", nil
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
