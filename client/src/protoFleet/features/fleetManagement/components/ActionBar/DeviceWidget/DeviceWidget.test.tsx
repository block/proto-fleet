import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { deviceActions } from "./constants";
import DeviceWidget from ".";

describe("Device widget", () => {
  const deviceButton = "device-widget-button";
  const devicePopover = "device-widget-popover";
  const deviceDialog = "device-widget-dialog";

  const expectedActions = [
    deviceActions.blinkLEDs,
    deviceActions.downloadLogs,
    deviceActions.factoryReset,
    deviceActions.reboot,
    deviceActions.shutdown,
    deviceActions.wakeUp,
  ];
  const actionsWithConfirmation = [
    deviceActions.factoryReset,
    deviceActions.reboot,
    deviceActions.shutdown,
    deviceActions.wakeUp,
  ];
  const isActionWithConfirmation = (
    action: string,
  ): action is "factory-reset" | "reboot" | "shutdown" => {
    return actionsWithConfirmation.includes(action as any);
  };

  const deviceWidgetProps = {
    selectedMiners: ["MinerId"],
    setHidden: vi.fn(),
  };

  test("renders device widget with actions", () => {
    const { getByTestId } = render(<DeviceWidget {...deviceWidgetProps} />);
    const buttonElement = getByTestId(deviceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(devicePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      expect(getByTestId(action + "-popover-button")).toBeInTheDocument();
    }
  });

  test("hides popover on click of action", () => {
    const { getByTestId, queryByTestId } = render(
      <DeviceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(deviceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(devicePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      fireEvent.click(popoverButtonElement);
      expect(queryByTestId(devicePopover)).not.toBeInTheDocument();

      fireEvent.click(buttonElement);
    }
  });

  test("renders confirmation dialog when action requires confirmation", async () => {
    const { getByTestId, queryByTestId } = render(
      <DeviceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(deviceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(devicePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      fireEvent.click(popoverButtonElement);

      await waitFor(() => {
        if (isActionWithConfirmation(action)) {
          expect(getByTestId(deviceDialog)).toBeInTheDocument();

          expect(getByTestId("cancel-button")).toBeInTheDocument();
          const confirmButtonElement = getByTestId(action + "-confirm-button");
          expect(confirmButtonElement).toBeInTheDocument();
          fireEvent.click(confirmButtonElement);
        } else {
          expect(queryByTestId(deviceDialog)).not.toBeInTheDocument();
        }

        fireEvent.click(buttonElement);
      });
    }
  });

  test("confirmation dialog renders correct number of miners", async () => {
    const numberOfMiners = 12;
    const { getByTestId, getByText } = render(
      <DeviceWidget
        {...deviceWidgetProps}
        selectedMiners={Array(numberOfMiners).fill("MinerId")}
      />,
    );

    const buttonElement = getByTestId(deviceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(devicePopover)).toBeInTheDocument();

    const popoverButtonElement = getByTestId("factory-reset-popover-button");
    expect(popoverButtonElement).toBeInTheDocument();

    fireEvent.click(popoverButtonElement);

    await waitFor(() => {
      expect(getByTestId(deviceDialog)).toBeInTheDocument();
      const element = getByText(
        `Reset ${numberOfMiners} miners to factory default?`,
      );
      expect(element).toBeInTheDocument();
    });
  });

  test("confirmation dialog closes on confirmation or cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <DeviceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(deviceButton);
    fireEvent.click(buttonElement);

    for (const action of actionsWithConfirmation) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      // trigger action
      fireEvent.click(popoverButtonElement);

      expect(getByTestId(deviceDialog)).toBeInTheDocument();
      const confirmButtonElement = getByTestId(action + "-confirm-button");
      expect(confirmButtonElement).toBeInTheDocument();
      fireEvent.click(confirmButtonElement);

      await waitFor(() => {
        expect(queryByTestId(deviceDialog)).not.toBeInTheDocument();
      });

      // open popover and trigger action
      fireEvent.click(buttonElement);
      fireEvent.click(getByTestId(action + "-popover-button"));

      expect(getByTestId(deviceDialog)).toBeInTheDocument();
      const cancelButtonElement = getByTestId("cancel-button");
      expect(cancelButtonElement).toBeInTheDocument();
      fireEvent.click(cancelButtonElement);

      await waitFor(() => {
        expect(queryByTestId(deviceDialog)).not.toBeInTheDocument();
      });

      // open popover for next iteration
      fireEvent.click(buttonElement);
    }
  });
});
