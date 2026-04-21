import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { PaneView } from "./PaneView";

// Stub out the stream hook — we test PaneView's rendering, not the
// SSE plumbing (covered separately in usePaneStream's integration via
// the Go pane_test.go and the Playwright spec).
vi.mock("@/hooks/usePaneStream", () => ({
  usePaneStream: vi.fn(() => ({
    text: "hello pane",
    connected: true,
    ended: false,
  })),
}));

describe("PaneView", () => {
  it("renders plaintext capture into the pre block", () => {
    render(<PaneView sessionName="alpha" />);
    const pre = screen.getByTestId("pane-view");
    expect(pre).toBeInTheDocument();
    expect(pre.innerHTML).toBe("hello pane");
  });

  it("labels its region with the session name", () => {
    render(<PaneView sessionName="alpha" />);
    expect(
      screen.getByRole("region", { name: /live pane for alpha/i }),
    ).toBeInTheDocument();
  });

  it("shows a Live indicator when connected", () => {
    render(<PaneView sessionName="alpha" />);
    expect(screen.getByText(/^live$/i)).toBeInTheDocument();
  });
});
