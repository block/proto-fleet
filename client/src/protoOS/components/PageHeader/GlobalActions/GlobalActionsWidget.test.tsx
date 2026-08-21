import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { GlobalActionsWidget } from "./GlobalActionsWidget";
import { PopoverProvider } from "@/shared/components/Popover";

describe("GlobalActionsWidget", () => {
  const mockOnBlinkLEDs = vi.fn();
  const mockOnDownloadLogs = vi.fn();

  const defaultProps = {
    onBlinkLEDs: mockOnBlinkLEDs,
    onDownloadLogs: mockOnDownloadLogs,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  test("renders ellipsis button", () => {
    const { container, getByLabelText, getByTestId } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const button = container.querySelector("button");
    expect(button).toBeInTheDocument();
    expect(getByLabelText("Global actions")).toBeInTheDocument();
    expect(getByTestId("global-actions-widget")).toHaveClass("!h-8", "!w-8", "!p-0");
    expect(getByTestId("global-actions-widget").querySelector("svg")?.parentElement).toHaveClass("h-4", "shrink-0");
  });

  test("opens popover when ellipsis button is clicked", () => {
    const { container, getByText } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const button = container.querySelector("button");
    fireEvent.click(button!);

    expect(getByText("Blink LEDs")).toBeInTheDocument();
    expect(getByText("Download logs")).toBeInTheDocument();
  });

  test("calls onBlinkLEDs when Blink LEDs button is clicked", () => {
    const { container, getByText } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

    expect(mockOnBlinkLEDs).toHaveBeenCalledTimes(1);
  });

  test("calls onDownloadLogs when Download logs button is clicked", () => {
    const { container, getByText } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    expect(mockOnDownloadLogs).toHaveBeenCalledTimes(1);
  });

  test("closes popover after Blink LEDs is clicked", () => {
    const { container, getByText, queryByText } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    expect(getByText("Blink LEDs")).toBeInTheDocument();

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

    expect(queryByText("Blink LEDs")).not.toBeInTheDocument();
  });

  test("closes popover after Download logs is clicked", () => {
    const { container, getByText, queryByText } = render(
      <PopoverProvider>
        <GlobalActionsWidget {...defaultProps} />
      </PopoverProvider>,
    );

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    expect(getByText("Download logs")).toBeInTheDocument();

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    expect(queryByText("Download logs")).not.toBeInTheDocument();
  });
});
