import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import PowerWidget from "./PowerWidget";

describe("Power Widget", () => {
  const buttonLabel = "Power";
  const PowerWidgetProps = {
    onReboot: vi.fn(),
    onSleep: vi.fn(),
    onWake: vi.fn(),
    miningStatus: {},
  };

  test("renders power widget popover with reboot and sleep if miner is running", () => {
    const { getByTestId } = render(
      <PowerWidget {...PowerWidgetProps} miningStatus={{ status: "Running" }} />
    );
    let { getByText, queryByText } = within(getByTestId("power-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);

    getByText = within(getByTestId("power-popover")).getByText;
    expect(getByTestId("power-popover")).toBeInTheDocument();
    expect(getByText("Reboot")).toBeInTheDocument();
    expect(getByText("Sleep")).toBeInTheDocument();
    expect(queryByText("Wake")).not.toBeInTheDocument();
  });

  test("renders power widget popover with reboot and wake if miner is stopped", () => {
    const { getByTestId } = render(
      <PowerWidget {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    let { getByText, queryByText } = within(getByTestId("power-widget"));
    const buttonElement = getByText(buttonLabel);
    fireEvent.click(buttonElement);

    getByText = within(getByTestId("power-popover")).getByText;
    expect(getByTestId("power-popover")).toBeInTheDocument();
    expect(getByText("Reboot")).toBeInTheDocument();
    expect(getByText("Wake")).toBeInTheDocument();
    expect(queryByText("Sleep")).not.toBeInTheDocument();
  });

  test("closes popover on click of reboot", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Reboot");
    fireEvent.click(buttonElement);
    expect(queryByTestId("power-popover")).not.toBeInTheDocument();
  });

  test("closes popover on click of sleep", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Sleep");
    fireEvent.click(buttonElement);
    expect(queryByTestId("power-popover")).not.toBeInTheDocument();
  });

  test("closes popover on click of wake", () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Wake");
    fireEvent.click(buttonElement);
    expect(queryByTestId("power-popover")).not.toBeInTheDocument();
  });

  test("shows confirmation dialog on click of reboot", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Reboot");
    fireEvent.click(buttonElement);
    expect(getByTestId("warn-reboot-dialog")).toBeInTheDocument();
  });

  test("shows confirmation dialog on click of sleep", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Sleep");
    fireEvent.click(buttonElement);
    expect(getByTestId("warn-sleep-dialog")).toBeInTheDocument();
  });

  test("shows confirmation dialog on click of wake", () => {
    const { getByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Wake");
    fireEvent.click(buttonElement);
    expect(getByTestId("warn-wake-dialog")).toBeInTheDocument();
  });

  test("closes the reboot confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Reboot");
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId("cancel-button");
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-reboot-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("power-popover")).not.toBeInTheDocument();
    });
  });

  test("closes the sleep confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Sleep");
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId("cancel-button");
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-sleep-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("power-popover")).not.toBeInTheDocument();
    });
  });

  test("closes the wake confirmation dialog on click of cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Wake");
    fireEvent.click(buttonElement);
    const cancelButtonElement = getByTestId("cancel-button");
    fireEvent.click(cancelButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-wake-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("power-popover")).not.toBeInTheDocument();
    });
  });

  test("shows rebooting dialog on confirming reboot", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Reboot");
    fireEvent.click(buttonElement);
    const rebootButtonElement = getByTestId("reboot-button");
    fireEvent.click(rebootButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-reboot-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("rebooting-dialog")).toBeInTheDocument();
    });
  });

  test("shows sleep dialog on confirming sleep", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Sleep");
    fireEvent.click(buttonElement);
    const sleepButtonElement = getByTestId("sleep-button");
    fireEvent.click(sleepButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-sleep-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("entering-sleep-dialog")).toBeInTheDocument();
    });
  });

  test("shows wake dialog on confirming wake", async () => {
    const { getByTestId, queryByTestId } = render(
      <PowerWidget shouldShowPopover {...PowerWidgetProps} miningStatus={{ status: "Stopped" }} />
    );
    const { getByText } = within(getByTestId("power-popover"));
    const buttonElement = getByText("Wake");
    fireEvent.click(buttonElement);
    const wakeButtonElement = getByTestId("wake-button");
    fireEvent.click(wakeButtonElement);
    await waitFor(() => {
      expect(queryByTestId("warn-wake-dialog")).not.toBeInTheDocument();
      expect(queryByTestId("waking-dialog")).toBeInTheDocument();
    });
  });
});
