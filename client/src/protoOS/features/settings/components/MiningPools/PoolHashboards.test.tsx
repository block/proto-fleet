import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import PoolHashboards from "./PoolHashboards";
import type { PoolBoardAssignment } from "@/protoOS/api/adapters/poolConfiguration";

const boards: PoolBoardAssignment[] = Array.from({ length: 60 }, (_, index) => ({
  mac: `AA:BB:CC:00:00:${index.toString(16).padStart(2, "0")}`,
  location: `Tank 1 · Module ${Math.floor(index / 3) + 1} · Board ${(index % 3) + 1}`,
})).reverse();

function openBoards() {
  fireEvent.click(screen.getByRole("button", { name: "View Client A hashboards" }));
  return screen.getByRole("dialog", { name: "Client A hashboards" });
}

test("keeps boards out of the page and renders only the current page in numeric location order", () => {
  render(<PoolHashboards boards={boards} title="Client A hashboards" />);
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByText(boards[0].mac)).not.toBeInTheDocument();
  const dialog = openBoards();
  expect(within(dialog).getAllByRole("row")).toHaveLength(26);
  expect(within(dialog).getAllByRole("row")[1]).toHaveTextContent("Tank 1 · Module 1 · Board 1");
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 1–25 of 60 hashboards");
  expect(within(dialog).getByRole("button", { name: "Previous page" })).toBeDisabled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Next page" }));
  expect(within(dialog).getAllByRole("row")[1]).toHaveTextContent("Tank 1 · Module 9 · Board 2");
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 26–50 of 60 hashboards");
  fireEvent.click(within(dialog).getByRole("button", { name: "Next page" }));
  expect(within(dialog).getAllByRole("row")).toHaveLength(11);
  expect(within(dialog).getByRole("button", { name: "Next page" })).toBeDisabled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Previous page" }));
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 26–50 of 60 hashboards");
});

test("searches all pages by MAC or location and resets pagination when the search changes", () => {
  render(<PoolHashboards boards={boards} title="Client A hashboards" />);
  const dialog = openBoards();
  fireEvent.click(within(dialog).getByRole("button", { name: "Next page" }));
  const search = within(dialog).getByLabelText("Search MAC or location");
  fireEvent.change(search, { target: { value: "aa:bb:cc:00:00:3b" } });
  expect(within(dialog).getAllByRole("row")).toHaveLength(2);
  expect(within(dialog).getByText("Tank 1 · Module 20 · Board 3")).toBeInTheDocument();
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 1–1 of 1 hashboard");
  fireEvent.change(search, { target: { value: "  tank 1 MODULE 20  " } });
  expect(within(dialog).getAllByRole("row")).toHaveLength(4);
  fireEvent.change(search, { target: { value: "missing-board" } });
  expect(within(dialog).getByText("No matching hashboards.")).toBeInTheDocument();
  fireEvent.change(search, { target: { value: "" } });
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 1–25 of 60 hashboards");
});

test("keeps unknown locations last, searchable, and ordered by MAC without changing the input", () => {
  const assignments = [
    { mac: "FF:FF:FF:FF:FF:FF" },
    { mac: "22:22:22:22:22:22", location: "Tank 10 · Module 1 · Board 1" },
    { mac: "11:11:11:11:11:11", location: "Tank 2 · Module 1 · Board 1" },
    { mac: "AA:AA:AA:AA:AA:AA" },
  ];
  render(<PoolHashboards boards={assignments} title="Client A hashboards" />);
  const dialog = openBoards();
  const rows = within(dialog).getAllByRole("row").slice(1);
  expect(rows.map((row) => within(row).getAllByRole("cell")[1].textContent)).toEqual([
    "11:11:11:11:11:11",
    "22:22:22:22:22:22",
    "AA:AA:AA:AA:AA:AA",
    "FF:FF:FF:FF:FF:FF",
  ]);
  expect(assignments[0].mac).toBe("FF:FF:FF:FF:FF:FF");
  fireEvent.change(within(dialog).getByLabelText("Search MAC or location"), {
    target: { value: "location unavailable" },
  });
  expect(within(dialog).getAllByRole("row")).toHaveLength(3);
});

test("clamps a smaller inventory to a valid page and resets the dialog when reopened", () => {
  const { rerender } = render(<PoolHashboards boards={boards} title="Client A hashboards" />);
  const dialog = openBoards();
  fireEvent.click(within(dialog).getByRole("button", { name: "Next page" }));
  rerender(<PoolHashboards boards={boards.slice(0, 3)} title="Client A hashboards" />);
  expect(within(dialog).getByRole("status")).toHaveTextContent("Showing 1–3 of 3 hashboards");
  fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  rerender(<PoolHashboards boards={boards} title="Client A hashboards" />);
  expect(within(openBoards()).getByRole("status")).toHaveTextContent("Showing 1–25 of 60 hashboards");
});

test("selects hashboards across pages and keeps the selection when returning from the pool picker", async () => {
  const onEditPool = vi.fn();
  const { rerender } = render(<PoolHashboards boards={boards} title="Client A hashboards" onEditPool={onEditPool} />);
  const dialog = within(openBoards());
  fireEvent.click(dialog.getByRole("checkbox", { name: "Select all hashboards on this page" }));
  fireEvent.click(dialog.getByRole("button", { name: "Next page" }));
  expect(dialog.getByText("25 hashboards selected")).toBeInTheDocument();
  fireEvent.click(dialog.getByRole("button", { name: "Select all" }));
  expect(dialog.getByText("60 hashboards selected")).toBeInTheDocument();
  fireEvent.click(dialog.getByRole("button", { name: "Edit pool" }));
  expect(new Set(onEditPool.mock.calls[0][0])).toEqual(new Set(boards.map(({ mac }) => mac)));

  rerender(<PoolHashboards boards={boards} title="Client A hashboards" onEditPool={onEditPool} poolPickerOpen />);
  rerender(<PoolHashboards boards={boards} title="Client A hashboards" onEditPool={onEditPool} />);
  const reopened = within(await screen.findByRole("dialog", { name: "Client A hashboards" }));
  expect(reopened.getByText("60 hashboards selected")).toBeInTheDocument();
  expect(reopened.getByRole("status")).toHaveTextContent("Showing 26–50 of 60 hashboards");
});
