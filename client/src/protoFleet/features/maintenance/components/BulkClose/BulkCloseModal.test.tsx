import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import BulkCloseModal from "./BulkCloseModal";

describe("BulkCloseModal", () => {
  it("renders resolutions as a table and reveals notes after a selection", () => {
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

    expect(onSuccess).toHaveBeenCalledOnce();
  });
});
