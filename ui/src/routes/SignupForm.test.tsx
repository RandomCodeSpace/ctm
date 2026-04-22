import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { SignupForm } from "@/routes/SignupForm";

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <MemoryRouter>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </MemoryRouter>
  );
}

function stubFetch(responses: Array<{ status: number; body: unknown }>) {
  let i = 0;
  const mock = vi.fn(async () => {
    const r = responses[i++] ?? responses[responses.length - 1];
    return new Response(JSON.stringify(r.body), {
      status: r.status,
      headers: { "content-type": "application/json" },
    });
  });
  globalThis.fetch = mock as unknown as typeof globalThis.fetch;
  return mock;
}

describe("SignupForm", () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => { originalFetch = globalThis.fetch; });
  afterEach(() => { globalThis.fetch = originalFetch; vi.restoreAllMocks(); });

  it("disables Create until username, password, confirm are all filled and match", async () => {
    render(wrap(<SignupForm />));
    const btn = screen.getByRole("button", { name: /create account/i });
    expect(btn).toBeDisabled();

    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    expect(btn).toBeDisabled();

    await userEvent.type(screen.getByLabelText(/confirm/i), "password321");
    expect(btn).toBeDisabled();

    const confirm = screen.getByLabelText(/confirm/i);
    await userEvent.clear(confirm);
    await userEvent.type(confirm, "password123");
    expect(btn).toBeEnabled();
  });

  it("POSTs to /api/auth/signup with username + password", async () => {
    const fetchMock = stubFetch([
      { status: 201, body: { token: "t", username: "alice@example.com" } },
    ]);
    render(wrap(<SignupForm />));
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    await userEvent.type(screen.getByLabelText(/confirm/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /create account/i }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const call = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(call[0]).toMatch(/\/api\/auth\/signup$/);
    expect(JSON.parse(String(call[1]?.body))).toEqual({
      username: "alice@example.com",
      password: "password123",
    });
  });

  it("shows already-registered error with a 'log in instead' link on 409", async () => {
    stubFetch([
      { status: 409, body: { error: "already_registered", message: "exists" } },
    ]);
    render(wrap(<SignupForm onSwitchToLogin={vi.fn()} />));
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    await userEvent.type(screen.getByLabelText(/confirm/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /create account/i }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/already/i);
    expect(screen.getByRole("button", { name: /log in instead/i })).toBeInTheDocument();
  });
});
