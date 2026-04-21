import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BashOnlyRow } from "./BashOnlyRow";
import type { ToolCallRow } from "@/hooks/useFeed";

function makeRow(overrides: Partial<ToolCallRow> = {}): ToolCallRow {
  return {
    session: "alpha",
    tool: "Bash",
    input: "ls -la /tmp",
    summary: "total 0\ndrwxrwxrwt  1 root root 4096 Apr 21 16:00 .",
    is_error: false,
    ts: "2026-04-21T16:28:00Z",
    ...overrides,
  };
}

describe("BashOnlyRow", () => {
  it("renders the command text and an 'ok' chip on success", () => {
    render(<BashOnlyRow row={makeRow()} />);
    expect(screen.getByText(/ls -la \/tmp/)).toBeInTheDocument();
    const chip = screen.getByTestId("bash-chip");
    expect(chip).toHaveAttribute("data-status", "ok");
    expect(chip).toHaveTextContent(/^ok$/i);
  });

  it("treats exit_code 0 as success even when field is present", () => {
    render(<BashOnlyRow row={makeRow({ exit_code: 0 })} />);
    const chip = screen.getByTestId("bash-chip");
    expect(chip).toHaveAttribute("data-status", "ok");
    expect(chip).toHaveTextContent(/^ok$/i);
  });

  it("renders an 'err <n>' chip when exit_code is non-zero", () => {
    render(
      <BashOnlyRow row={makeRow({ is_error: true, exit_code: 127 })} />,
    );
    const chip = screen.getByTestId("bash-chip");
    expect(chip).toHaveAttribute("data-status", "err");
    expect(chip).toHaveTextContent(/err\s*127/i);
  });

  it("renders a bare 'err' chip when is_error is true without exit_code", () => {
    render(<BashOnlyRow row={makeRow({ is_error: true })} />);
    const chip = screen.getByTestId("bash-chip");
    expect(chip).toHaveAttribute("data-status", "err");
    expect(chip).toHaveTextContent(/^err$/i);
  });

  it("expands on click to show the full command and output", async () => {
    const user = userEvent.setup();
    const long =
      "echo " + "abcdefghij".repeat(20); // > 120 chars so it truncates
    render(
      <BashOnlyRow
        row={makeRow({
          input: long,
          summary: "line1\nline2\nline3",
        })}
      />,
    );

    // Collapsed: no expanded blocks.
    expect(screen.queryByTestId("bash-expanded-cmd")).toBeNull();
    expect(screen.queryByTestId("bash-expanded-output")).toBeNull();

    await user.click(screen.getByRole("button"));

    expect(screen.getByTestId("bash-expanded-cmd")).toHaveTextContent(long);
    const output = screen.getByTestId("bash-expanded-output");
    expect(output).toHaveTextContent("line1");
    expect(output).toHaveTextContent("line2");
    expect(output).toHaveTextContent("line3");
  });
});
