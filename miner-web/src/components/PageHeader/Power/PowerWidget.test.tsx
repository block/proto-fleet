import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PowerWidget from "./PowerWidget";

describe("Power Widget", () => {
  const powerButton = "power-button";
  const powerPopover = "power-popover";
  const popoverRebootButton = "popover-reboot-button";
  const popoverSleepButton = "popover-sleep-button";
  const popoverWakeUpButton = "popover-wake-up-button";
  const cancelButton = "cancel-button";
  const rebootButton = "reboot-button";
  const sleepButton = "sleep-button";
  const wakeUpButton = "wake-up-button";
  const warnRebootDialog = "warn-reboot-dialog";
  const warnSleepDialog = "warn-sleep-dialog";
  const warnWakeUpDialog = "warn-wake-up-dialog";
  const rebootingDialog = "rebooting-dialog";
  const enteringSleepDialog = "entering-sleep-dialog";
  const wakingDialog = "waking-dialog";

  const PowerWidgetProps = {
    onReboot: vi.fn(),
    onSleep: vi.fn(),
    onWake: vi.fn(),
    miningStatus: {},
  };

  test("renders power widget popover with reboot and sleep if miner is running", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget {...PowerWidgetProps} miningStatus={{ status: "Running" }} />
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
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });

  test("shows confirmation dialog on click of reboot", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverRebootButton);
    fireEvent.click(buttonElement);
    expect(getByTestId(warnRebootDialog)).toBeInTheDocument();
  });

  test("shows confirmation dialog on click of sleep", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverSleepButton);
    fireEvent.click(buttonElement);
    expect(getByTestId(warnSleepDialog)).toBeInTheDocument();
  });

  test("shows confirmation dialog on click of wake up", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    expect(getByTestId(warnWakeUpDialog)).toBeInTheDocument();
  });

  test("closes the reboot confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverRebootButton);
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId(cancelButton);
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnRebootDialog)).not.toBeInTheDocument();
      expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
    });
  });

  test("closes the sleep confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverSleepButton);
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId(cancelButton);
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnSleepDialog)).not.toBeInTheDocument();
      expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
    });
  });

  test("closes the wake up confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId(cancelButton);
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnWakeUpDialog)).not.toBeInTheDocument();
      expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
    });
  });

  test("shows rebooting dialog on confirming reboot", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverRebootButton);
    fireEvent.click(buttonElement);
    const rebootButtonElement = getByTestId(rebootButton);
    fireEvent.click(rebootButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnRebootDialog)).not.toBeInTheDocument();
      expect(queryByTestId(rebootingDialog)).toBeInTheDocument();
    });
  });

  test("shows sleep dialog on confirming sleep", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const buttonElement = getByTestId(popoverSleepButton);
    fireEvent.click(buttonElement);
    const sleepButtonElement = getByTestId(sleepButton);
    fireEvent.click(sleepButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnSleepDialog)).not.toBeInTheDocument();
      expect(queryByTestId(enteringSleepDialog)).toBeInTheDocument();
    });
  });

  test("shows wake up dialog on confirming wake up", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    const wakeButtonElement = getByTestId(wakeUpButton);
    fireEvent.click(wakeButtonElement);
    await waitFor(() => {
      expect(queryByTestId(warnWakeUpDialog)).not.toBeInTheDocument();
      expect(queryByTestId(wakingDialog)).toBeInTheDocument();
    });
  });
});
