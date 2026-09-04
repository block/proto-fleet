import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MaintenancePage from "./MaintenancePage";
vi.mock("../components/TicketQueue/TicketQueue", () => ({ default: () => <div>Queue content</div> }));
vi.mock("../components/TicketHistory/HistoryTab", () => ({ default: () => <div>History content</div> }));
vi.mock("../components/TicketInventory/InventoryTab", () => ({ default: () => <div>Inventory content</div> }));
it("exposes Queue, History, and Inventory tabs", async () => {
  const user = userEvent.setup();
  render(<MaintenancePage />);
  expect(screen.getByText("Queue content")).toBeInTheDocument();
  expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual(["Queue", "Inventory", "History"]);
  await user.click(screen.getByRole("button", { name: "Inventory" }));
  expect(screen.getByText("Inventory content")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "History" })).toBeInTheDocument();
});
