import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import {
  minerCols,
  minerColTitles,
  MinerFilterState,
} from "@/protoFleet/components/MinerList/constants";
import minerColConfig from "@/protoFleet/components/MinerList/minerColConfig";
import MinerListActionBar from "@/protoFleet/components/MinerList/MinerListActionBar";
import { miners } from "@/protoFleet/components/MinerList/stories/mocks";
import type { Miner } from "@/protoFleet/components/MinerList/types";
import List from "@/shared/components/List/index";
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
    minerCols.name,
    minerCols.macAddress,
    minerCols.status,
    minerCols.hashrate,
    minerCols.efficiency,
    minerCols.powerUsage,
    minerCols.temperature,
  ] as (keyof Miner)[];
  type MinerKeyValueType = Miner["macAddress"];

  it("renders cols correctly", () => {
    render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
      />,
    );

    for (const col of activeCols) {
      expect(screen.getByText(minerColTitles[col])).toBeInTheDocument();
    }
  });

  it("renders rows correctly", () => {
    render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
      />,
    );

    expect(screen.getAllByRole("row")).toHaveLength(miners.length + 1);
  });

  it("does not render checkboxes when items are not selectable", () => {
    const { getByTestId } = render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
        itemSelectable={false}
        renderActionBar={(selectedItems) => (
          <MinerListActionBar selectedMiners={selectedItems} />
        )}
      />,
    );

    const selectMinerCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;
    expect(selectMinerCheckboxes).toHaveLength(0);
  });

  it("selects all items when clicking select all checkbox", () => {
    const { getByTestId } = render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
        itemSelectable
      />,
    );
    const selectAllCheckbox = getByTestId("list-header").querySelector(
      "input[type='checkbox']",
    ) as HTMLInputElement;

    const selectMinerCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    // expect select all checkbox to be unchecked
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);

    // click individual item checkboxes and make sure select all checkbox is unchecked and total checked is only 1
    fireEvent.click(selectMinerCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(1);

    // click select all checkboxes and make sure all checkboxes are checked
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(true);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(miners.length);

    // click item 1 (deselect) checkbox and make select all checkbox unchecked
    fireEvent.click(selectMinerCheckboxes[0]);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(miners.length - 1);

    // click select all twice to deselect all miners
    fireEvent.click(selectAllCheckbox);
    fireEvent.click(selectAllCheckbox);
    expect(selectAllCheckbox.checked).toBe(false);
    expect(
      Array.from(selectMinerCheckboxes).filter((c) => c.checked),
    ).toHaveLength(0);
  });

  it("renders action bar when items are selected", () => {
    const renderActionBar = vi.fn(() => <div>Action Bar</div>);

    const { getByTestId } = render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
        itemSelectable
        renderActionBar={renderActionBar}
      />,
    );

    const selectMinerCheckboxes = getByTestId("list-body").querySelectorAll(
      "input[type='checkbox']",
      // eslint-disable-next-line
    ) as NodeListOf<HTMLInputElement>;

    fireEvent.click(selectMinerCheckboxes[0]);
    expect(renderActionBar).toHaveBeenCalledWith([miners[0].macAddress]);
    expect(screen.getByText("Action Bar")).toBeInTheDocument();
  });

  it("renders actions popover and triggers the correct action", async () => {
    const mockAction = vi.fn();
    const actions = [
      { title: "Edit", actionHandler: mockAction },
      { title: "Delete", actionHandler: mockAction },
    ] as ListAction<Miner>[];

    const { getAllByTestId } = render(
      <List<Miner, MinerKeyValueType, MinerFilterState>
        activeCols={activeCols}
        colTitles={minerColTitles}
        colConfig={minerColConfig}
        items={miners}
        itemKey="macAddress"
        actions={actions}
      />,
    );

    const actionButton = getAllByTestId("list-actions-trigger")[0];
    fireEvent.click(actionButton);

    const editAction = screen.getByText(actions[0].title);
    fireEvent.click(editAction);

    expect(mockAction).toHaveBeenCalled();
    expect(mockAction).toHaveBeenCalledWith(miners[0]);
  });
});
