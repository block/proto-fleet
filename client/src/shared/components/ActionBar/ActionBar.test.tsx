import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import ActionBar from ".";
describe("Action Bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedItems: ["MAC1"],
    renderActions: () => <div>Action</div>,
  };

  test("renders action bar with correct number of miners", () => {
    const selectedMiners = ["MAC1", "MAC2", "MAC3"];
    const { getByText } = render(<ActionBar {...actionBarProps} selectedItems={selectedMiners} />);

    const element = getByText(selectedMiners.length + " miners selected");
    expect(element).toBeInTheDocument();
  });

  test("reads 'All N … selected' in all mode using totalCount", () => {
    const { getByText } = render(<ActionBar {...actionBarProps} selectionMode="all" totalCount={42} />);

    expect(getByText("All 42 miners selected")).toBeInTheDocument();
  });

  test("reads 'All N … matching filters selected' in all mode when filters are active", () => {
    const { getByText } = render(<ActionBar {...actionBarProps} selectionMode="all" totalCount={7} filtersActive />);

    expect(getByText("All 7 miners matching filters selected")).toBeInTheDocument();
  });

  test("hides action bar when there are no miners", () => {
    let selectedMiners = ["MAC1"];
    const { getByTestId, queryByTestId, rerender } = render(
      <ActionBar {...actionBarProps} selectedItems={selectedMiners} />,
    );

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();

    selectedMiners = [];
    rerender(<ActionBar {...actionBarProps} selectedItems={selectedMiners} />);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("closes action bar on click of close button", () => {
    const { getByTestId, queryByTestId } = render(<ActionBar {...actionBarProps} />);

    expect(getByTestId(actionBarTestId)).toBeInTheDocument();
    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(queryByTestId(actionBarTestId)).not.toBeInTheDocument();
  });

  test("lets action content hide the bar", () => {
    render(
      <ActionBar
        selectedItems={["item-1"]}
        renderActions={(setHidden) => <button onClick={() => setHidden(true)}>Run action</button>}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Run action" }));
    expect(screen.getByTestId("action-bar")).toHaveClass("invisible");
  });

  test("calls onClose callback when close button is clicked", () => {
    const onCloseMock = vi.fn();
    const { getByTestId } = render(<ActionBar {...actionBarProps} onClose={onCloseMock} />);

    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(onCloseMock).toHaveBeenCalledOnce();
  });

  test("renders selection controls only once", () => {
    const onSelectAll = vi.fn();

    render(
      <ActionBar
        {...actionBarProps}
        selectionControls={
          <button type="button" data-testid="select-all-control" onClick={onSelectAll}>
            Select all
          </button>
        }
      />,
    );

    const controls = screen.getAllByTestId("select-all-control");
    expect(controls).toHaveLength(1);

    fireEvent.click(controls[0]);
    expect(onSelectAll).toHaveBeenCalledOnce();
  });
});
