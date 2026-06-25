import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

describe("CurtailmentStopConfirmationDialog", () => {
  it("keeps restore confirmation open while submitting", () => {
    const onCancel = vi.fn();

    render(
      <CurtailmentStopConfirmationDialog open action="restore" isSubmitting onCancel={onCancel} onConfirm={vi.fn()} />,
    );

    fireEvent.keyDown(document, { key: "Escape" });

    expect(onCancel).not.toHaveBeenCalled();
  });
});
