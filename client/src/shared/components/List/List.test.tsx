import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import List from "@/shared/components/List/index";
import testColConfig from "@/shared/components/List/mocks/colConfig";
import {
  testCols,
  testColTitles,
  TestItem,
  testItems,
} from "@/shared/components/List/mocks/data";
import { ListAction } from "@/shared/components/List/types";

beforeAll(() => {
  vi.mock("recharts", () => ({
    ResponsiveContainer: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-responsive-container">{children}</div>
    ),
    LineChart: ({ children }: { children: ReactNode }) => (
      <div data-testid="recharts-line-chart">{children}</div>
    ),
    ReferenceLine: () => <div data-testid="recharts-reference-line" />,
    Line: () => <div data-testid="recharts-line" />,
    XAxis: () => <div data-testid="recharts-xaxis" />,
    YAxis: () => <div data-testid="recharts-yaxis" />,
  }));
});

describe("List", () => {
  const activeCols = [
    testCols.name,
    testCols.status,
    testCols.value,
    testCols.timestamp,
  ] as (keyof TestItem)[];
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
    const selectAllCheckbox = getByTestId("list-header").querySelector(
      "input[type='checkbox']",
    ) as HTMLInputElement;

    const selectItemCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // expect select all checkbox to be unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectItemCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);

    // click individual item checkboxes and make sure select all checkbox is unchecked and total checked is only 1
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectItemCheckboxes).filter((c) => c.checked),
    ).toHaveLength(1);

    // click select all checkboxes and make sure all checkboxes are checked
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(
      Array.from(selectItemCheckboxes).filter((c) => c.checked),
    ).toHaveLength(testItems.length);

    // click item 1 (deselect) checkbox and make select all checkbox unchecked
    fireEvent.click(selectItemCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectItemCheckboxes).filter((c) => c.checked),
    ).toHaveLength(testItems.length - 1);

    // click select all twice to deselect all items
    fireEvent.click(selectAllCheckbox);
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectItemCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);
  });

  it("renders action bar when items are selected", () => {
    const renderActionBar = vi.fn(() => <div>Action Bar</div>);

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

    fireEvent.click(selectItemCheckboxes[0]);
    expect(renderActionBar).toHaveBeenCalledWith([testItems[0].id]);
    expect(screen.getByText("Action Bar")).toBeInTheDocument();
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
});
