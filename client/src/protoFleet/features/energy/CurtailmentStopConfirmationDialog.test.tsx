import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import CurtailmentStopConfirmationDialog from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";

describe("CurtailmentStopConfirmationDialog", () => {
  it("warns that force restore overrides automation demand and duration guards", () => {
    render(<CurtailmentStopConfirmationDialog open action="forceRestore" onCancel={vi.fn()} onConfirm={vi.fn()} />);

    expect(screen.getByText("Force restore automation event?")).toBeInTheDocument();
    expect(screen.getByText(/overrides active automation demand and minimum-duration guards/i)).toBeInTheDocument();
    expect(screen.getByText(/MQTT demand is still OFF or the source is stale/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Force restore" })).toBeInTheDocument();
  });
});
