import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import ActionBar from ".";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";

// MinerActionsMenu imports hooks from the removed fleet store slice.
// Mock it so the test that renders it directly doesn't crash.
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu", () => ({
  default: ({ onActionStart }: { onActionStart?: () => void }) => (
    <div>
      <span>More</span>
      <button data-testid="actions-menu-button" onClick={onActionStart}>
        <button data-testid="reboot-popover-button" onClick={onActionStart}>
          Reboot
        </button>
      </button>
    </div>
  ),
}));

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: [],
    validatePool: vi.fn(({ onSuccess }) => {
      onSuccess?.();
    }),
    validatePoolPending: false,
  }),
}));

describe("Action Bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedItems: ["MAC1"],
    renderActions: () => <div>Action</div>,
  };

  const minersText = "miners selected";

  test("renders action bar correctly", () => {
    const { getByTestId, queryByText } = render(<ActionBar {...actionBarProps} />);

    const closeButton = getByTestId("close-button");
    expect(closeButton).toBeDefined();
    const minersElement = queryByText(minersText);
    expect(minersElement).toBeDefined();

    const actionButton = queryByText("Action");
    expect(actionButton).toBeDefined();
  });

  test("renders action bar with correct number of miners", () => {
    const selectedMiners = ["MAC1", "MAC2", "MAC3"];
    const { getByText } = render(<ActionBar {...actionBarProps} selectedItems={selectedMiners} />);

    const element = getByText(selectedMiners.length + " miners selected");
    expect(element).toBeInTheDocument();
  });

  test("hides action bar when there are no miners", () => {
    let selectedMiners = ["MAC1"];
    const { getByTestId, queryByTestId, rerender } = render(
      <ActionBar {...actionBarProps} selectedItems={selectedMiners} />,
    );

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();

    selectedMiners = [];
    rerender(<ActionBar {...actionBarProps} selectedItems={selectedMiners} />);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("closes action bar on click of close button", () => {
    const { getByTestId, queryByTestId } = render(<ActionBar {...actionBarProps} />);

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();
    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("renders MinerActionsMenu and calls setHidden method properly", async () => {
    const onActionStartMock = vi.fn();
    const selectedMiners = ["MinerId1"];

    const { getByText, getByTestId } = render(
      <ActionBar
        selectedItems={selectedMiners}
        renderActions={(setHidden) => (
          <MinerActionsMenu
            selectedMiners={selectedMiners}
            selectionMode="subset"
            onActionStart={() => {
              onActionStartMock();
              setHidden(true);
            }}
          />
        )}
      />,
    );

    expect(getByText("More")).toBeInTheDocument();

    fireEvent.click(getByTestId("actions-menu-button"));
    fireEvent.click(getByTestId("reboot-popover-button"));
    expect(onActionStartMock).toHaveBeenCalled();
  });

  test("calls onClose callback when close button is clicked", () => {
    const onCloseMock = vi.fn();
    const { getByTestId } = render(<ActionBar {...actionBarProps} onClose={onCloseMock} />);

    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(onCloseMock).toHaveBeenCalledOnce();
  });

  test("does not throw error when onClose is not provided", () => {
    const { getByTestId } = render(<ActionBar {...actionBarProps} />);

    const closeButton = getByTestId("close-button");

    // Should not throw error when clicking close without onClose prop
    expect(() => fireEvent.click(closeButton)).not.toThrow();
  });

  test("renders selection controls only once", () => {
    const onSelectAll = vi.fn();

    render(
      <ActionBar
        {...actionBarProps}
        selectionControls={
          <button type="button" data-testid="select-all-control" onClick={onSelectAll}>
            Select all
          </button>
        }
      />,
    );

    const controls = screen.getAllByTestId("select-all-control");
    expect(controls).toHaveLength(1);

    fireEvent.click(controls[0]);
    expect(onSelectAll).toHaveBeenCalledOnce();
  });
});
