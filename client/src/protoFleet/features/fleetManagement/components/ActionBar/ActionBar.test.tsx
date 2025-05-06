import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import DeviceWidget from "./DeviceWidget";
import ActionBar from ".";
import PerformanceWidget from "@/protoFleet/features/fleetManagement/components/ActionBar/PerformanceWidget";

describe("Action Bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedItems: ["MAC1"],
    renderActions: () => <div>Action</div>,
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

    const actionButton = queryByText("Action");
    expect(actionButton).toBeDefined();
  });

  test("renders action bar with correct number of miners", () => {
    const selectedItems = ["MAC1", "MAC2", "MAC3"];
    const { getByText } = render(
      <ActionBar {...actionBarProps} selectedItems={selectedItems} />,
    );

    const element = getByText(selectedItems.length + " miners selected");
    expect(element).toBeInTheDocument();
  });

  test("hides action bar when there are no miners", () => {
    let selectedItems = ["MAC1"];
    const { getByTestId, queryByTestId, rerender } = render(
      <ActionBar {...actionBarProps} selectedItems={selectedItems} />,
    );

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();

    selectedItems = [];
    rerender(<ActionBar {...actionBarProps} selectedItems={selectedItems} />);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("closes action bar on click of close button", () => {
    const { getByTestId, queryByTestId } = render(
      <ActionBar {...actionBarProps} />,
    );

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();
    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("renders all actions and calls setHidden method properly", async () => {
    const setHiddenMock = vi.fn();

    const { getByText, getByTestId } = render(
      <ActionBar
        {...actionBarProps}
        renderActions={(numberOfItems) => (
          <>
            <DeviceWidget
              numberOfMiners={numberOfItems}
              setHidden={setHiddenMock}
            />
            <PerformanceWidget
              numberOfMiners={numberOfItems}
              setHidden={setHiddenMock}
            />
          </>
        )}
      />,
    );

    const actionTexts = ["Device", "Performance"];
    actionTexts.forEach((title) => {
      expect(getByText(title)).toBeInTheDocument();
    });

    fireEvent.click(getByTestId("device-widget-button"));
    fireEvent.click(getByTestId("factory-reset-popover-button"));
    expect(setHiddenMock).toHaveBeenCalled();
  });
});
