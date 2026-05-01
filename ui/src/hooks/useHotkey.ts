import { useEffect } from "react";

/**
 * Tiny global-hotkey hook. Subscribes to `keydown` on the window and
 * fires `handler()` when any of the configured combinations match.
 *
 * `keys` accepts either:
 *   - "mod+k"   → Meta OR Ctrl + k (cross-platform)
 *   - "/"       → literal slash; **skipped when focus is inside an
 *                 editable field** (input, textarea, [contenteditable])
 *                 so the user can still type "/" while searching.
 *
 * Escape is intentionally NOT handled here — the Dialog component
 * already handles Esc via radix, and double-handling would fight.
 */
type Hotkey = "mod+k" | "/";

function isEditable(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  // `isContentEditable` is the canonical check, but JSDOM doesn't
  // always compute it from the attribute — fall back to the attribute
  // directly so tests and older browsers still behave correctly.
  if (el.isContentEditable) return true;
  const ce = el.getAttribute("contenteditable");
  if (ce !== null && ce !== "false") return true;
  return false;
}

export function useHotkey(keys: Hotkey[], handler: () => void): void {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      for (const key of keys) {
        if (key === "mod+k") {
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
            e.preventDefault();
            handler();
            return;
          }
        } else if (key === "/") {
          // Skip the slash-open shortcut when the user is typing in a
          // field — otherwise "/" could never be typed into a URL box.
          if (e.key === "/" && !isEditable(e.target)) {
            e.preventDefault();
            handler();
            return;
          }
        }
      }
    }
    globalThis.addEventListener("keydown", onKey);
    return () => globalThis.removeEventListener("keydown", onKey);
  }, [keys, handler]);
}
