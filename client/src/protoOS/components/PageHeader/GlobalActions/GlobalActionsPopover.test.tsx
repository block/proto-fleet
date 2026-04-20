import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { GlobalActionsPopover } from "./GlobalActionsPopover";
import { PopoverProvider } from "@/shared/components/Popover";

describe("GlobalActionsPopover", () => {
  const mockOnBlinkLEDs = vi.fn();
  const mockOnDownloadLogs = vi.fn();

  const defaultProps = {
    onBlinkLEDs: mockOnBlinkLEDs,
    onDownloadLogs: mockOnDownloadLogs,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("renders Blink LEDs button", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const blinkButton = getByText("Blink LEDs");
    expect(blinkButton).toBeInTheDocument();
  });

  test("renders Download logs button", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const downloadButton = getByText("Download logs");
    expect(downloadButton).toBeInTheDocument();
  });

  test("calls onBlinkLEDs when Blink LEDs button is clicked", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

    expect(mockOnBlinkLEDs).toHaveBeenCalledTimes(1);
  });

  test("calls onDownloadLogs when Download logs button is clicked", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    expect(mockOnDownloadLogs).toHaveBeenCalledTimes(1);
  });

  test("renders LEDIndicator icon for Blink LEDs button", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const blinkButton = getByText("Blink LEDs").closest("button");
    const svg = blinkButton?.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });

  test("renders Terminal icon for Download logs button", () => {
    const { getByText } = render(
      <PopoverProvider>
        <GlobalActionsPopover {...defaultProps} />
      </PopoverProvider>,
    );

    const downloadButton = getByText("Download logs").closest("button");
    const svg = downloadButton?.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });
});
