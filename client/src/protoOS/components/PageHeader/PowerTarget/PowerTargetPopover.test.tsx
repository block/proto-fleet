import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import PowerTargetPopover from "./PowerTargetPopover";
import { PopoverProvider } from "@/shared/components/Popover";

let mockedPending = false;
const mockedUpdateMiningTarget = vi.fn();
vi.mock("@/protoOS/api/useMiningTarget", () => ({
  useMiningTarget: vi.fn(() => ({
    miningTarget: 1500,
    performanceMode: "MaximumHashrate",
    bounds: { min: 400, max: 2000 },
    pending: mockedPending,
    updateMiningTarget: mockedUpdateMiningTarget,
  })),
}));

// mock canvas used in useValueWidth hook
beforeAll(() => {
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    value: vi.fn(() => ({
      font: "",
      measureText: vi.fn(() => ({ width: 42 })),
    })),
    configurable: true,
  });
});

describe("Power Target Popover", () => {
  it("shows error when input value is below minimum bound", () => {
    const { getByText, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );
    const input = getByLabelText("Power target");
    fireEvent.change(input, { target: { value: "0.1" } });
    expect(
      getByText("Value must be between 0.4kW and 2kW"),
    ).toBeInTheDocument();
  });

  it("shows error when input value is above maximum bound", () => {
    const { getByText, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );
    const input = getByLabelText("Power target");
    fireEvent.change(input, { target: { value: "4" } });
    expect(
      getByText("Value must be between 0.4kW and 2kW"),
    ).toBeInTheDocument();
  });

  it("calls updateMiningTarget with correct values when Apply is clicked", async () => {
    const { getByText, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );
    const input = getByLabelText("Power target");
    fireEvent.change(input, { target: { value: "1" } });
    fireEvent.click(getByText("Apply"));
    await waitFor(() => {
      expect(mockedUpdateMiningTarget).toHaveBeenCalledWith({
        performance_mode: "MaximumHashrate",
        power_target_watts: 1000,
      });
    });
  });

  it("disables Apply button when error is present", () => {
    const { getByTestId, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );
    const input = getByLabelText("Power target");
    fireEvent.change(input, { target: { value: "0" } });
    expect(getByTestId("power-target-apply-button")).toBeDisabled();
  });

  it("calls onDismiss when Cancel is clicked", () => {
    const onDismiss = vi.fn();
    const { getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={onDismiss} />,
      </PopoverProvider>,
    );
    fireEvent.click(getByText("Cancel"));
    expect(onDismiss).toHaveBeenCalled();
  });

  it("shows loading state when pending is true", () => {
    mockedPending = true;
    const { getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );
    expect(getByText("Applying")).toBeInTheDocument();
  });
});
