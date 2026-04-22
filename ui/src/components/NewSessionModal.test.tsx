import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { NewSessionModal } from "@/components/NewSessionModal";

const navigateMock = vi.fn();
vi.mock("react-router", async () => {
  const actual =
    await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigateMock };
});

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

function stubFetchSequence(responses: Array<{ status: number; body: unknown }>) {
  let i = 0;
  const mock = vi.fn(async () => {
    const r = responses[i++] ?? responses[responses.length - 1];
    return new Response(
      r.body === undefined ? "" : JSON.stringify(r.body),
      {
        status: r.status,
        headers: { "content-type": "application/json" },
      },
    );
  });
  globalThis.fetch = mock as unknown as typeof globalThis.fetch;
  return mock;
}

describe("NewSessionModal", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    navigateMock.mockReset();
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders workdir input and Create button disabled initially", () => {
    render(
      wrap(<NewSessionModal open={true} onClose={vi.fn()} recents={[]} />),
    );
    const input = screen.getByRole("textbox", { name: /workdir/i });
    const create = screen.getByRole("button", { name: /create/i });
    expect(input).toBeInTheDocument();
    expect(create).toBeDisabled();
  });

  it("pre-fills the top recent and enables Create", () => {
    render(
      wrap(
        <NewSessionModal
          open={true}
          onClose={vi.fn()}
          recents={["/home/dev/projects/ctm", "/home/dev/other"]}
        />,
      ),
    );
    const input = screen.getByRole("textbox", { name: /workdir/i }) as HTMLInputElement;
    expect(input.value).toBe("/home/dev/projects/ctm");
    expect(screen.getByRole("button", { name: /create/i })).toBeEnabled();
  });

  it("tapping a recent replaces the input value", async () => {
    render(
      wrap(
        <NewSessionModal
          open={true}
          onClose={vi.fn()}
          recents={["/a", "/b", "/c"]}
        />,
      ),
    );
    await userEvent.click(screen.getByRole("button", { name: "/b" }));
    const input = screen.getByRole("textbox", { name: /workdir/i }) as HTMLInputElement;
    expect(input.value).toBe("/b");
  });

  it("submits and navigates on 201", async () => {
    const onClose = vi.fn();
    stubFetchSequence([
      { status: 201, body: { name: "ctm", uuid: "u", mode: "yolo", workdir: "/ctm" } },
    ]);
    render(
      wrap(<NewSessionModal open={true} onClose={onClose} recents={["/ctm"]} />),
    );
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith("/s/ctm"));
    expect(onClose).toHaveBeenCalled();
  });

  it("on 409 collision, surfaces the rename / go-to-existing panel", async () => {
    stubFetchSequence([
      {
        status: 409,
        body: {
          error: "name_exists",
          message: "exists",
          session: { name: "ctm", uuid: "u", mode: "yolo", workdir: "/ctm" },
        },
      },
    ]);
    render(
      wrap(<NewSessionModal open={true} onClose={vi.fn()} recents={["/ctm"]} />),
    );
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    expect(
      await screen.findByRole("button", { name: /go to existing/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /rename/i })).toBeInTheDocument();
  });

  it("rename flow re-submits with a new name", async () => {
    const fetchMock = stubFetchSequence([
      {
        status: 409,
        body: {
          error: "name_exists",
          message: "exists",
          session: { name: "ctm", uuid: "u", mode: "yolo", workdir: "/ctm" },
        },
      },
      { status: 201, body: { name: "ctm-2", uuid: "u2", mode: "yolo", workdir: "/ctm" } },
    ]);
    render(
      wrap(<NewSessionModal open={true} onClose={vi.fn()} recents={["/ctm"]} />),
    );
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    await userEvent.click(await screen.findByRole("button", { name: /rename/i }));

    const nameInput = screen.getByRole("textbox", { name: /new name/i }) as HTMLInputElement;
    expect(nameInput.value).toBe("ctm-2");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith("/s/ctm-2"));
    const call1 = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(JSON.parse(String(call1[1]?.body))).toEqual({
      workdir: "/ctm",
      name: "ctm-2",
    });
  });
});
