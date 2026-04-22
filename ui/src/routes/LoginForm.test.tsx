import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { AuthProvider } from "@/components/AuthProvider";
import { LoginForm } from "@/routes/LoginForm";

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <AuthProvider>{ui}</AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

function stubFetch(status: number, body: unknown) {
  const mock = vi.fn(async () => new Response(JSON.stringify(body), {
    status, headers: { "content-type": "application/json" },
  }));
  globalThis.fetch = mock as unknown as typeof globalThis.fetch;
  return mock;
}

describe("LoginForm", () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => { originalFetch = globalThis.fetch; });
  afterEach(() => { globalThis.fetch = originalFetch; vi.restoreAllMocks(); });

  it("disables Log in until both fields are filled", async () => {
    render(wrap(<LoginForm />));
    const btn = screen.getByRole("button", { name: /log in/i });
    expect(btn).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    expect(btn).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    expect(btn).toBeEnabled();
  });

  it("posts credentials on submit", async () => {
    const fetchMock = stubFetch(200, { token: "t", username: "alice@example.com" });
    render(wrap(<LoginForm />));
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /log in/i }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const call = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(call[0]).toMatch(/\/api\/auth\/login$/);
    expect(JSON.parse(String(call[1]?.body))).toEqual({
      username: "alice@example.com", password: "password123",
    });
  });

  it("shows invalid-credentials error on 401", async () => {
    stubFetch(401, { error: "invalid_credentials", message: "nope" });
    render(wrap(<LoginForm />));
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "wrong");
    await userEvent.click(screen.getByRole("button", { name: /log in/i }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/invalid|does not match|nope/i);
  });

  it("shows not-registered hint on 404 with a link to signup", async () => {
    stubFetch(404, { error: "not_registered", message: "none" });
    render(wrap(<LoginForm onSwitchToSignup={vi.fn()} />));
    await userEvent.type(screen.getByLabelText(/email/i), "alice@example.com");
    await userEvent.type(screen.getByLabelText(/^password$/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /log in/i }));
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: /sign up/i })).toBeInTheDocument();
  });
});
