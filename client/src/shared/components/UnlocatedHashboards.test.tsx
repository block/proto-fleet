import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import UnlocatedHashboards, { type UnlocatedHashboard } from "./UnlocatedHashboards";

const boards: UnlocatedHashboard[] = Array.from({ length: 28 }, (_, i) => ({
  id: `board-${i}`,
  identity: `aa:bb:cc:dd:ee:${String(i).padStart(2, "0")}`,
  serial: `serial-${i}`,
  status: "Hashing",
  hashrate: "2.0 TH/s",
  temperature: "28.0 °C",
  firmware: "v1.2",
  issues: i === 27 ? ["Over temperature"] : [],
}));

describe("Unlocated hashboards", () => {
  it("opens, pages, searches by serial, and shows one board's issue details", () => {
    render(<UnlocatedHashboards boards={boards} />);
    fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
    expect(screen.getByRole("dialog", { name: "Unlocated hashboards" })).toBeInTheDocument();
    expect(screen.getByText("Showing 1–25 of 28")).toBeInTheDocument();
    expect(screen.queryByText(boards[27].identity)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    fireEvent.click(screen.getByRole("button", { name: `View hashboard ${boards[27].identity}` }));
    expect(screen.getByText("Over temperature")).toBeInTheDocument();
    expect(screen.getByText("Firmware: v1.2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Back to hashboards" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Search MAC or serial number" }), {
      target: { value: "serial-3" },
    });
    expect(screen.getByText("Showing 1–1 of 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: `View hashboard ${boards[3].identity}` })).toBeInTheDocument();
    expect(screen.queryByText(/Tank 0|Module 0/)).not.toBeInTheDocument();
  });

  it("retains inventory but hides stale current readings", () => {
    render(<UnlocatedHashboards boards={[boards[0]]} stale />);
    fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
    expect(screen.getByText(boards[0].identity)).toBeInTheDocument();
    expect(screen.queryByText("2.0 TH/s")).not.toBeInTheDocument();
    expect(screen.getByText("Unknown")).toBeInTheDocument();
  });

  it("removes relocated boards from the open list without losing the dialog", () => {
    const { rerender } = render(<UnlocatedHashboards boards={[boards[0]]} />);
    fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
    rerender(<UnlocatedHashboards boards={[]} />);
    expect(screen.getByRole("dialog", { name: "Unlocated hashboards" })).toBeInTheDocument();
    expect(screen.getByText("No unlocated hashboards reported.")).toBeInTheDocument();
    expect(screen.queryByText(boards[0].identity)).not.toBeInTheDocument();
  });

  it("does not show a callout for an empty inventory", () => {
    const { container } = render(<UnlocatedHashboards boards={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});

it("opens lower-row details at the top and restores list scroll and focus on return", () => {
  render(<UnlocatedHashboards boards={boards} />);
  fireEvent.click(screen.getByRole("button", { name: "View hashboards" }));
  const scrollArea = screen.getByTestId("modal");
  fireEvent.scroll(scrollArea, { target: { scrollTop: 800 } });
  const lowerRow = screen.getByRole("button", { name: `View hashboard ${boards[24].identity}` });
  lowerRow.focus();
  expect(lowerRow).toHaveFocus();
  fireEvent.click(lowerRow);
  expect(scrollArea.scrollTop).toBe(0);
  expect(scrollArea.querySelector("button")).toHaveFocus();
  expect(lowerRow.isConnected).toBe(false);
  fireEvent.scroll(scrollArea, { target: { scrollTop: 120 } });
  const back = screen.getByRole("button", { name: "Back to hashboards" });
  back.focus();
  fireEvent.click(back);
  expect(scrollArea.scrollTop).toBe(800);
  expect(scrollArea.querySelector("button")).toHaveFocus();
  expect(back.isConnected).toBe(false);
});
