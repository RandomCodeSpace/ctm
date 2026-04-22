import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { SessionInputBar } from "@/components/SessionInputBar";

function renderBar(props: { sessionName: string; mode: "yolo" | "safe" }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <SessionInputBar
          sessionName={props.sessionName}
          mode={props.mode}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

function stubFetchOK() {
  const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
    return new Response(null, { status: 204 });
  });
  globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
  return fetchMock;
}

function stubFetchErr(status: number, body: unknown) {
  const fetchMock = vi.fn(async () => {
    return new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });
  });
  globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
  return fetchMock;
}

describe("SessionInputBar", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("renders nothing when mode is not yolo", () => {
    const { container } = renderBar({ sessionName: "alpha", mode: "safe" });
    expect(container.firstChild).toBeNull();
  });

  it("renders Yes / No / Continue buttons and a text input on yolo", () => {
    renderBar({ sessionName: "alpha", mode: "yolo" });
    expect(screen.getByRole("button", { name: /approve/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /deny/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /continue/i })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: /custom input/i })).toBeInTheDocument();
  });

  it("tapping Approve POSTs preset=yes", async () => {
    const fetchMock = stubFetchOK();
    renderBar({ sessionName: "alpha", mode: "yolo" });
    await userEvent.click(screen.getByRole("button", { name: /approve/i }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const [, init] = fetchMock.mock.calls[0];
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({ preset: "yes" });
  });

  it("tapping Deny POSTs preset=no", async () => {
    const fetchMock = stubFetchOK();
    renderBar({ sessionName: "alpha", mode: "yolo" });
    await userEvent.click(screen.getByRole("button", { name: /deny/i }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ preset: "no" });
  });

  it("tapping Continue POSTs preset=continue", async () => {
    const fetchMock = stubFetchOK();
    renderBar({ sessionName: "alpha", mode: "yolo" });
    await userEvent.click(screen.getByRole("button", { name: /continue/i }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ preset: "continue" });
  });

  it("pressing Enter in the text input POSTs text and clears the field", async () => {
    const fetchMock = stubFetchOK();
    renderBar({ sessionName: "alpha", mode: "yolo" });
    const input = screen.getByRole("textbox", { name: /custom input/i }) as HTMLInputElement;
    await userEvent.type(input, "approve{Enter}");
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ text: "approve" });
    await waitFor(() => expect(input.value).toBe(""));
  });

  it("send button is disabled while the text input is empty", () => {
    renderBar({ sessionName: "alpha", mode: "yolo" });
    const send = screen.getByRole("button", { name: /send/i });
    expect(send).toBeDisabled();
  });

  it("surfaces the server error message inline on failure", async () => {
    stubFetchErr(403, { error: "not_yolo", message: "boom" });
    renderBar({ sessionName: "alpha", mode: "yolo" });
    await userEvent.click(screen.getByRole("button", { name: /approve/i }));
    const status = await screen.findByRole("status");
    expect(status.textContent).toMatch(/boom|could not/i);
  });
});
