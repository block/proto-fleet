import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import BulkCloseModal from "./BulkCloseModal";
import { RepairLocation } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const DismissibleBulkClose = ({ onSubmit }: { onSubmit: () => Promise<boolean> }) => {
  const [open, setOpen] = useState(true);
  return open ? (
    <BulkCloseModal
      ticketIds={["1", "2"]}
      onDismiss={() => setOpen(false)}
      onSuccess={() => setOpen(false)}
      onSubmit={onSubmit}
    />
  ) : null;
};

describe("BulkCloseModal", () => {
  it.each([
    { dismiss: "Escape", success: true },
    { dismiss: "header close", success: true },
    { dismiss: "Cancel", success: true },
    { dismiss: "Escape", success: false },
    { dismiss: "header close", success: false },
    { dismiss: "Cancel", success: false },
  ])(
    "blocks $dismiss during submission and restores dismissal after success=$success",
    async ({ dismiss, success }) => {
      const user = userEvent.setup();
      let finish!: (value: boolean) => void;
      const pending = new Promise<boolean>((resolve) => {
        finish = resolve;
      });
      render(<DismissibleBulkClose onSubmit={() => pending} />);
      await user.click(screen.getByRole("radio", { name: /Deferred/ }));
      await user.type(screen.getByLabelText("Notes (optional)"), "Waiting for parts");
      await user.click(screen.getByRole("button", { name: "Close tickets" }));
      expect(screen.getByRole("button", { name: "Close tickets" })).toBeDisabled();
      const attemptDismiss = () =>
        dismiss === "Escape"
          ? user.keyboard("{Escape}")
          : user.click(screen.getByRole("button", { name: dismiss === "Cancel" ? "Cancel" : "Close dialog" }));
      await attemptDismiss();
      expect(screen.getByLabelText("Notes (optional)")).toHaveValue("Waiting for parts");
      expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
      await act(async () => finish(success));
      if (!success) {
        expect(screen.getByRole("alert")).toHaveTextContent("Unable to close tickets");
        expect(screen.getByLabelText("Notes (optional)")).toHaveValue("Waiting for parts");
        expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
        await attemptDismiss();
      }
      expect(screen.queryByLabelText("Notes (optional)")).not.toBeInTheDocument();
    },
  );

  it("renders resolutions as a table and reveals notes after a selection", async () => {
    const onSuccess = vi.fn();
    render(<BulkCloseModal ticketIds={["1", "2", "3"]} onDismiss={vi.fn()} onSuccess={onSuccess} />);

    expect(screen.getByTestId("modal").parentElement).toHaveClass("w-[min(calc(100vw-(--spacing(4))),640px)]");
    expect(screen.getByRole("table", { name: "Close ticket resolution options" })).toBeInTheDocument();
    expect(screen.getByText("Select a resolution for all selected tickets.")).toHaveClass(
      "mb-4",
      "max-w-[600px]",
      "text-300",
    );
    expect(screen.getByText("Issue was fixed")).toHaveClass("text-right", "text-300");
    expect(screen.queryByLabelText("Notes (optional)")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Close tickets" })).toHaveClass("bg-core-primary-fill");
    expect(screen.getByRole("button", { name: "Close tickets" })).not.toHaveClass("bg-intent-critical-fill");
    expect(screen.getByRole("button", { name: "Close tickets" })).toBeDisabled();

    fireEvent.click(screen.getByRole("radio", { name: /Repaired/ }));

    expect(screen.getByLabelText("Notes (optional)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close tickets" }));

    await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce());
  });

  it("clears a hidden repair location after switching to a non-repair resolution", async () => {
    const onSubmit = vi.fn(async () => true);
    render(
      <BulkCloseModal ticketIds={["1"]} includesMiner onDismiss={vi.fn()} onSubmit={onSubmit} onSuccess={vi.fn()} />,
    );

    fireEvent.click(screen.getByRole("radio", { name: /Repaired/ }));
    fireEvent.change(screen.getByLabelText("Repair location"), {
      target: { value: String(RepairLocation.REPAIR_BENCH) },
    });
    fireEvent.click(screen.getByRole("radio", { name: /Deferred/ }));
    fireEvent.click(screen.getByRole("button", { name: "Close tickets" }));

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          case: "bulkClose",
          value: expect.objectContaining({
            repairLocation: RepairLocation.UNSPECIFIED,
          }),
        }),
      ),
    );
  });
});
