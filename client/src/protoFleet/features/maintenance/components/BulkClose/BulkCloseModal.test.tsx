import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import BulkCloseModal from "./BulkCloseModal";

describe("BulkCloseModal", () => {
  it("uses the standard modal and submits one resolution for the selected tickets", () => {
    const onSuccess = vi.fn();
    render(<BulkCloseModal ticketIds={["1", "2", "3"]} onDismiss={vi.fn()} onSuccess={onSuccess} />);

    expect(screen.getByTestId("modal").parentElement).toHaveClass("w-[min(calc(100vw-(--spacing(4))),640px)]");
    expect(screen.getByRole("button", { name: "Close tickets" })).toBeDisabled();

    fireEvent.click(screen.getByRole("radio", { name: /Repaired/ }));
    fireEvent.click(screen.getByRole("button", { name: "Close tickets" }));

    expect(onSuccess).toHaveBeenCalledOnce();
  });
});
