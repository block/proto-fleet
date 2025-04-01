import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import ActionBar from ".";

describe("Action Bar", () => {
  const actionBar = "action-bar";

  const actionBarProps = {
    selectedMiners: ["MAC1"],
  };

  const minersText = "miners selected";

  test("renders action bar correctly", () => {
    const { getByTestId, queryByText } = render(
      <ActionBar {...actionBarProps} />,
    );

    const closeButton = getByTestId("close-button");
    expect(closeButton).toBeDefined();
    const minersElement = queryByText(minersText);
    expect(minersElement).toBeDefined();

    const deviceButton = getByTestId("device-widget-button");
    expect(deviceButton).toBeDefined();
    const performanceButton = getByTestId("performance-widget-button");
    expect(performanceButton).toBeDefined();
    const settingsButton = getByTestId("settings-widget-button");
    expect(settingsButton).toBeDefined();
  });

  test("renders action bar with correct number of miners", () => {
    const selectedMiners = ["MAC1", "MAC2", "MAC3"];
    const { getByText } = render(<ActionBar selectedMiners={selectedMiners} />);

    const element = getByText(selectedMiners.length + " miners selected");
    expect(element).toBeInTheDocument();
  });

  test("hides action bar when there are no miners", () => {
    let selectedMiners = ["MAC1"];
    const { getByTestId, queryByTestId, rerender } = render(
      <ActionBar selectedMiners={selectedMiners} />,
    );

    expect(getByTestId(actionBar)).toBeInTheDocument();

    selectedMiners = [];
    rerender(<ActionBar selectedMiners={selectedMiners} />);

    expect(queryByTestId(actionBar)).not.toBeInTheDocument();
  });

  test("closes action bar on click of close button", () => {
    const { getByTestId, queryByTestId } = render(
      <ActionBar {...actionBarProps} />,
    );

    expect(getByTestId(actionBar)).toBeInTheDocument();
    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(queryByTestId(actionBar)).not.toBeInTheDocument();
  });

  test("hides and displays action bar depending on confirmation dialog visibility", () => {
    const { getByTestId } = render(<ActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBar);
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
    const { getByTestId } = render(<ActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBar);
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
