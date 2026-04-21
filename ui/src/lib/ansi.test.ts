import { describe, it, expect } from "vitest";
import { ansiToHtml } from "./ansi";

/** Helper: strip HTML-escaped entities for assertion convenience. */
function stripTags(html: string): string {
  return html.replace(/<[^>]+>/g, "");
}

describe("ansiToHtml", () => {
  it("passes plain text through unchanged (no wrapping span)", () => {
    const out = ansiToHtml("hello world");
    expect(out).toBe("hello world");
  });

  it("escapes HTML entities", () => {
    const out = ansiToHtml("<script>a & b > c</script>");
    expect(out).toBe("&lt;script&gt;a &amp; b &gt; c&lt;/script&gt;");
  });

  it("wraps red foreground in ansi-fg-red span", () => {
    const input = `\x1b[31mhello\x1b[0m`;
    const out = ansiToHtml(input);
    expect(out).toContain(`<span class="ansi-fg-red">hello</span>`);
    // Reset closes the span — nothing open at the tail.
    expect(out.endsWith("</span>")).toBe(true);
  });

  it("handles green fg + bold combination", () => {
    const input = `\x1b[1;32mOK\x1b[0m`;
    const out = ansiToHtml(input);
    // Both classes on the same span.
    expect(out).toMatch(
      /<span class="(ansi-fg-green ansi-bold|ansi-bold ansi-fg-green)">OK<\/span>/,
    );
  });

  it("handles bg colours (44 → blue)", () => {
    const input = `\x1b[44mbg\x1b[0m`;
    const out = ansiToHtml(input);
    expect(out).toContain(`<span class="ansi-bg-blue">bg</span>`);
  });

  it("renders SGR sequences split across multiple lines", () => {
    const input = `\x1b[31mred line 1\nred line 2\x1b[0m\nplain`;
    const out = ansiToHtml(input);
    expect(out).toContain(
      `<span class="ansi-fg-red">red line 1\nred line 2</span>`,
    );
    expect(out.endsWith("\nplain")).toBe(true);
  });

  it("reset clears state and leaves subsequent text unwrapped", () => {
    const input = `\x1b[33myellow\x1b[0m plain`;
    const out = ansiToHtml(input);
    expect(out).toContain(`<span class="ansi-fg-yellow">yellow</span>`);
    // The trailing space + plain should be outside any span.
    expect(out.endsWith(" plain")).toBe(true);
  });

  it("ignores unsupported SGR codes without breaking text flow", () => {
    // 4 (underline) is not in our supported set — we should still
    // render the text, just without adding a class.
    const input = `\x1b[4munderline?\x1b[0m`;
    expect(stripTags(ansiToHtml(input))).toBe("underline?");
  });

  it("39 resets fg, 49 resets bg", () => {
    const input = `\x1b[31;44mA\x1b[39mB\x1b[49mC`;
    const out = ansiToHtml(input);
    // A has both fg+bg, B only bg, C nothing.
    expect(out).toContain(">A</span>");
    expect(out).toContain(">B</span>");
    expect(out.endsWith("C")).toBe(true);
  });

  it("empty input returns empty string", () => {
    expect(ansiToHtml("")).toBe("");
  });

  it("bright colours use the bright class names", () => {
    const input = `\x1b[91mbright red\x1b[0m`;
    const out = ansiToHtml(input);
    expect(out).toContain(`ansi-fg-bright-red`);
  });
});
