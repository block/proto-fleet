import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ManageMinersModal from "./ManageMinersModal";

// Track the latest props passed to MinerSelectionList via a ref-based approach
const latestProps: { current: any } = { current: {} };
const mockGetSelection = vi.fn(() => ({
  selectedItems: [] as string[],
  allSelected: false,
  totalMiners: 0,
}));

vi.mock("@/protoFleet/components/MinerSelectionList", () => ({
  __esModule: true,
  default: forwardRef((props: any, ref: any) => {
    const propsRef = useRef(props);
    useEffect(() => {
      propsRef.current = props;
      latestProps.current = props;
    });
    useImperativeHandle(ref, () => ({
      getSelection: mockGetSelection,
    }));
    return <div data-testid="miner-selection-list" />;
  }),
}));

vi.mock("@/shared/components/Modal", () => ({
  default: ({ children, open, buttons }: any) =>
    open !== false ? (
      <div data-testid="modal">
        {children}
        {buttons?.map((btn: any, i: number) => (
          <button key={i} onClick={btn.onClick}>
            {btn.text}
          </button>
        ))}
      </div>
    ) : null,
}));

vi.mock("@/shared/components/Callout", () => ({
  default: ({ title }: any) => <div data-testid="callout">{title}</div>,
}));

vi.mock("@/shared/assets/icons", () => ({
  Alert: () => <span>alert-icon</span>,
  DismissCircle: () => <span>icon</span>,
}));

const defaultProps = {
  show: true,
  currentRackMiners: [] as string[],
  currentRackLabel: "Rack-01",
  maxSlots: 25,
  onDismiss: vi.fn(),
  onConfirm: vi.fn(),
};

describe("ManageMinersModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    latestProps.current = {};
  });

  it("does not render when show is false", () => {
    render(<ManageMinersModal {...defaultProps} show={false} />);
    expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
  });

  it("renders MinerSelectionList with correct filter config", () => {
    render(<ManageMinersModal {...defaultProps} />);

    expect(screen.getByTestId("miner-selection-list")).toBeInTheDocument();
    expect(latestProps.current.filterConfig).toEqual({
      showTypeFilter: true,
      showRackFilter: false,
      showGroupFilter: false,
    });
  });

  it("passes currentRackMiners as initialSelectedItems", () => {
    render(<ManageMinersModal {...defaultProps} currentRackMiners={["miner-1", "miner-2"]} />);
    expect(latestProps.current.initialSelectedItems).toEqual(["miner-1", "miner-2"]);
  });

  it("disables miners in other racks via isRowDisabled", () => {
    render(<ManageMinersModal {...defaultProps} currentRackLabel="Rack-01" />);

    const isRowDisabled = latestProps.current.isRowDisabled;
    expect(isRowDisabled({ rackLabel: "Other-Rack", deviceIdentifier: "m1" })).toBe(true);
    expect(isRowDisabled({ rackLabel: "Rack-01", deviceIdentifier: "m2" })).toBe(false);
    expect(isRowDisabled({ rackLabel: "", deviceIdentifier: "m3" })).toBe(false);
  });

  it("calls onConfirm with selected IDs on continue", () => {
    const onConfirm = vi.fn();
    mockGetSelection.mockReturnValue({
      selectedItems: ["miner-1", "miner-2"],
      allSelected: false,
      totalMiners: 10,
    });

    render(<ManageMinersModal {...defaultProps} onConfirm={onConfirm} />);
    fireEvent.click(screen.getByText(/Continue/));

    expect(onConfirm).toHaveBeenCalledWith(["miner-1", "miner-2"], false, undefined);
  });

  it("shows overflow error when selection exceeds maxSlots", () => {
    mockGetSelection.mockReturnValue({
      selectedItems: ["m1", "m2", "m3"],
      allSelected: false,
      totalMiners: 10,
    });

    render(<ManageMinersModal {...defaultProps} maxSlots={2} />);
    fireEvent.click(screen.getByText(/Continue/));

    expect(screen.getByText(/Cannot add 3 miners with only 2 available slots/)).toBeInTheDocument();
  });

  it("does not call onConfirm when overflow error", () => {
    const onConfirm = vi.fn();
    mockGetSelection.mockReturnValue({
      selectedItems: ["m1", "m2", "m3"],
      allSelected: false,
      totalMiners: 10,
    });

    render(<ManageMinersModal {...defaultProps} maxSlots={2} onConfirm={onConfirm} />);
    fireEvent.click(screen.getByText(/Continue/));

    expect(onConfirm).not.toHaveBeenCalled();
  });
});
