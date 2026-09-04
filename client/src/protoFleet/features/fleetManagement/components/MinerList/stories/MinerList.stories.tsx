import { useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import { expect, userEvent, within } from "storybook/test";

import MinerListComponent from "../MinerList";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import {
  allIssueMiners,
  allStatusMiners,
  errorMessages,
} from "@/protoFleet/features/fleetManagement/components/MinerList/stories/statusMocks";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const meta: Meta<typeof MinerListComponent> = {
  title: "Proto Fleet/MinerList",
  component: MinerListComponent,
};

export default meta;
type Story = StoryObj<typeof MinerListComponent>;

const buildMinersRecord = (minerList: MinerStateSnapshot[]): Record<string, MinerStateSnapshot> =>
  Object.fromEntries(minerList.map((m) => [m.deviceIdentifier, m]));

const buildErrorsByDevice = (
  minerList: MinerStateSnapshot[],
  errors: ErrorMessage[],
): Record<string, ErrorMessage[]> => {
  const byDevice: Record<string, ErrorMessage[]> = {};
  for (const m of minerList) {
    byDevice[m.deviceIdentifier] = [];
  }
  for (const error of errors) {
    if (error.deviceIdentifier && byDevice[error.deviceIdentifier]) {
      byDevice[error.deviceIdentifier].push(error);
    }
  }
  return byDevice;
};

const matchesGlobalSearch = (miner: MinerStateSnapshot, query: string) => {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return true;

  return [
    miner.name,
    miner.deviceIdentifier,
    miner.serialNumber,
    miner.macAddress,
    miner.ipAddress,
    miner.workerName,
  ].some((value) => value.toLocaleLowerCase().includes(normalized));
};

/**
 * Story-only server round trip: MinerList owns the real URL-backed search
 * field, while this wrapper applies that URL query to the same six miner fields
 * as the API and returns the matching page. This keeps the in-situ story
 * interactive without reproducing the shipped search control.
 */
const SearchableMinerList = ({ initialQuery }: { initialQuery: string }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const currentQuery = searchParams.get("search") ?? "";

  useEffect(() => {
    const next = new URLSearchParams(searchParams);
    if (initialQuery) next.set("search", initialQuery);
    else next.delete("search");
    setSearchParams(next, { replace: true });
    // Seed once per story mount. Subsequent URL updates belong to MinerList.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQuery, setSearchParams]);

  const visibleMiners = useMemo(
    () => miners.filter((miner) => matchesGlobalSearch(miner, currentQuery)),
    [currentQuery],
  );

  return (
    <div className="min-h-screen bg-surface-base">
      <MinerListWrapper
        minerList={visibleMiners}
        totalMiners={visibleMiners.length}
        totalUnfilteredMiners={miners.length}
      />
    </div>
  );
};

// Helper component to render MinerList with props derived from mock data
const MinerListWrapper = ({
  minerList,
  totalMiners = minerList.length,
  totalUnfilteredMiners = minerList.length,
}: {
  minerList: MinerStateSnapshot[];
  totalMiners?: number;
  totalUnfilteredMiners?: number;
}) => {
  const minerIds = minerList.map((miner) => miner.deviceIdentifier);
  const minersRecord = buildMinersRecord(minerList);
  const errorsByDevice = buildErrorsByDevice(minerList, errorMessages);

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <MinerListComponent
        title="Miners"
        minerIds={minerIds}
        miners={minersRecord}
        errorsByDevice={errorsByDevice}
        errorsLoaded={true}
        getActiveBatches={() => []}
        totalMiners={totalMiners}
        totalUnfilteredMiners={totalUnfilteredMiners}
        onAddMiners={action("onAddMiners")}
      />
    </div>
  );
};

// ============================================================================
// Consolidated Story with All States and Issues
// ============================================================================

export const AllStatusesAndIssuesMinerList: Story = {
  render: () => {
    const allMiners = [...allStatusMiners, ...allIssueMiners];
    return (
      <div className="space-y-8">
        <div>
          <h2 className="mb-4 text-heading-300">All Statuses and Issues</h2>
          <MinerListWrapper minerList={allMiners} />
        </div>
      </div>
    );
  },
};

// ============================================================================
// Other Examples
// ============================================================================

export const OperationalMinerList: Story = {
  render: () => <MinerListWrapper minerList={miners} />,
};

export const CollapsibleGlobalSearch: Story = {
  name: "Collapsible global search",
  render: () => <SearchableMinerList initialQuery="" />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const toggle = await canvas.findByRole("button", { name: "Search miners" });

    await expect(canvas.queryByRole("textbox", { name: "Search miners" })).not.toBeInTheDocument();
    await userEvent.click(toggle);

    const input = await canvas.findByRole("textbox", { name: "Search miners" });
    await expect(input).toHaveFocus();
    await expect(toggle).not.toBeInTheDocument();

    await userEvent.click(await canvas.findByRole("button", { name: "Clear Search miners" }));
    await expect(canvas.queryByRole("textbox", { name: "Search miners" })).not.toBeInTheDocument();
    await expect(canvas.getByRole("button", { name: "Search miners" })).toHaveAttribute("aria-expanded", "false");
  },
};

export const GlobalSearchResults: Story = {
  name: "Global search results",
  render: () => <SearchableMinerList initialQuery="worker-03" />,
};

export const GlobalSearchNoMatches: Story = {
  name: "Global search no matches",
  render: () => <SearchableMinerList initialQuery="west-yard" />,
};

export const EmptyMinerList: Story = {
  render: () => (
    <div>
      <MinerListComponent
        title="Miners"
        minerIds={[]}
        miners={{}}
        errorsByDevice={{}}
        errorsLoaded={true}
        getActiveBatches={() => []}
        totalMiners={0}
        onAddMiners={action("onAddMiners")}
      />
    </div>
  ),
};
