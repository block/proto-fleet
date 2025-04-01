import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { performanceActions } from "./constants";
import PerformanceWidget from ".";

describe("Performance widget", () => {
  const performanceButton = "performance-widget-button";
  const performancePopover = "performance-widget-popover";
  const performanceDialog = "performance-widget-dialog";

  const expectedActions = [
    performanceActions.performanceMode,
    performanceActions.curtail,
  ];
  const actionsWithConfirmation = [performanceActions.curtail];

  const deviceWidgetProps = {
    numberOfMiners: 1,
    setHidden: vi.fn(),
  };

  test("renders device widget with actions", () => {
    const { getByTestId } = render(
      <PerformanceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(performanceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(performancePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      expect(getByTestId(action + "-popover-button")).toBeInTheDocument();
    }
  });

  test("hides popover on click of action", () => {
    const { getByTestId, queryByTestId } = render(
      <PerformanceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(performanceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(performancePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      fireEvent.click(popoverButtonElement);
      expect(queryByTestId(performancePopover)).not.toBeInTheDocument();

      fireEvent.click(buttonElement);
    }
  });

  test("renders confirmation dialog when action requires confirmation", async () => {
    const { getByTestId, queryByTestId } = render(
      <PerformanceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(performanceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(performancePopover)).toBeInTheDocument();
    for (const action of expectedActions) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      fireEvent.click(popoverButtonElement);

      await waitFor(() => {
        if (actionsWithConfirmation.includes(action)) {
          expect(getByTestId(performanceDialog)).toBeInTheDocument();

          expect(getByTestId("cancel-button")).toBeInTheDocument();
          const confirmButtonElement = getByTestId(action + "-confirm-button");
          expect(confirmButtonElement).toBeInTheDocument();
          fireEvent.click(confirmButtonElement);
        } else {
          expect(queryByTestId(performanceDialog)).not.toBeInTheDocument();
        }

        fireEvent.click(buttonElement);
      });
    }
  });

  test("confirmation dialog renders correct number of miners", async () => {
    const numberOfMiners = 12;
    const { getByTestId, getByText } = render(
      <PerformanceWidget
        {...deviceWidgetProps}
        numberOfMiners={numberOfMiners}
      />,
    );

    const buttonElement = getByTestId(performanceButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(performancePopover)).toBeInTheDocument();

    const popoverButtonElement = getByTestId("curtail-popover-button");
    expect(popoverButtonElement).toBeInTheDocument();

    fireEvent.click(popoverButtonElement);

    await waitFor(() => {
      expect(getByTestId(performanceDialog)).toBeInTheDocument();
      const element = getByText(`Curtail ${numberOfMiners} miners?`);
      expect(element).toBeInTheDocument();
    });
  });

  test("confirmation dialog closes on confirmation or cancel", async () => {
    const { getByTestId, queryByTestId } = render(
      <PerformanceWidget {...deviceWidgetProps} />,
    );
    const buttonElement = getByTestId(performanceButton);
    fireEvent.click(buttonElement);

    for (const action of actionsWithConfirmation) {
      const popoverButtonElement = getByTestId(action + "-popover-button");
      expect(popoverButtonElement).toBeInTheDocument();

      // trigger action
      fireEvent.click(popoverButtonElement);

      expect(getByTestId(performanceDialog)).toBeInTheDocument();
      const confirmButtonElement = getByTestId(action + "-confirm-button");
      expect(confirmButtonElement).toBeInTheDocument();
      fireEvent.click(confirmButtonElement);

      await waitFor(() => {
        expect(queryByTestId(performanceDialog)).not.toBeInTheDocument();
      });

      // open popover and trigger action
      fireEvent.click(buttonElement);
      fireEvent.click(getByTestId(action + "-popover-button"));

      expect(getByTestId(performanceDialog)).toBeInTheDocument();
      const cancelButtonElement = getByTestId("cancel-button");
      expect(cancelButtonElement).toBeInTheDocument();
      fireEvent.click(cancelButtonElement);

      await waitFor(() => {
        expect(queryByTestId(performanceDialog)).not.toBeInTheDocument();
      });

      // open popover for next iteration
      fireEvent.click(buttonElement);
    }
  });
});
