import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DoctorPanel } from "@/routes/DoctorPanel";
import { TOKEN_KEY } from "@/lib/api";

/*
 * Renders the DoctorPanel route with a fresh QueryClient (no retries)
 * inside a MemoryRouter so <Link to="/"> doesn't throw. A bearer
 * token is pre-seeded so AuthProvider isn't on the tree (the route
 * itself doesn't require it — AuthProvider wraps the whole app in
 * <App />, not this route — but api() still injects it on fetch).
 */
function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <DoctorPanel />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("DoctorPanel", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.setItem(TOKEN_KEY, "test-token");
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    localStorage.clear();
  });

  it("renders three rows — one per status — and colour-codes the dots", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          checks: [
            { name: "dep:tmux", status: "ok", message: "/usr/bin/tmux" },
            {
              name: "env:PATH",
              status: "warn",
              message: "short",
              remediation: "fix your PATH",
            },
            {
              name: "serve:token",
              status: "err",
              message: "missing",
              remediation: "run ctm doctor",
            },
          ],
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      ),
    ) as unknown as typeof globalThis.fetch;

    renderPanel();

    await waitFor(() => {
      expect(screen.getByText("dep:tmux")).toBeInTheDocument();
    });
    expect(screen.getByText("env:PATH")).toBeInTheDocument();
    expect(screen.getByText("serve:token")).toBeInTheDocument();

    // Status dots are role=status with aria-label "check <status>".
    const okDot = screen.getByLabelText("check ok");
    const warnDot = screen.getByLabelText("check warn");
    const errDot = screen.getByLabelText("check err");
    expect(okDot).toHaveAttribute("data-status", "ok");
    expect(warnDot).toHaveAttribute("data-status", "warn");
    expect(errDot).toHaveAttribute("data-status", "err");
    // Colour classes — the key signal for the panel's spec.
    expect(okDot.className).toContain("bg-live-dot");
    expect(warnDot.className).toContain("bg-accent-gold");
    expect(errDot.className).toContain("bg-alert-ember");
  });

  it("expands a row with remediation when clicked and hides it on second click", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          checks: [
            {
              name: "env:PATH",
              status: "warn",
              message: "short",
              remediation: "export PATH=...",
            },
          ],
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      ),
    ) as unknown as typeof globalThis.fetch;

    const user = userEvent.setup();
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText("env:PATH")).toBeInTheDocument();
    });

    // Remediation not visible before expanding.
    expect(screen.queryByText("export PATH=...")).not.toBeInTheDocument();

    const toggle = screen.getByRole("button", {
      name: /env:PATH.*warn/i,
    });
    await user.click(toggle);
    expect(screen.getByText("export PATH=...")).toBeInTheDocument();

    await user.click(toggle);
    expect(screen.queryByText("export PATH=...")).not.toBeInTheDocument();
  });

  it("does not make rows without remediation expandable", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          checks: [
            { name: "dep:tmux", status: "ok", message: "/usr/bin/tmux" },
          ],
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      ),
    ) as unknown as typeof globalThis.fetch;

    renderPanel();

    await waitFor(() => {
      expect(screen.getByText("dep:tmux")).toBeInTheDocument();
    });
    // Button exists but is disabled — nothing to expand.
    const btn = screen.getByRole("button", {
      name: /dep:tmux.*ok/i,
    });
    expect(btn).toBeDisabled();
    expect(btn).not.toHaveAttribute("aria-expanded");
  });
});
