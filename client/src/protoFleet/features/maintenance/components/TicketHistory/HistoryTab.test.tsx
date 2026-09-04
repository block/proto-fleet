import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import HistoryTab from "./HistoryTab";
const listCompleted = vi.fn(async ({ onSuccess, onFinally }) => {
  onSuccess({
    tickets: [
      {
        ticket: {
          id: 2n,
          ticketNumber: "TK-2",
          component: "Fan",
          diagnosis: "Fixed",
          minerIdentifier: "M2",
          resolution: 1,
          assigneeName: "alex",
          siteName: "Denver",
          buildingName: "B1",
        },
      },
    ],
    totalCount: 1,
    nextPageToken: "",
  });
  onFinally();
});
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ listCompleted }) }));
vi.mock("@/protoFleet/features/maintenance/hooks/useMaintenanceOptions", () => ({
  useMaintenanceOptions: () => ({ assignees: [{ id: "1", username: "alex" }] }),
}));
vi.mock("../TicketDetail/TicketDetailModal", () => ({ default: () => null }));
it("loads completed ticket history without an export control", async () => {
  render(<HistoryTab />);
  await waitFor(() => expect(screen.getByText("TK-2")).toBeInTheDocument());
  expect(screen.queryByRole("button", { name: /Export CSV/i })).not.toBeInTheDocument();
  expect(listCompleted).toHaveBeenCalledWith(expect.objectContaining({ pageSize: 50 }));
});
