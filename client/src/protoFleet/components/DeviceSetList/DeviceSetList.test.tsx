import { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import DeviceSetList from "./DeviceSetList";
import type { DeviceSetListItem } from "./DeviceSetList";
import { DeviceSetSchema, DeviceSetStatsSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { DeviceSet, DeviceSetStats } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";

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

const createMockDeviceSet = (id: bigint, label: string): DeviceSet =>
  create(DeviceSetSchema, {
    id,
    label,
    deviceCount: 5,
    typeDetails: { case: "groupInfo", value: {} },
  });

const createMockStats = (deviceSetId: bigint): DeviceSetStats =>
  create(DeviceSetStatsSchema, {
    deviceSetId,
    deviceCount: 5,
    reportingCount: 5,
    totalHashrateThs: 100,
    avgEfficiencyJth: 25,
    totalPowerKw: 10,
    minTemperatureC: 30,
    maxTemperatureC: 60,
    hashingCount: 4,
    brokenCount: 1,
    offlineCount: 0,
    sleepingCount: 0,
    hashrateReportingCount: 5,
    efficiencyReportingCount: 5,
    powerReportingCount: 5,
    temperatureReportingCount: 5,
  });

const defaultProps = {
  renderName: (item: DeviceSetListItem) => <span>{item.deviceSet.label}</span>,
  renderMiners: (item: DeviceSetListItem) => <span>{item.deviceSet.deviceCount}</span>,
  currentSort: { field: "name" as const, direction: "asc" as const },
  onSort: vi.fn(),
  itemName: { singular: "group", plural: "groups" },
};

describe("DeviceSetList", () => {
  it("uses descending sort when the issues header is selected", () => {
    const deviceSet = createMockDeviceSet(1n, "Group A");
    const stats = createMockStats(1n);
    const onSort = vi.fn();

    render(
      <DeviceSetList {...defaultProps} deviceSets={[deviceSet]} statsMap={new Map([[1n, stats]])} onSort={onSort} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Issues" }));
    expect(onSort).toHaveBeenCalledWith("issues", "desc");
  });

  describe("emptyStateRow prop", () => {
    it("renders empty state row when items are empty and emptyStateRow is provided", () => {
      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={<div>No matching items</div>}
        />,
      );

      expect(screen.getByTestId("list-empty-row")).toBeInTheDocument();
      expect(screen.getByText("No matching items")).toBeInTheDocument();
    });

    it("does not render empty state row when items are present", () => {
      const deviceSet = createMockDeviceSet(1n, "Group A");
      const stats = createMockStats(1n);

      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[deviceSet]}
          statsMap={new Map([[1n, stats]])}
          emptyStateRow={<div>No matching items</div>}
        />,
      );

      expect(screen.queryByTestId("list-empty-row")).not.toBeInTheDocument();
      expect(screen.getByText("Group A")).toBeInTheDocument();
    });

    it("does not render empty state row when items are empty and emptyStateRow is undefined", () => {
      render(<DeviceSetList {...defaultProps} deviceSets={[]} statsMap={new Map()} />);

      expect(screen.queryByTestId("list-empty-row")).not.toBeInTheDocument();
    });

    it("keeps column headers visible when showing empty state row", () => {
      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={<div>No matching items</div>}
        />,
      );

      expect(screen.getByTestId("list-header")).toBeInTheDocument();
      expect(screen.getByText("Name")).toBeInTheDocument();
    });
  });

  describe("no results empty state content", () => {
    const renderEmptyState = (onClearFilters: () => void) => (
      <NoFilterResultsEmptyState hasActiveFilters onClearFilters={onClearFilters} />
    );

    it("renders 'No results' heading in the empty state", () => {
      const handleClearFilters = vi.fn();

      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={renderEmptyState(handleClearFilters)}
        />,
      );

      expect(screen.getByText("No results")).toBeInTheDocument();
    });

    it("renders description text in the empty state", () => {
      const handleClearFilters = vi.fn();

      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={renderEmptyState(handleClearFilters)}
        />,
      );

      expect(screen.getByText("Try adjusting or clearing your filters.")).toBeInTheDocument();
    });

    it("renders the 'Clear all filters' button in the empty state", () => {
      const handleClearFilters = vi.fn();

      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={renderEmptyState(handleClearFilters)}
        />,
      );

      expect(screen.getByTestId("clear-all-filters-button")).toBeInTheDocument();
      expect(screen.getByText("Clear all filters")).toBeInTheDocument();
    });

    it("calls the clear filters handler when 'Clear all filters' button is clicked", () => {
      const handleClearFilters = vi.fn();

      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          emptyStateRow={renderEmptyState(handleClearFilters)}
        />,
      );

      fireEvent.click(screen.getByTestId("clear-all-filters-button"));
      expect(handleClearFilters).toHaveBeenCalledTimes(1);
    });
  });

  describe("pagination visibility with empty state", () => {
    it("does not render pagination when items are empty and empty state is shown", () => {
      render(
        <DeviceSetList
          {...defaultProps}
          deviceSets={[]}
          statsMap={new Map()}
          total={0}
          emptyStateRow={<div>No results</div>}
        />,
      );

      expect(screen.queryByLabelText("Previous page")).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Next page")).not.toBeInTheDocument();
    });
  });
});
