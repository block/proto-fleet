import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import RackPane from "./RackPane";

const { mockUseWindowDimensions } = vi.hoisted(() => ({
  mockUseWindowDimensions: vi.fn(),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
}));

const defaultProps = {
  rows: 4,
  cols: 4,
  numberingOrigin: "bottom-left" as const,
  slotAssignments: {},
  assignmentMode: "manual" as const,
  assignedCount: 0,
  totalSlots: 16,
  originLabel: "Bottom left",
  selectedSlotKey: "2-0",
  showPopover: true,
  hasMiners: false,
  onCellClick: vi.fn(),
  onSelectFromList: vi.fn(),
  onSearchMiners: vi.fn(),
  onScanQr: vi.fn(),
  onPopoverDismiss: vi.fn(),
  onHoverMiner: vi.fn(),
};

describe("RackPane", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseWindowDimensions.mockReturnValue({ isPhone: true, isTablet: false });
  });

  it("renders slot actions with the shared mobile action sheet styling", () => {
    render(<RackPane {...defaultProps} />);

    expect(screen.getByTestId("rack-slot-actions-sheet")).toBeInTheDocument();
    expect(screen.getByTestId("rack-slot-actions-sheet").parentElement).toBe(document.body);
    expect(screen.getByTestId("rack-slot-actions-sheet-content")).toHaveClass(
      "rounded-t-2xl",
      "bg-surface-elevated-base",
    );
    expect(screen.queryByTestId("rack-slot-popover")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Select from list" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Scan to assign" })).toBeInTheDocument();
  });

  it("dismisses only the slot action sheet when tapping the backdrop", () => {
    const parentClick = vi.fn();

    render(
      <div onClick={parentClick}>
        <RackPane {...defaultProps} />
      </div>,
    );

    fireEvent.click(screen.getByTestId("rack-slot-actions-sheet"));

    expect(defaultProps.onPopoverDismiss).toHaveBeenCalledTimes(1);
    expect(parentClick).not.toHaveBeenCalled();
    expect(defaultProps.onSelectFromList).not.toHaveBeenCalled();
    expect(defaultProps.onSearchMiners).not.toHaveBeenCalled();
    expect(defaultProps.onScanQr).not.toHaveBeenCalled();
  });

  it("remounts the desktop slot popover when the selected slot changes", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false, isTablet: false });

    const { rerender } = render(<RackPane {...defaultProps} hasMiners selectedSlotKey="2-0" />);
    const initialPopover = screen.getByTestId("rack-slot-popover");

    rerender(<RackPane {...defaultProps} hasMiners selectedSlotKey="2-1" />);

    expect(screen.getByTestId("rack-slot-popover")).not.toBe(initialPopover);
  });
});
