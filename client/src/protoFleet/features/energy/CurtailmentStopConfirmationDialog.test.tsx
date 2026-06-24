import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

describe("CurtailmentStopConfirmationDialog", () => {
  it("warns that force restore does not disable automation", () => {
    render(<CurtailmentStopConfirmationDialog open action="forceRestore" onCancel={vi.fn()} onConfirm={vi.fn()} />);

    expect(screen.getByText("Force restore automation event?")).toBeInTheDocument();
    expect(screen.getByText(/bypasses restore guards for this event only/i)).toBeInTheDocument();
    expect(screen.getByText(/stop or disable the automation first/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Force restore" })).toBeInTheDocument();
  });

  it("keeps force restore open while submitting", () => {
    const onCancel = vi.fn();

    render(
      <CurtailmentStopConfirmationDialog
        open
        action="forceRestore"
        isSubmitting
        onCancel={onCancel}
        onConfirm={vi.fn()}
      />,
    );

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onCancel).not.toHaveBeenCalled();
  });
});
