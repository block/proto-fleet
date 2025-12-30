import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import List from "@/shared/components/List/index";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import { testCols, testColTitles, TestItem, testItems } from "@/shared/components/List/mocks/data";
import { ListAction } from "@/shared/components/List/types";

beforeAll(() => {
  vi.mock("recharts", () => ({
    ResponsiveContainer: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-responsive-container">{children}</div>
    ),
    LineChart: ({ children }: { children: ReactNode }) => <div data-testid="recharts-line-chart">{children}</div>,
    ReferenceLine: () => <div data-testid="recharts-reference-line" />,
    Line: () => <div data-testid="recharts-line" />,
    XAxis: () => <div data-testid="recharts-xaxis" />,
    YAxis: () => <div data-testid="recharts-yaxis" />,
  }));
});

describe("List", () => {
  const activeCols = [testCols.name, testCols.status, testCols.value, testCols.timestamp] as (keyof TestItem)[];
  type TestItemKey = TestItem["id"];

  it("renders cols correctly", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    for (const col of activeCols) {
      expect(screen.getByText(testColTitles[col])).toBeInTheDocument();
    }
  });

  it("renders rows correctly", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    expect(screen.getAllByRole("row")).toHaveLength(testItems.length + 1);
  });

  it("does not render checkboxes when items are not selectable", () => {
    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable={false}
      />,
    );

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;
    expect(selectItemCheckboxes).toHaveLength(0);
  });

  it("selects all items when clicking select all checkbox", () => {
    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
      />,
    );
    const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // expect select all checkbox to be unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);

    // click individual item checkboxes and make sure select all checkbox is unchecked and total checked is only 1
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

    // click select all checkboxes and make sure all checkboxes are checked
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length);

    // click item 1 (deselect) checkbox and make select all checkbox unchecked
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length - 1);

    // click select all twice to deselect all items
    fireEvent.click(selectAllCheckbox);
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("renders action bar when items are selected and provides clearSelection callback", () => {
    const renderActionBar = vi.fn((_selectedItems: TestItemKey[], clearSelection: () => void) => (
      <div>
        <div>Action Bar</div>
        <button onClick={clearSelection} data-testid="clear-selection-btn">
          Clear
        </button>
      </div>
    ));

    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
        renderActionBar={renderActionBar}
      />,
    );

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // Select first item
    fireEvent.click(selectItemCheckboxes[0]);

    // Verify renderActionBar was called with selectedItems and clearSelection callback
    expect(renderActionBar).toHaveBeenCalled();
    const lastCall = renderActionBar.mock.calls[renderActionBar.mock.calls.length - 1];
    expect(lastCall[0]).toEqual([testItems[0].id]); // selectedItems
    expect(typeof lastCall[1]).toBe("function"); // clearSelection callback

    expect(screen.getByText("Action Bar")).toBeInTheDocument();

    // Click clear button
    const clearButton = screen.getByTestId("clear-selection-btn");
    fireEvent.click(clearButton);

    // Verify all checkboxes are now unchecked
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("clearSelection callback deselects all items", async () => {
    let clearSelectionCallback: (() => void) | null = null;

    const renderActionBar = vi.fn((_selectedItems: TestItemKey[], clearSelection: () => void) => {
      clearSelectionCallback = clearSelection;
      return <div>Action Bar</div>;
    });

    const { getByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        itemSelectable
        renderActionBar={renderActionBar}
      />,
    );

    const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // Select all items
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(testItems.length);

    // Call clearSelection callback
    expect(clearSelectionCallback).not.toBeNull();
    clearSelectionCallback!();

    // Wait for React to update the DOM
    await new Promise((resolve) => setTimeout(resolve, 0));

    // Verify all checkboxes are now unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(0);
  });

  it("renders actions popover and triggers the correct action", async () => {
    const mockAction = vi.fn();
    const actions = [
      { title: "Edit", actionHandler: mockAction },
      { title: "Delete", actionHandler: mockAction },
    ] as ListAction<TestItem>[];

    const { getAllByTestId } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        actions={actions}
      />,
    );

    const actionButton = getAllByTestId("list-actions-trigger")[0];
    fireEvent.click(actionButton);

    const editAction = screen.getByText(actions[0].title);
    fireEvent.click(editAction);

    expect(mockAction).toHaveBeenCalled();
    expect(mockAction).toHaveBeenCalledWith(testItems[0]);
  });

  describe("selection mode", () => {
    it("sets mode to 'all' when Select All is clicked without active filters", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("all");
    });

    it("sets mode to 'subset' when Select All is clicked with active filters", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={true}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'subset' when individual item is selected", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      fireEvent.click(selectItemCheckboxes[0]);

      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'none' when selection is cleared via Select All toggle", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select all
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Deselect all
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("none");
    });

    it("transitions from 'all' to 'subset' when individual item is deselected", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select all (mode = "all")
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Deselect one item (mode should transition to "subset")
      fireEvent.click(selectItemCheckboxes[0]);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("subset");
    });

    it("passes selectionMode to renderActionBar callback", () => {
      const renderActionBar = vi.fn(
        (_selectedItems: TestItemKey[], _clearSelection: () => void, selectionMode: string) => (
          <div data-testid="selection-mode">{selectionMode}</div>
        ),
      );

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          renderActionBar={renderActionBar}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(renderActionBar).toHaveBeenCalled();
      expect(getByTestId("selection-mode").textContent).toBe("all");
    });
  });

  describe("Shift+click range selection", () => {
    it("selects range of items when Shift+clicking after initial selection", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (normal click)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click third checkbox to select range (items 0, 1, 2)
      fireEvent.click(selectItemCheckboxes[2], { shiftKey: true });

      // All three items should be selected
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);
    });

    it("selects range in reverse order when Shift+clicking above initial selection", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click third checkbox first (normal click)
      fireEvent.click(selectItemCheckboxes[2]);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click first checkbox to select range (items 0, 1, 2)
      fireEvent.click(selectItemCheckboxes[0], { shiftKey: true });

      // All three items should be selected
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);
    });

    it("adds to existing selection when Shift+clicking with items already selected", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select first item
      fireEvent.click(selectItemCheckboxes[0]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Select fourth item separately
      fireEvent.click(selectItemCheckboxes[3]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(2);

      // Shift+click second item (should add items 1, 2 to selection since last click was on index 3)
      fireEvent.click(selectItemCheckboxes[1], { shiftKey: true });

      // Items 0, 1, 2, 3 should all be selected now
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(selectItemCheckboxes[3].checked).toBe(true);
    });

    it("clears anchor after Shift+click so next Shift+click requires new anchor", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (sets anchor at index 0)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(1);

      // Shift+click third checkbox (selects 0, 1, 2 and clears anchor)
      fireEvent.click(selectItemCheckboxes[2], { shiftKey: true });
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);

      // Another Shift+click should NOT do range selection since anchor was cleared
      // It should just select that single item (normal click behavior when no anchor)
      fireEvent.click(selectItemCheckboxes[3], { shiftKey: true });

      // Should only add item 3, not create a range from anywhere
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(4);
      expect(selectItemCheckboxes[3].checked).toBe(true);
    });

    it("does not select range when Shift+clicking to uncheck", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Select first three items individually
      fireEvent.click(selectItemCheckboxes[0]);
      fireEvent.click(selectItemCheckboxes[1]);
      fireEvent.click(selectItemCheckboxes[2]);
      expect(Array.from(selectItemCheckboxes).filter((c) => c.checked)).toHaveLength(3);

      // Shift+click to uncheck should only uncheck that item (no range deselection)
      fireEvent.click(selectItemCheckboxes[1], { shiftKey: true });

      // Only item 1 should be unchecked, items 0 and 2 remain checked
      expect(selectItemCheckboxes[0].checked).toBe(true);
      expect(selectItemCheckboxes[1].checked).toBe(false);
      expect(selectItemCheckboxes[2].checked).toBe(true);
    });

    it("sets selection mode to 'all' when Shift+click selects all items", () => {
      const onSelectionModeChange = vi.fn();
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click first checkbox (sets anchor at index 0)
      fireEvent.click(selectItemCheckboxes[0]);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("subset");

      // Shift+click last checkbox to select all items (0, 1, 2, 3, 4)
      fireEvent.click(selectItemCheckboxes[4], { shiftKey: true });

      // All items should be selected
      expect(Array.from(selectItemCheckboxes).every((c) => c.checked)).toBe(true);

      // Mode should be "all" since all items are selected
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");
    });
  });
});
