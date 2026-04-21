import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/components/AuthProvider";
import { TOKEN_KEY } from "@/lib/api";

function renderWithAuth() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        {/* AuthProvider mounts <TokenPasteScreen> when no token is set. */}
        <div>protected content</div>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

describe("TokenPasteScreen (via AuthProvider)", () => {
  let originalFetch: typeof globalThis.fetch;
  let originalLocation: Location;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    originalLocation = window.location;
    localStorage.clear();
    // Stub location so `replace` doesn't actually navigate the JSDOM env.
    Object.defineProperty(window, "location", {
      writable: true,
      value: {
        pathname: "/auth",
        search: "?next=%2Fs%2Fclaude",
        replace: vi.fn(),
      } as unknown as Location,
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    Object.defineProperty(window, "location", {
      writable: true,
      value: originalLocation,
    });
    localStorage.clear();
  });

  it("rejects an invalid token (401) and surfaces an inline error", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response("unauthorized", { status: 401 }),
    ) as unknown as typeof globalThis.fetch;

    const user = userEvent.setup();
    renderWithAuth();

    // Form is mounted because no token in localStorage.
    expect(screen.queryByText(/protected content/i)).toBeNull();
    const textarea = screen.getByPlaceholderText(/paste here/i);
    await user.type(textarea, "bad-token");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent(/invalid token/i);
    });
    // Token is NOT persisted on failure.
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
  });

  it("persists a valid token (200) and would redirect to ?next=", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({ version: "test", port: 37778, has_webhook: false }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    ) as unknown as typeof globalThis.fetch;

    const user = userEvent.setup();
    renderWithAuth();

    const textarea = screen.getByPlaceholderText(/paste here/i);
    await user.type(textarea, "good-token");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(localStorage.getItem(TOKEN_KEY)).toBe("good-token");
    });
    expect(window.location.replace).toHaveBeenCalledWith("/s/claude");
  });

  it("does not submit when the textarea is empty (button stays disabled)", () => {
    renderWithAuth();
    const btn = screen.getByRole("button", { name: /continue/i });
    expect(btn).toBeDisabled();
  });
});
