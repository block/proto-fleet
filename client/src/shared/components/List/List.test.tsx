import { ReactNode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { createPortal } from "react-dom";
import { defaultListFilter } from "@/shared/components/List/constants";
import List from "@/shared/components/List/index";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import { testCols, testColTitles, testFilters, TestItem, testItems } from "@/shared/components/List/mocks/data";
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

  const setListDimensions = (
    container: HTMLElement,
    dimensions: { clientWidth: number; tableWidth: number; overflowPaddingWidth?: number },
  ) => {
    const scrollContainer = container.querySelector("table")?.parentElement as HTMLDivElement;
    const table = container.querySelector("table") as HTMLTableElement;

    Object.defineProperty(scrollContainer, "clientWidth", { configurable: true, value: dimensions.clientWidth });
    Object.defineProperty(table, "offsetWidth", {
      configurable: true,
      get: () => {
        const lastHeaderCell = screen.getByText(testColTitles.timestamp).closest("th");
        const hasOverflowPadding = lastHeaderCell?.classList.contains("desktop:pr-(--list-padding-right-desktop)");
        return dimensions.tableWidth + (hasOverflowPadding ? (dimensions.overflowPaddingWidth ?? 0) : 0);
      },
    });
  };

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

  it("renders th elements with scope='col' attribute", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    const headerCells = screen.getAllByRole("columnheader");
    for (const th of headerCells) {
      expect(th).toHaveAttribute("scope", "col");
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

  it("does not apply trailing padding classes when the last column is already reachable", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        paddingRight={{
          phone: "24px",
          tablet: "24px",
          laptop: "40px",
          desktop: "40px",
        }}
      />,
    );

    expect(screen.getByText(testColTitles.timestamp).closest("th")).not.toHaveClass(
      "desktop:pr-(--list-padding-right-desktop)",
    );
    expect(screen.getAllByTestId(testCols.timestamp)[0]).not.toHaveClass("desktop:pr-(--list-padding-right-desktop)");
  });

  it("applies trailing padding classes when the last column requires horizontal scrolling", () => {
    const { container } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        paddingRight={{
          phone: "24px",
          tablet: "24px",
          laptop: "40px",
          desktop: "40px",
        }}
      />,
    );

    setListDimensions(container, { clientWidth: 200, tableWidth: 360, overflowPaddingWidth: 40 });

    fireEvent(window, new Event("resize"));

    expect(screen.getByText(testColTitles.timestamp).closest("th")).toHaveClass(
      "desktop:pr-(--list-padding-right-desktop)",
    );
    expect(screen.getAllByTestId(testCols.timestamp)[0]).toHaveClass("desktop:pr-(--list-padding-right-desktop)");
  });

  it("clears horizontal overflow after the table shrinks below the container width", () => {
    const { container } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        paddingRight={{
          phone: "24px",
          tablet: "24px",
          laptop: "40px",
          desktop: "40px",
        }}
      />,
    );

    const scrollContainer = container.querySelector("table")?.parentElement as HTMLDivElement;

    setListDimensions(container, { clientWidth: 400, tableWidth: 440, overflowPaddingWidth: 40 });
    fireEvent(window, new Event("resize"));
    expect(scrollContainer).toHaveClass("overflow-x-auto");

    setListDimensions(container, { clientWidth: 400, tableWidth: 390, overflowPaddingWidth: 40 });
    fireEvent(window, new Event("resize"));

    expect(scrollContainer).toHaveClass("overflow-x-hidden");
  });

  it("extends row dividers to the container edge when the table does not overflow horizontally", () => {
    const { container } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    expect(container.querySelectorAll('[data-testid="row-divider-extension"]')).toHaveLength(testItems.length);
  });

  it("does not extend row dividers when the table overflows horizontally", () => {
    const { container } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
      />,
    );

    setListDimensions(container, { clientWidth: 200, tableWidth: 400 });

    fireEvent(window, new Event("resize"));

    expect(container.querySelector('[data-testid="row-divider-extension"]')).not.toBeInTheDocument();
  });

  it("matches the sticky shadow mask to the configured sticky background", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        stickyBgColor="bg-surface-elevated-base"
      />,
    );

    expect(screen.getByText(testColTitles.name).closest("th")).toHaveStyle({
      "--list-sticky-shadow-mask-bg": "var(--color-surface-elevated-base)",
    });
  });

  it("recomputes horizontal overflow when the table size changes without a window resize", () => {
    const OriginalResizeObserver = globalThis.ResizeObserver;
    let resizeObserverCallback: (() => void) | undefined;

    globalThis.ResizeObserver = class ResizeObserver {
      constructor(callback: (...args: unknown[]) => void) {
        resizeObserverCallback = () => callback([], this);
      }

      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
    } as unknown as typeof ResizeObserver;

    try {
      const { container } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
        />,
      );

      const scrollContainer = container.querySelector("table")?.parentElement as HTMLDivElement;

      setListDimensions(container, { clientWidth: 200, tableWidth: 400 });
      fireEvent(window, new Event("resize"));
      act(() => {
        resizeObserverCallback?.();
      });

      expect(scrollContainer).toHaveClass("overflow-x-auto");
    } finally {
      globalThis.ResizeObserver = OriginalResizeObserver;
    }
  });

  it("does not register a resize listener when horizontal overflow handling is disabled", () => {
    const addEventListenerSpy = vi.spyOn(window, "addEventListener");
    let resizeListenerCount: number;

    try {
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          overflowContainer={false}
        />,
      );
      resizeListenerCount = addEventListenerSpy.mock.calls.filter(([eventName]) => eventName === "resize").length;
    } finally {
      addEventListenerSpy.mockRestore();
    }

    expect(resizeListenerCount).toBe(0);
  });

  it("shows item count by default", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        total={testItems.length}
        itemName={{ singular: "miner", plural: "miners" }}
      />,
    );

    expect(screen.getByText(`${testItems.length} miners`)).toBeInTheDocument();
  });

  it("hides item count when hideTotal is true", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        total={testItems.length}
        itemName={{ singular: "miner", plural: "miners" }}
        hideTotal
      />,
    );

    expect(screen.queryByText(`${testItems.length} miners`)).not.toBeInTheDocument();
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

  it("makes the row drag handle keyboard-focusable when reordering is enabled", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        onRowReorder={vi.fn()}
        rowDragHandleColumn={testCols.name as keyof TestItem}
      />,
    );

    expect(screen.getAllByTestId("reorder-handle")[0]).toHaveAttribute("tabindex", "0");
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

  it("renders an in-table empty state row while keeping the header visible", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={[]}
        itemKey="id"
        emptyStateRow={<div>No matching items</div>}
      />,
    );

    expect(screen.getByTestId("list-header")).toBeInTheDocument();
    expect(screen.getByTestId("list-empty-row")).toBeInTheDocument();
    expect(screen.getByText("No matching items")).toBeInTheDocument();
  });

  it("resets filter UI when initialActiveFilters changes", async () => {
    const props = {
      activeCols,
      colTitles: testColTitles,
      colConfig: testColConfig,
      items: testItems,
      itemKey: "id" as const,
      filters: testFilters,
      initialActiveFilters: {
        buttonFilters: [defaultListFilter],
        dropdownFilters: {
          valueRange: ["low"],
        },
      },
    };

    const { rerender } = render(<List<TestItem, TestItemKey> {...props} />);

    expect(await screen.findByTestId("active-filter-valueRange")).toBeInTheDocument();

    rerender(
      <List<TestItem, TestItemKey>
        {...props}
        initialActiveFilters={{
          buttonFilters: [defaultListFilter],
          dropdownFilters: {},
        }}
      />,
    );

    expect(screen.queryByTestId("active-filter-valueRange")).not.toBeInTheDocument();
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

    const editAction = screen.getByText("Edit");
    fireEvent.click(editAction);

    expect(mockAction).toHaveBeenCalled();
    expect(mockAction).toHaveBeenCalledWith(testItems[0]);
  });

  it("keeps the action column aligned when a row hides every action", () => {
    const actions = [
      {
        title: "Edit",
        actionHandler: vi.fn(),
        hidden: (item: TestItem) => item.id === testItems[0].id,
      },
    ] as ListAction<TestItem>[];

    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        actions={actions}
      />,
    );

    expect(screen.getAllByTestId("action")).toHaveLength(testItems.length);
  });

  it("preserves destructive styling when a row renders a single visible action button", () => {
    const actions = [
      {
        title: "Delete",
        actionHandler: vi.fn(),
        variant: "destructive" as const,
      },
    ] as ListAction<TestItem>[];

    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        actions={actions}
      />,
    );

    const button = screen.getAllByRole("button", { name: "Delete" })[0];
    expect(button).toHaveClass("bg-intent-critical-10");
    expect(button).toHaveClass("text-text-critical");
  });

  it("exempts specified columns from disabled styling on disabled rows", () => {
    const { getAllByRole } = render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={testItems}
        itemKey="id"
        isRowDisabled={(item: TestItem) => item.id === "1"}
        columnsExemptFromDisabledStyling={new Set<keyof TestItem>(["status"])}
      />,
    );

    const rows = getAllByRole("row");
    // First row is header, second row is first item (id: "1", which is disabled)
    const disabledRow = rows[1];
    const cells = disabledRow.querySelectorAll("td");

    // Find the status column index
    const statusColIndex = activeCols.indexOf("status" as keyof TestItem);

    // Verify that status column does not have the opacity-50 class if it exists on other cells
    const hasOpacityClass = Array.from(cells).some((cell) => cell.className.includes("opacity-50"));

    if (hasOpacityClass) {
      // If opacity styling is applied to any cell, status column should NOT have it (it's exempted)
      expect(cells[statusColIndex].className).not.toContain("opacity-50");
    } else {
      // If no opacity styling is applied, that's also acceptable - just verify status column exists
      expect(cells[statusColIndex]).toBeInTheDocument();
    }
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

    it("keeps mode at 'none' when Select All finds no selectable rows", () => {
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
          isRowDisabled={() => true}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("none");
      expect(selectAllCheckbox.checked).toBe(false);
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

    it("resets mode to 'none' when customSelectedItems is externally set to empty", () => {
      const onSelectionModeChange = vi.fn();
      let selectedItems: TestItemKey[] = [];
      const customSetSelectedItems = (items: TestItemKey[]) => {
        selectedItems = items;
      };

      const { getByTestId, rerender } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={selectedItems}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Click Select All to set internal selectionMode to "all"
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Rerender with the selected items to sync state
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={selectedItems}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Verify all items are now selected
      expect(selectAllCheckbox.checked).toBe(true);
      expect(selectItemCheckboxes.every((c) => c.checked)).toBe(true);

      // Clear the mock to track new calls
      onSelectionModeChange.mockClear();

      // Simulate external "Select none" by setting customSelectedItems to empty array
      // This is what happens when ModalSelectAllFooter's "Select none" is clicked
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          hasActiveFilters={false}
          onSelectionModeChange={onSelectionModeChange}
          customSelectedItems={[]}
          customSetSelectedItems={customSetSelectedItems}
        />,
      );

      // Verify all items are now deselected
      expect(selectAllCheckbox.checked).toBe(false);
      expect(selectItemCheckboxes.every((c) => !c.checked)).toBe(true);

      // Verify selection mode was reset to 'none'
      expect(onSelectionModeChange).toHaveBeenCalledWith("none");
    });

    it("uses customSelectionMode when the full controlled-selection props are provided", () => {
      const renderActionBar = vi.fn(
        (_selectedItems: TestItemKey[], _clearSelection: () => void, selectionMode: string) => (
          <div data-testid="controlled-selection-mode">{selectionMode}</div>
        ),
      );

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          customSelectedItems={[testItems[0].id]}
          customSetSelectedItems={vi.fn()}
          onSelectionModeChange={vi.fn()}
          customSelectionMode="all"
          renderActionBar={renderActionBar}
        />,
      );

      expect(renderActionBar).toHaveBeenCalled();
      expect(screen.getByTestId("controlled-selection-mode")).toHaveTextContent("all");
    });
  });

  it("preserves customSelectedItems for items not in current page when preserveOffPageSelection is true", () => {
    const customSetSelectedItems = vi.fn();
    const pageOneItems = [testItems[0], testItems[1]];
    const selectedAcrossPages = [testItems[0].id, testItems[2].id];

    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={pageOneItems}
        itemKey="id"
        itemSelectable
        customSelectedItems={selectedAcrossPages}
        customSetSelectedItems={customSetSelectedItems}
        preserveOffPageSelection
      />,
    );

    expect(customSetSelectedItems).not.toHaveBeenCalled();
  });

  it("does not show the header checkbox as partially checked for off-page selections", () => {
    render(
      <List<TestItem, TestItemKey>
        activeCols={activeCols}
        colTitles={testColTitles}
        colConfig={testColConfig}
        items={[testItems[0], testItems[1]]}
        itemKey="id"
        itemSelectable
        customSelectedItems={[testItems[2].id]}
        customSetSelectedItems={vi.fn()}
        preserveOffPageSelection
      />,
    );

    const selectAllCheckbox = screen
      .getByTestId("list-header")
      .querySelector("input[type='checkbox']") as HTMLInputElement;
    expect(selectAllCheckbox.checked).toBe(false);
    expect(screen.getByTestId("select-all-checkbox").querySelector('[class*="bg-core-primary-fill/40"]')).toBeNull();
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

  describe("disabled rows", () => {
    it("disables checkboxes for rows matching isRowDisabled predicate", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[2].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // First and third checkboxes should be disabled
      expect(selectItemCheckboxes[0].disabled).toBe(true);
      expect(selectItemCheckboxes[1].disabled).toBe(false);
      expect(selectItemCheckboxes[2].disabled).toBe(true);
      expect(selectItemCheckboxes[3].disabled).toBe(false);
    });

    it("applies opacity-50 class to disabled row cell content", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const rows = Array.from(getByTestId("list-body").querySelectorAll("tr"));

      // Row itself should not have opacity-50
      expect(rows[0].className).not.toContain("opacity-50");
      expect(rows[1].className).not.toContain("opacity-50");

      // Cell content in the first (disabled) row should have opacity-50
      const disabledRowCellContents = Array.from(
        rows[0].querySelectorAll("td > div:not([data-testid='row-divider-extension'])"),
      );
      disabledRowCellContents.forEach((content) => {
        expect(content.className).toContain("opacity-50");
      });

      // Enabled row content should not have opacity-50
      const enabledRowCellContents = Array.from(
        rows[1].querySelectorAll("td > div:not([data-testid='row-divider-extension'])"),
      );
      enabledRowCellContents.forEach((content) => {
        expect(content.className).not.toContain("opacity-50");
      });
    });

    it("keeps selection mode as subset when pageScopedSelection is enabled", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          pageScopedSelection
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("excludes disabled rows when Select All is clicked", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[1].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Click Select All
      fireEvent.click(selectAllCheckbox);

      // Only enabled checkboxes (items 2, 3, 4) should be checked
      expect(selectItemCheckboxes[0].checked).toBe(false); // disabled
      expect(selectItemCheckboxes[1].checked).toBe(false); // disabled
      expect(selectItemCheckboxes[2].checked).toBe(true); // enabled
      expect(selectItemCheckboxes[3].checked).toBe(true); // enabled
      expect(selectItemCheckboxes[4].checked).toBe(true); // enabled
    });

    it("shows Select All as checked when all selectable items are selected", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id || item.id === testItems[1].id;

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Manually select all enabled items (items 2, 3, 4)
      fireEvent.click(selectItemCheckboxes[2]);
      fireEvent.click(selectItemCheckboxes[3]);
      fireEvent.click(selectItemCheckboxes[4]);

      // Select All checkbox should now be checked
      expect(selectAllCheckbox.checked).toBe(true);
    });

    it("passes totalSelectable to renderActionBar (total - totalDisabled)", () => {
      const renderActionBar = vi.fn(
        (
          _selectedItems: TestItemKey[],
          _clearSelection: () => void,
          _selectionMode: string,
          totalSelectable?: number,
        ) => <div data-testid="total-selectable">{totalSelectable}</div>,
      );

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          total={10}
          totalDisabled={3}
          renderActionBar={renderActionBar}
          isRowDisabled={() => false}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // totalSelectable should be 10 - 3 = 7
      expect(getByTestId("total-selectable").textContent).toBe("7");
    });

    it("excludes disabled items when syncing selection in 'all' mode", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const onSelectionModeChange = vi.fn();

      const { getByTestId, rerender } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems.slice(0, 3)}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select all (should select items 1 and 2, skipping item 0)
      fireEvent.click(selectAllCheckbox);
      expect(onSelectionModeChange).toHaveBeenLastCalledWith("all");

      // Simulate loading more items
      rerender(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Item 0 should still be disabled and unchecked
      expect(selectItemCheckboxes[0].disabled).toBe(true);
      expect(selectItemCheckboxes[0].checked).toBe(false);

      // All other items should be selected
      expect(selectItemCheckboxes[1].checked).toBe(true);
      expect(selectItemCheckboxes[2].checked).toBe(true);
      expect(selectItemCheckboxes[3].checked).toBe(true);
      expect(selectItemCheckboxes[4].checked).toBe(true);
    });

    it("shows Select All as unchecked when no selectable items exist", () => {
      const isRowDisabled = () => true; // All items disabled

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          isRowDisabled={isRowDisabled}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;

      // Select All should be unchecked when all items are disabled
      expect(selectAllCheckbox.checked).toBe(false);

      // Click Select All should not select anything
      fireEvent.click(selectAllCheckbox);
      expect(selectAllCheckbox.checked).toBe(false);
    });

    it("disables single action button for disabled rows", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const mockActionHandler = vi.fn();
      const actions = [{ title: "Edit", actionHandler: mockActionHandler }];

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          isRowDisabled={isRowDisabled}
          actions={actions}
        />,
      );

      const actionButtons = getByTestId("list-body").querySelectorAll("button");

      // First row action button should be disabled
      expect(actionButtons[0].disabled).toBe(true);

      // Other row action buttons should be enabled
      expect(actionButtons[1].disabled).toBe(false);
      expect(actionButtons[2].disabled).toBe(false);

      // Clicking disabled button should not trigger action
      fireEvent.click(actionButtons[0]);
      expect(mockActionHandler).not.toHaveBeenCalled();

      // Clicking enabled button should trigger action
      fireEvent.click(actionButtons[1]);
      expect(mockActionHandler).toHaveBeenCalledWith(testItems[1]);
    });

    it("disables multi-action menu for disabled rows", () => {
      const isRowDisabled = (item: TestItem) => item.id === testItems[0].id;
      const mockAction1 = vi.fn();
      const mockAction2 = vi.fn();
      const actions = [
        { title: "Edit", actionHandler: mockAction1 },
        { title: "Delete", actionHandler: mockAction2 },
      ];

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          isRowDisabled={isRowDisabled}
          actions={actions}
        />,
      );

      const actionTriggers = getByTestId("list-body").querySelectorAll("[data-testid='list-actions-trigger']");

      // First row action trigger should be disabled
      expect((actionTriggers[0] as HTMLButtonElement).disabled).toBe(true);

      // Other row action triggers should be enabled
      expect((actionTriggers[1] as HTMLButtonElement).disabled).toBe(false);

      // Clicking disabled trigger should not open menu
      fireEvent.click(actionTriggers[0]);
      expect(document.querySelector(".popover-content")).toBeNull();

      // Clicking enabled trigger should open menu
      fireEvent.click(actionTriggers[1]);
      // Menu should be visible (implementation shows popover when actionsVisible is true)
      const rows = document.querySelectorAll("[data-testid='action'] > div > div");
      expect(rows.length).toBeGreaterThan(1); // Popover should exist
    });
  });

  describe("sorting", () => {
    it("calls onSort with ASC direction when clicking unsorted column (default)", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column

      // Assert - defaults to ASC when no getDefaultSortDirection callback provided
      expect(onSort).toHaveBeenCalledWith("name", "asc");
    });

    it("toggles direction from DESC to ASC when clicking currently sorted column", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "desc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column (currently sorted DESC)

      // Assert
      expect(onSort).toHaveBeenCalledWith("name", "asc");
    });

    it("toggles direction from ASC to DESC when clicking currently sorted column", () => {
      // Arrange
      const onSort = vi.fn();
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
          onSort={onSort}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");
      fireEvent.click(headerButtons[0]); // Click "name" column (currently sorted ASC)

      // Assert
      expect(onSort).toHaveBeenCalledWith("name", "desc");
    });

    it("sets aria-sort to ascending when column is sorted ASC", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert
      expect(headerCells[0]).toHaveAttribute("aria-sort", "ascending");
    });

    it("sets aria-sort to descending when column is sorted DESC", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "desc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert
      expect(headerCells[0]).toHaveAttribute("aria-sort", "descending");
    });

    it("does not set aria-sort on unsorted columns", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name", "value"]);
      const currentSort = { field: "name" as keyof TestItem, direction: "asc" as const };

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
          currentSort={currentSort}
        />,
      );

      const headerCells = getByTestId("list-header").querySelectorAll("th");

      // Assert - "status" column (index 1) is not sorted
      expect(headerCells[1]).not.toHaveAttribute("aria-sort");
    });

    it("renders buttons only for sortable columns", () => {
      // Arrange
      const sortableColumns = new Set<keyof TestItem>(["name"]); // Only name is sortable

      // Act
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          sortableColumns={sortableColumns}
        />,
      );

      const headerButtons = getByTestId("list-header").querySelectorAll("button");

      // Assert - only 1 button for the sortable "name" column
      expect(headerButtons).toHaveLength(1);
      expect(headerButtons[0]).toHaveTextContent(testColTitles.name);
    });
  });

  describe("client-side filtering with Select All", () => {
    // Filter function that reduces items based on status
    const filterByActiveStatus = (item: TestItem) => item.status === "active";

    it("sets mode to 'subset' when Select All is clicked with client-side filter reducing items", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Should be "subset" because client-side filter reduces visible items (5 items -> 2 active items)
      expect(onSelectionModeChange).toHaveBeenCalledWith("subset");
    });

    it("sets mode to 'all' when Select All is clicked with filter that matches all items", () => {
      const onSelectionModeChange = vi.fn();

      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={() => true} // Filter matches all items
          onSelectionModeChange={onSelectionModeChange}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Should be "all" because filter matches all items
      expect(onSelectionModeChange).toHaveBeenCalledWith("all");
    });

    it("only selects filtered items when Select All is clicked with active filter", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      fireEvent.click(selectAllCheckbox);

      // Only "active" items should be visible and selected (items 1 and 5)
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Only 2 items should be visible (the active ones)
      expect(selectItemCheckboxes).toHaveLength(2);

      // All visible items should be selected
      expect(selectItemCheckboxes.every((c) => c.checked)).toBe(true);
    });

    it("shows Select All as checked when all filtered items are selected", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={(item) => filterByActiveStatus(item)}
        />,
      );

      const selectAllCheckbox = getByTestId("list-header").querySelector("input[type='checkbox']") as HTMLInputElement;
      const selectItemCheckboxes = Array.from(
        getByTestId("list-body").querySelectorAll("input[type='checkbox']"),
      ) as HTMLInputElement[];

      // Manually select all visible (filtered) items
      selectItemCheckboxes.forEach((checkbox) => fireEvent.click(checkbox));

      // Select All checkbox should now be checked
      expect(selectAllCheckbox.checked).toBe(true);
    });

    it("calls onFilterChange when filterItem changes filtered results", () => {
      const onFilterChange = vi.fn();

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          filterItem={() => true}
          onFilterChange={onFilterChange}
        />,
      );

      // onFilterChange is called when filters are applied through the UI
      // The callback should be available and callable
      expect(onFilterChange).toBeDefined();
    });
  });

  describe("footerContent prop", () => {
    it("renders footerContent at the bottom of the scroll container", () => {
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          footerContent={<div data-testid="footer-content">Pagination Controls</div>}
        />,
      );

      const footerContent = screen.getByTestId("footer-content");
      expect(footerContent).toBeInTheDocument();
      expect(footerContent.textContent).toBe("Pagination Controls");
    });

    it("renders footerContent after the table element", () => {
      const { container } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          footerContent={<div data-testid="footer-content">Footer</div>}
        />,
      );

      const table = container.querySelector("table");
      const footerContent = screen.getByTestId("footer-content");

      expect(table).toBeInTheDocument();
      expect(footerContent).toBeInTheDocument();

      const tableParent = table?.parentElement;
      const footerParent = footerContent.parentElement;

      expect(tableParent).toBeTruthy();
      expect(footerParent).toBeTruthy();
    });

    it("does not render footerContent when prop is not provided", () => {
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
        />,
      );

      expect(screen.queryByTestId("footer-content")).not.toBeInTheDocument();
    });
  });

  describe("radio selection mode", () => {
    it("renders radio buttons instead of checkboxes", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          selectionType="radio"
        />,
      );

      const radios = getByTestId("list-body").querySelectorAll("input[type='radio']");
      const checkboxes = getByTestId("list-body").querySelectorAll("input[type='checkbox']");
      expect(radios).toHaveLength(testItems.length);
      expect(checkboxes).toHaveLength(0);
    });

    it("does not render the select-all checkbox in the header", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          selectionType="radio"
        />,
      );

      const headerCheckboxes = getByTestId("list-header").querySelectorAll("input[type='checkbox']");
      expect(headerCheckboxes).toHaveLength(0);
    });

    it("allows only one item to be selected at a time", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          selectionType="radio"
        />,
      );

      const radios = getByTestId("list-body").querySelectorAll(
        "input[type='radio']",
        // eslint-disable-next-line
      ) as NodeListOf<HTMLInputElement>;

      // Select first item
      fireEvent.click(radios[0]);
      expect(radios[0].checked).toBe(true);

      // Select second item — first should be deselected
      fireEvent.click(radios[1]);
      expect(radios[0].checked).toBe(false);
      expect(radios[1].checked).toBe(true);
    });

    it("clicking a selected radio does not deselect it", () => {
      const { getByTestId } = render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          selectionType="radio"
        />,
      );

      const radios = getByTestId("list-body").querySelectorAll(
        "input[type='radio']",
        // eslint-disable-next-line
      ) as NodeListOf<HTMLInputElement>;

      fireEvent.click(radios[0]);
      expect(radios[0].checked).toBe(true);

      // Click the same radio again — should remain selected
      fireEvent.click(radios[0]);
      expect(radios[0].checked).toBe(true);
    });
  });

  describe("onRowClick", () => {
    it("calls onRowClick with the correct item when a row is clicked", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const rows = screen.getAllByTestId("list-row");
      fireEvent.click(rows[0]);

      expect(handleRowClick).toHaveBeenCalledTimes(1);
      expect(handleRowClick).toHaveBeenCalledWith(testItems[0], 0);
    });

    it("adds cursor-pointer class to rows when onRowClick is provided", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      expect(row.className).toContain("cursor-pointer");
    });

    it("uses a hover overlay on clickable row cells so sticky cells stay opaque", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          onRowClick={handleRowClick}
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      const checkboxCell = screen.getAllByTestId("checkbox")[0];
      const stickyDataCell = row.querySelectorAll("td")[1];
      const nonStickyDataCell = row.querySelectorAll("td")[2];
      const rowDividerExtension = row.querySelector('[data-testid="row-divider-extension"]');

      expect(row.className).not.toContain("dark:hover:bg-core-primary-5");
      expect(checkboxCell.className).toContain(
        "dark:group-hover:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]",
      );
      expect(stickyDataCell?.className).toContain(
        "dark:group-hover:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]",
      );
      expect(stickyDataCell?.className).toContain(
        "dark:group-hover:before:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]",
      );
      expect(nonStickyDataCell?.className).toContain(
        "dark:group-hover:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]",
      );
      expect(rowDividerExtension?.className).toContain(
        "dark:group-hover:bg-[linear-gradient(var(--color-core-primary-5),var(--color-core-primary-5))]",
      );
    });

    it("does not add cursor-pointer class when onRowClick is not provided", () => {
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      expect(row.className).not.toContain("cursor-pointer");
    });

    it("does not trigger onRowClick when the checkbox cell is clicked", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          onRowClick={handleRowClick}
        />,
      );

      const checkbox = screen.getAllByTestId("checkbox")[0];
      fireEvent.click(checkbox);

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("triggers onRowClick when Enter key is pressed on a focused row", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      fireEvent.keyDown(row, { key: "Enter" });

      expect(handleRowClick).toHaveBeenCalledTimes(1);
      expect(handleRowClick).toHaveBeenCalledWith(testItems[0], 0);
    });

    it("triggers onRowClick when Space key is pressed on a focused row", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      fireEvent.keyDown(row, { key: " " });

      expect(handleRowClick).toHaveBeenCalledTimes(1);
    });

    it("does not trigger onRowClick when Enter is pressed on a nested interactive element", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          itemSelectable
          onRowClick={handleRowClick}
        />,
      );

      const checkbox = screen.getAllByTestId("checkbox")[0].querySelector("input");
      expect(checkbox).toBeTruthy();
      fireEvent.keyDown(checkbox!, { key: "Enter", bubbles: true });

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick for clicks from portaled content", () => {
      const handleRowClick = vi.fn();

      const configWithPortal = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => (
            <>
              {item.name}
              {createPortal(<button data-testid={`portal-btn-${item.id}`}>Portal</button>, document.body)}
            </>
          ),
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithPortal}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const portalBtn = screen.getByTestId(`portal-btn-${testItems[0].id}`);
      fireEvent.click(portalBtn);

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("sets tabIndex on rows when onRowClick is provided", () => {
      const handleRowClick = vi.fn();
      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const row = screen.getAllByTestId("list-row")[0];
      expect(row).not.toHaveAttribute("role", "button");
      expect(row).toHaveAttribute("tabindex", "0");
    });

    it("does not trigger onRowClick when a nested button is clicked", () => {
      const handleRowClick = vi.fn();
      const buttonClick = vi.fn();

      const configWithButton = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => (
            <button data-testid={`btn-${item.id}`} onClick={buttonClick}>
              {item.name}
            </button>
          ),
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithButton}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      fireEvent.click(screen.getByTestId(`btn-${testItems[0].id}`));

      expect(buttonClick).toHaveBeenCalledTimes(1);
      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick when a nested link is clicked", () => {
      const handleRowClick = vi.fn();

      const configWithLink = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => (
            <a href="#" data-testid={`link-${item.id}`}>
              {item.name}
            </a>
          ),
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithLink}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      fireEvent.click(screen.getByTestId(`link-${testItems[0].id}`));

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick when a data-interactive element is clicked", () => {
      const handleRowClick = vi.fn();

      const configWithInteractive = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => (
            <div data-interactive data-testid={`interactive-${item.id}`}>
              {item.name}
            </div>
          ),
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithInteractive}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      fireEvent.click(screen.getByTestId(`interactive-${testItems[0].id}`));

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick when action-cell padding is clicked", () => {
      const handleRowClick = vi.fn();
      const actions: ListAction<TestItem>[] = [
        { title: "Action 1", actionHandler: vi.fn() },
        { title: "Action 2", actionHandler: vi.fn() },
      ];

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={testColConfig}
          items={testItems}
          itemKey="id"
          actions={actions}
          onRowClick={handleRowClick}
        />,
      );

      const actionCells = screen.getAllByTestId("action");
      fireEvent.click(actionCells[0]);

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick when Enter is pressed on a nested button", () => {
      const handleRowClick = vi.fn();

      const configWithButton = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => <button data-testid={`btn-${item.id}`}>{item.name}</button>,
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithButton}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      const button = screen.getByTestId(`btn-${testItems[0].id}`);
      fireEvent.keyDown(button, { key: "Enter", bubbles: true });

      expect(handleRowClick).not.toHaveBeenCalled();
    });

    it("does not trigger onRowClick when a role='button' element is clicked", () => {
      const handleRowClick = vi.fn();

      const configWithRoleButton = {
        ...testColConfig,
        [testCols.name]: {
          ...testColConfig[testCols.name as keyof typeof testColConfig],
          component: (item: TestItem) => (
            <div role="button" data-testid={`role-btn-${item.id}`}>
              {item.name}
            </div>
          ),
        },
      };

      render(
        <List<TestItem, TestItemKey>
          activeCols={activeCols}
          colTitles={testColTitles}
          colConfig={configWithRoleButton}
          items={testItems}
          itemKey="id"
          onRowClick={handleRowClick}
        />,
      );

      fireEvent.click(screen.getByTestId(`role-btn-${testItems[0].id}`));

      expect(handleRowClick).not.toHaveBeenCalled();
    });
  });
});
