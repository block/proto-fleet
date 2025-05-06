import { fireEvent, render, waitFor } from "@testing-library/react";
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
    const deviceButton = getByTestId("device-widget-button");
    fireEvent.click(deviceButton);
    const rebootButton = getByTestId("reboot-popover-button");
    fireEvent.click(rebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);

    const confirmRebootButton = getByTestId("reboot-confirm-button");
    fireEvent.click(confirmRebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(false);
  });

  test("hides and displays action bar depending on modal visibility", async () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const settingsButton = getByTestId("settings-widget-button");
    fireEvent.click(settingsButton);
    const miningPoolsButton = getByTestId("mining-pool-popover-button");
    fireEvent.click(miningPoolsButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);

    const closeModalButton = getByTestId("header-icon-button");
    fireEvent.click(closeModalButton);

    await waitFor(() => {
      expect(actionBarElement.classList.contains("invisible")).toBe(false);
    });
  });
});
