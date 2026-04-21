/**
 * Tiny ANSI → HTML converter for V24 (live tmux pane capture).
 *
 * Scope: SGR codes only. No cursor movement, no clear-screen, no OSC.
 * tmux's `capture-pane -e` already flattens the pane into a linear
 * stream of text + SGR, so we don't need a full terminal emulator —
 * we just colourise spans.
 *
 * Supported SGR codes:
 *   0                — reset
 *   1                — bold
 *   30–37, 90–97     — fg colour (standard + bright)
 *   40–47, 100–107   — bg colour (standard + bright)
 *
 * All other codes are ignored (passed over without changing state).
 * This is intentional — it keeps the converter small and resilient
 * to programs that emit fancier sequences without breaking layout.
 *
 * Output: open/close `<span class="ansi-…">` pairs around runs of
 * text. HTML-escaped before wrapping so any `<`, `>`, `&` in pane
 * output can never break out of the span.
 *
 * ~80 LOC target. No deps.
 */

const COLOUR_NAMES: Record<number, string> = {
  30: "black",
  31: "red",
  32: "green",
  33: "yellow",
  34: "blue",
  35: "magenta",
  36: "cyan",
  37: "white",
  // Bright variants use the same class name — the CSS maps both
  // flavours coarsely onto the theme tokens.
  90: "bright-black",
  91: "bright-red",
  92: "bright-green",
  93: "bright-yellow",
  94: "bright-blue",
  95: "bright-magenta",
  96: "bright-cyan",
  97: "bright-white",
};

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

interface State {
  fg: string | null;
  bg: string | null;
  bold: boolean;
}

function classesFor(s: State): string[] {
  const cls: string[] = [];
  if (s.fg) cls.push(`ansi-fg-${s.fg}`);
  if (s.bg) cls.push(`ansi-bg-${s.bg}`);
  if (s.bold) cls.push("ansi-bold");
  return cls;
}

function applySGR(state: State, codes: number[]): State {
  const next: State = { ...state };
  if (codes.length === 0) codes = [0];
  for (const c of codes) {
    if (c === 0) {
      next.fg = null;
      next.bg = null;
      next.bold = false;
    } else if (c === 1) {
      next.bold = true;
    } else if (c === 22) {
      next.bold = false;
    } else if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97)) {
      next.fg = COLOUR_NAMES[c];
    } else if (c === 39) {
      next.fg = null;
    } else if ((c >= 40 && c <= 47) || (c >= 100 && c <= 107)) {
      // Map bg code to the same palette label.
      const fgEq = c - 10;
      next.bg = COLOUR_NAMES[fgEq];
    } else if (c === 49) {
      next.bg = null;
    }
    // Ignore everything else.
  }
  return next;
}

/**
 * Convert raw text containing CSI SGR escape sequences to HTML.
 * Safe to inject via dangerouslySetInnerHTML — all user content is
 * HTML-escaped before any `<span>` wrapping.
 */
export function ansiToHtml(input: string): string {
  // ESC [ <params> m  — capture params as group 1.
  // eslint-disable-next-line no-control-regex
  const csi = /\x1b\[([\d;]*)m/g;

  let state: State = { fg: null, bg: null, bold: false };
  let out = "";
  let openSpan = false;
  let cursor = 0;
  let m: RegExpExecArray | null;

  const openIfNeeded = () => {
    const cls = classesFor(state);
    if (cls.length > 0) {
      out += `<span class="${cls.join(" ")}">`;
      openSpan = true;
    }
  };
  const closeIfNeeded = () => {
    if (openSpan) {
      out += "</span>";
      openSpan = false;
    }
  };

  openIfNeeded();
  while ((m = csi.exec(input)) !== null) {
    const chunk = input.slice(cursor, m.index);
    if (chunk) out += escapeHtml(chunk);
    cursor = m.index + m[0].length;
    const codes = m[1]
      .split(";")
      .filter((p) => p.length > 0)
      .map((p) => parseInt(p, 10))
      .filter((n) => Number.isFinite(n));
    closeIfNeeded();
    state = applySGR(state, codes);
    openIfNeeded();
  }
  const tail = input.slice(cursor);
  if (tail) out += escapeHtml(tail);
  closeIfNeeded();
  return out;
}
