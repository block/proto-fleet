import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PowerWidget from "./PowerWidget";

describe("Power Widget", () => {
  const powerButton = "power-button";
  const powerPopover = "power-popover";
  const popoverRebootButton = "popover-reboot-button";
  const popoverSleepButton = "popover-sleep-button";
  const popoverWakeUpButton = "popover-wake-up-button";

  const PowerWidgetProps = {
    onReboot: vi.fn(),
    onSleep: vi.fn(),
    onWake: vi.fn(),
    miningStatus: {},
  };

  test("renders power widget popover with reboot and sleep if miner is running", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget {...PowerWidgetProps} miningStatus={{ status: "Mining" }} />
    );
    const buttonElement = getByTestId(powerButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(powerPopover)).toBeInTheDocument();
    expect(queryByTestId(popoverRebootButton)).toBeInTheDocument();
    expect(queryByTestId(popoverSleepButton)).toBeInTheDocument();
    expect(queryByTestId(popoverWakeUpButton)).not.toBeInTheDocument();
  });

  test("renders power widget popover with reboot and wake up if miner is stopped", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const buttonElement = getByTestId(powerButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(powerPopover)).toBeInTheDocument();
    expect(getByTestId(popoverRebootButton)).toBeInTheDocument();
    expect(getByTestId(popoverWakeUpButton)).toBeInTheDocument();
    expect(queryByTestId(popoverSleepButton)).not.toBeInTheDocument();
  });

  test("closes popover on click of reboot", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverRebootButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });

  test("closes popover on click of sleep", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverSleepButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });

  test("closes popover on click of wake up", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget
        shouldShowPopover
        {...PowerWidgetProps}
        miningStatus={{ status: "Stopped" }}
      />
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });
});
