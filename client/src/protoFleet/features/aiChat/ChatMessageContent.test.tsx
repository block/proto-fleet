import { render, screen, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import ChatMessageContent from "./ChatMessageContent";

describe("ChatMessageContent", () => {
  test("renders Markdown data tables as structured HTML", () => {
    render(
      <ChatMessageContent
        content={`Fleet health

| Miner state | Count |
| --- | ---: |
| Total | 14 |
| Hashing | 0 |
| Offline | 14 |

All miners are offline.`}
      />,
    );

    const table = screen.getByRole("table");
    expect(table).toHaveClass("text-300");
    expect(table).not.toHaveClass("text-200");
    expect(within(table).getByRole("columnheader", { name: "Miner state" })).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: "Miner state" })).toHaveClass("text-emphasis-300");
    expect(within(table).getByRole("columnheader", { name: "Miner state" })).not.toHaveClass("text-emphasis-200");
    expect(within(table).getByRole("columnheader", { name: "Count" })).toHaveClass("text-right");
    const offlineRow = within(table).getByRole("cell", { name: "Offline" }).closest("tr");
    expect(offlineRow).not.toBeNull();
    expect(within(offlineRow as HTMLTableRowElement).getByRole("cell", { name: "14" })).toHaveClass("tabular-nums");
    expect(screen.getByText("All miners are offline.")).toBeInTheDocument();
    expect(screen.queryByText(/---:/)).not.toBeInTheDocument();
  });

  test("keeps ordinary answers as whitespace-preserving text", () => {
    const { container } = render(<ChatMessageContent content={"Offline: 14\nNo sites configured."} />);

    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(container.querySelector(".whitespace-pre-wrap")).toHaveTextContent("Offline: 14 No sites configured.");
  });
});
