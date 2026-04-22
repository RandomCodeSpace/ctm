import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/components/AuthProvider";
import { SettingsDrawer } from "@/components/SettingsDrawer";
import type { ConfigPayload } from "@/hooks/useConfigUpdate";

const seeded: ConfigPayload = {
  webhook_url: "https://old.example",
  webhook_auth: "Bearer old",
  attention: {
    error_rate_pct: 20,
    error_rate_window: 30,
    idle_minutes: 5,
    quota_pct: 85,
    context_pct: 90,
    yolo_unchecked_minutes: 30,
  },
};

/**
 * Wraps the drawer in a fresh QueryClient per test. retry:false keeps
 * a failed fetch from silently re-triggering between assertions —
 * vitest fake timers don't drive TanStack's internal retry scheduler
 * reliably, and we only exercise single fetches here.
 */
function renderDrawer(fetchImpl: typeof globalThis.fetch) {
  globalThis.fetch = fetchImpl;
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onClose = vi.fn();
  return {
    onClose,
    ...render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <SettingsDrawer open onClose={onClose} />
        </AuthProvider>
      </QueryClientProvider>,
    ),
  };
}

describe("SettingsDrawer", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders the form seeded with GET /api/config values", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/api/config")) {
        return new Response(JSON.stringify(seeded), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderDrawer(fetchMock as unknown as typeof globalThis.fetch);

    // URL input populated.
    const urlInput = await screen.findByLabelText(/^webhook url$/i);
    expect(urlInput).toHaveValue("https://old.example");

    // A sample threshold populated.
    const quotaInput = screen.getByLabelText(/^quota %$/i);
    expect(quotaInput).toHaveValue(85);
  });

  it("submit PATCHes the form and shows the restarting banner", async () => {
    const calls: Array<{ method?: string; body?: unknown }> = [];
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.endsWith("/api/config") && (!init?.method || init.method === "GET")) {
          return new Response(JSON.stringify(seeded), {
            status: 200,
            headers: { "content-type": "application/json" },
          });
        }
        if (url.endsWith("/api/config") && init?.method === "PATCH") {
          calls.push({
            method: init.method,
            body: JSON.parse(String(init.body)),
          });
          return new Response(JSON.stringify({ status: "restarting" }), {
            status: 202,
            headers: { "content-type": "application/json" },
          });
        }
        throw new Error(`unexpected fetch: ${url}`);
      },
    );

    const user = userEvent.setup();
    renderDrawer(fetchMock as unknown as typeof globalThis.fetch);

    // Wait for seed.
    const urlInput = await screen.findByLabelText(/^webhook url$/i);
    await user.clear(urlInput);
    await user.type(urlInput, "https://new.example");

    await user.click(screen.getByRole("button", { name: /save & restart/i }));

    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.method).toBe("PATCH");
    const body = calls[0]?.body as ConfigPayload;
    expect(body.webhook_url).toBe("https://new.example");
    expect(body.attention.quota_pct).toBe(85);

    // Restarting banner surfaces.
    await waitFor(() => {
      expect(screen.getByText(/daemon restarting/i)).toBeInTheDocument();
    });
  });

  it("disables submit and surfaces an error when a threshold is invalid", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/api/config")) {
        return new Response(JSON.stringify(seeded), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    const user = userEvent.setup();
    renderDrawer(fetchMock as unknown as typeof globalThis.fetch);

    const quotaInput = await screen.findByLabelText(/^quota %$/i);
    await user.clear(quotaInput);
    await user.type(quotaInput, "150");

    // Inline validation error.
    expect(
      await screen.findByText(/quota % must be <= 100/i),
    ).toBeInTheDocument();

    // Submit button disabled.
    const submit = screen.getByRole("button", { name: /save & restart/i });
    expect(submit).toBeDisabled();
  });

  it("invokes onClose when the Close button fires", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify(seeded), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    );
    const user = userEvent.setup();
    const { onClose } = renderDrawer(
      fetchMock as unknown as typeof globalThis.fetch,
    );
    await screen.findByLabelText(/^webhook url$/i);
    // Two "Close" buttons exist: the sr-only X that radix Sheet
    // auto-injects, plus the explicit footer button we rendered. Both
    // trigger onClose; we click the last one (the footer) so the
    // assertion targets the button we wrote.
    const closeButtons = screen.getAllByRole("button", { name: /^close$/i });
    await user.click(closeButtons[closeButtons.length - 1]);
    expect(onClose).toHaveBeenCalled();
  });
});
