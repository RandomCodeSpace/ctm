import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useHotkey } from "@/hooks/useHotkey";

function dispatchKey(
  key: string,
  init: KeyboardEventInit = {},
  target?: Element,
) {
  const event = new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
    ...init,
  });
  if (target) {
    target.dispatchEvent(event);
  } else {
    window.dispatchEvent(event);
  }
}

describe("useHotkey", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("fires on Cmd+K", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["mod+k"], handler));
    dispatchKey("k", { metaKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("fires on Ctrl+K (cross-platform)", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["mod+k"], handler));
    dispatchKey("k", { ctrlKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does not fire on a plain 'k'", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["mod+k"], handler));
    dispatchKey("k");
    expect(handler).not.toHaveBeenCalled();
  });

  it("fires on '/' when focus is outside editable fields", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["/"], handler));
    dispatchKey("/");
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does NOT fire on '/' when focus is inside an <input>", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["/"], handler));
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    dispatchKey("/", {}, input);
    expect(handler).not.toHaveBeenCalled();
  });

  it("does NOT fire on '/' when focus is inside a <textarea>", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["/"], handler));
    const ta = document.createElement("textarea");
    document.body.appendChild(ta);
    ta.focus();
    dispatchKey("/", {}, ta);
    expect(handler).not.toHaveBeenCalled();
  });

  it("does NOT fire on '/' when focus is on [contenteditable]", () => {
    const handler = vi.fn();
    renderHook(() => useHotkey(["/"], handler));
    const div = document.createElement("div");
    div.setAttribute("contenteditable", "true");
    document.body.appendChild(div);
    div.focus();
    dispatchKey("/", {}, div);
    expect(handler).not.toHaveBeenCalled();
  });

  it("unsubscribes on unmount", () => {
    const handler = vi.fn();
    const { unmount } = renderHook(() => useHotkey(["mod+k"], handler));
    unmount();
    dispatchKey("k", { metaKey: true });
    expect(handler).not.toHaveBeenCalled();
  });
});
