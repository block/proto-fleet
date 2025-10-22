import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

describe("Miner list action bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedMiners: ["MAC1"],
  };

  test("hides and displays action bar depending on confirmation dialog visibility", () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const actionsMenuButton = getByTestId("actions-menu-button");
    fireEvent.click(actionsMenuButton);
    const rebootButton = getByTestId("reboot-popover-button");
    fireEvent.click(rebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);

    const confirmRebootButton = getByTestId("reboot-confirm-button");
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
});
