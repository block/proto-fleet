import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

describe("Miner list action bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedMiners: ["MAC1"],
  };

  test("hides and displays action bar depending on confirmation dialog visibility", async () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const actionsMenuButton = getByTestId("actions-menu-button");
    fireEvent.click(actionsMenuButton);
    const rebootButton = getByTestId("reboot-popover-button");
    fireEvent.click(rebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);

    const confirmRebootButton = await waitFor(() =>
      getByTestId("reboot-confirm-button"),
    );
    fireEvent.click(confirmRebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(false);
  });

  test("hides action bar when mining pool action is triggered", () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const actionsMenuButton = getByTestId("actions-menu-button");
    fireEvent.click(actionsMenuButton);
    const miningPoolsButton = getByTestId("mining-pool-popover-button");
    fireEvent.click(miningPoolsButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);
  });

  test("calls onClearSelection when action bar close button is clicked", () => {
    const onClearSelectionMock = vi.fn();
    const { getByTestId } = render(
      <MinerListActionBar
        {...actionBarProps}
        onClearSelection={onClearSelectionMock}
      />,
    );

    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(onClearSelectionMock).toHaveBeenCalledOnce();
  });

  test("does not throw when onClearSelection is not provided", () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const closeButton = getByTestId("close-button");

    // Should not throw error when clicking close without onClearSelection prop
    expect(() => fireEvent.click(closeButton)).not.toThrow();
  });
});
