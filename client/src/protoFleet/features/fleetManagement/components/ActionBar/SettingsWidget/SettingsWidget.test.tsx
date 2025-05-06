import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { settingsActions } from "./constants";
import SettingsWidget from "./SettingsWidget";

describe("Settings widget", () => {
  const settingsButton = "settings-widget-button";
  const settingsPopover = "settings-widget-popover";

  const expectedActions = [
    settingsActions.miningPool,
    settingsActions.coolingMode,
    settingsActions.security,
  ];

  const settingsWidgetProps = {
    numberOfMiners: 1,
    setHidden: vi.fn(),
  };

  test("renders settings widget with actions", () => {
    const { getByTestId } = render(<SettingsWidget {...settingsWidgetProps} />);
    const buttonElement = getByTestId(settingsButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(settingsPopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      expect(getByTestId(action + "-popover-button")).toBeInTheDocument();
    }
  });

  test("hides popover on click of action", () => {
    const { getByTestId, queryByTestId } = render(
      <SettingsWidget {...settingsWidgetProps} />,
    );
    const buttonElement = getByTestId(settingsButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(settingsPopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      fireEvent.click(popoverButtonElement);
      expect(queryByTestId(settingsPopover)).not.toBeInTheDocument();

      fireEvent.click(buttonElement);
    }
  });

  test("renders mining pools modal when mining pool action is clicked", () => {
    const numberOfMiners = 12;
    const { getByTestId, getByText } = render(
      <SettingsWidget
        {...settingsWidgetProps}
        numberOfMiners={numberOfMiners}
      />,
    );
    const buttonElement = getByTestId(settingsButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(settingsPopover)).toBeInTheDocument();

    const popoverButtonElement = getByTestId("mining-pool-popover-button");
    expect(popoverButtonElement).toBeInTheDocument();

    fireEvent.click(popoverButtonElement);

    expect(getByTestId("modal")).toBeInTheDocument();
    expect(
      getByText(`Update the mining pools for ${numberOfMiners} miners.`),
    ).toBeInTheDocument();
  });
});
