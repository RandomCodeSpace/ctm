import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToolFrequencySparkline } from "./ToolFrequencySparkline";
import type { ToolCallRow } from "@/hooks/useFeed";

function renderWithFeed(sessionName: string, events: ToolCallRow[]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData<ToolCallRow[]>(["feed", sessionName], events);
  return render(
    <QueryClientProvider client={qc}>
      <ToolFrequencySparkline sessionName={sessionName} />
    </QueryClientProvider>,
  );
}

function tc(ts: string, tool = "Bash"): ToolCallRow {
  return { session: "s", tool, input: "", is_error: false, ts };
}

describe("ToolFrequencySparkline", () => {
  it("renders nothing when the feed cache is empty", () => {
    const { container } = renderWithFeed("s", []);
    expect(container.querySelector("svg")).toBeNull();
  });

  it("renders a 20-rect SVG when there is cached activity", () => {
    const events: ToolCallRow[] = [];
    for (let i = 0; i < 5; i++) {
      events.push(tc(new Date(Date.now() - i * 60_000).toISOString()));
    }
    const { container } = renderWithFeed("s", events);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg!.querySelectorAll("rect").length).toBe(20);
  });
});
