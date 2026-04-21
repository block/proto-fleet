import { render, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import PowerTargetPopover from "./PowerTargetPopover";
import { PopoverProvider } from "@/shared/components/Popover";

const mockedUpdateMiningTarget = vi.fn();

// Create mock return value that we can modify per test
let mockReturnValue = {
  miningTarget: 1500,
  defaultTarget: 1200,
  performanceMode: "MaximumHashrate",
  bounds: { min: 400, max: 2000 },
  pending: false,
  updateMiningTarget: mockedUpdateMiningTarget,
};

vi.mock("@/protoOS/api/hooks/useMiningTarget", () => ({
  useMiningTarget: vi.fn(() => mockReturnValue),
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

beforeEach(() => {
  // Reset mock function
  mockedUpdateMiningTarget.mockClear();

  // Reset mock values for each test
  mockReturnValue.miningTarget = 1500;
  mockReturnValue.defaultTarget = 1200;
  mockReturnValue.performanceMode = "MaximumHashrate";
  mockReturnValue.bounds = { min: 400, max: 2000 };
  mockReturnValue.pending = false;
  mockReturnValue.updateMiningTarget = mockedUpdateMiningTarget;

  // Clear mock call history
  vi.clearAllMocks();
});

describe("Power Target Popover", () => {
  it("renders the power target options correctly", () => {
    const { getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );

    // Check that all options are present
    expect(getByText("Default")).toBeInTheDocument();
    expect(getByText("Max")).toBeInTheDocument();
    expect(getByText("Custom")).toBeInTheDocument();

    // Check that the power values are showing correctly
    expect(getByText("1.2 kW")).toBeInTheDocument(); // Default target
    expect(getByText("2 kW")).toBeInTheDocument(); // Max target
  });

  it("shows input field when Custom is selected", async () => {
    // Set miningTarget equal to defaultTarget so component starts in "default" mode
    mockReturnValue.miningTarget = 1200; // Same as defaultTarget

    const user = userEvent.setup();
    const { getByText, queryByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} />,
      </PopoverProvider>,
    );

    // Initially, input should not be visible (in default mode)
    expect(queryByLabelText("Power target")).not.toBeInTheDocument();

    // Click Custom option
    await user.click(getByText("Custom"));

    // Now input should be visible
    expect(queryByLabelText("Power target")).toBeInTheDocument();
  });

  it("shows error when input value is below minimum bound", async () => {
    const user = userEvent.setup();
    const { getByText, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} onUpdateStart={vi.fn()} />,
      </PopoverProvider>,
    );

    // First select Custom mode to make the input appear
    await user.click(getByText("Custom"));
    const input = getByLabelText("Power target");
    await user.clear(input);
    await user.type(input, "0.1");

    // Check for error that contains minimum power text
    expect(getByText(/minimum power target/i)).toBeInTheDocument();
  });

  it("shows error when input value is above maximum bound", async () => {
    const user = userEvent.setup();
    const { getByText, getByLabelText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} onUpdateStart={vi.fn()} />,
      </PopoverProvider>,
    );
    // First select Custom mode to make the input appear
    await user.click(getByText("Custom"));
    const input = getByLabelText("Power target");
    await user.clear(input);
    await user.type(input, "4");
    // Check for error that contains maximum power text
    expect(getByText(/maximum power target/i)).toBeInTheDocument();
  });

  it("calls updateMiningTarget with correct values when Apply is clicked", async () => {
    const user = userEvent.setup();
    const mockedOnUpdateStart = vi.fn();
    const { getByText, getByLabelText, getByTestId } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} onUpdateStart={mockedOnUpdateStart} />
      </PopoverProvider>,
    );
    // First select Custom mode to make the input appear
    await user.click(getByText("Custom"));
    const input = getByLabelText("Power target");
    await user.clear(input);
    await user.type(input, "1");

    // Wait for input value to be processed and button to be enabled
    await waitFor(() => {
      expect(input).toHaveValue(1);
      expect(getByTestId("power-target-apply-button")).not.toBeDisabled();
    });

    await user.click(getByTestId("power-target-apply-button"));
    await waitFor(() => {
      expect(mockedOnUpdateStart).toHaveBeenCalledWith({
        performance_mode: "MaximumHashrate",
        power_target_watts: 1000,
      });
    });
  });

  it("disables Apply button when error is present", async () => {
    const user = userEvent.setup();
    const { getByTestId, getByLabelText, getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} onUpdateStart={vi.fn()} />,
      </PopoverProvider>,
    );
    // First select Custom mode to make the input appear
    await user.click(getByText("Custom"));
    const input = getByLabelText("Power target");
    await user.clear(input);
    await user.type(input, "0");
    expect(getByTestId("power-target-apply-button")).toBeDisabled();
  });

  it("calls onDismiss when Cancel is clicked", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const { getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={onDismiss} onUpdateStart={vi.fn()} />,
      </PopoverProvider>,
    );
    await user.click(getByText("Cancel"));
    expect(onDismiss).toHaveBeenCalled();
  });

  it("shows loading state when pending is true", () => {
    // Override mock to set pending to true for this test
    mockReturnValue.pending = true;

    const { getByText } = render(
      <PopoverProvider>
        <PowerTargetPopover onDismiss={vi.fn()} onUpdateStart={vi.fn()} />,
      </PopoverProvider>,
    );
    expect(getByText("Applying")).toBeInTheDocument();
  });
});
