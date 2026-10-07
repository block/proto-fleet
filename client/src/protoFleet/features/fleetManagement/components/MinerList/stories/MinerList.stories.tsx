import { useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import { expect, spyOn, userEvent, within } from "storybook/test";

import MinerListComponent from "../MinerList";
import { fleetInventoryClient } from "@/protoFleet/api/clients";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  EntityKind,
  GetModuleHashboardsResponseSchema,
  InventoryItemSchema,
  ModuleIssuesSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
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
        // Mirror the Fleet page's own layout props. Without them the story
        // renders edge-to-edge with no page inset, which hides any overflow
        // that only appears once the filter row is padded.
        paddingLeft={{ phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" }}
        overflowContainer={false}
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

    // The field autofocuses on expand, and Input hides its label on focus with
    // `visibility: hidden`. Browsers keep a hidden `<label for>` as the
    // accessible name, but Testing Library's name computation drops hidden
    // labels, so a role-and-name query fails the moment the field opens.
    // Querying through the label association does not consult visibility.
    // The `input` selector keeps the collapsed toggle, which carries the same
    // aria-label, from matching.
    const searchField = () => canvas.queryByLabelText("Search miners", { selector: "input" });

    await expect(searchField()).not.toBeInTheDocument();
    await userEvent.click(toggle);

    const input = await canvas.findByLabelText("Search miners", { selector: "input" });
    await expect(input).toHaveFocus();
    await expect(toggle).not.toBeInTheDocument();

    await userEvent.click(await canvas.findByRole("button", { name: "Clear Search miners" }));
    await expect(searchField()).not.toBeInTheDocument();
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

export const MinersAndModules: Story = {
  args: {
    availableContainers: [
      { id: "1", label: "Container 1 · North" },
      { id: "2", label: "Container 2 · South" },
    ],
    title: "Mining assets",
    minerIds: [miners[0].deviceIdentifier],
    miners: buildMinersRecord([miners[0]]),
    errorsByDevice: {},
    errorsLoaded: true,
    getActiveBatches: () => [],
    totalMiners: 3,
    inventoryMinerCount: 1,
    inventoryItems: [
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: 1n },
        name: "Container 1 T1 M1",
        facilityName: "Container 1",
        groupName: "Tank 1",
        state: "hashing",
        hashrateThs: 300,
        powerKw: 3.6,
        efficiencyJth: 12,
        minTemperatureC: 62,
        maxTemperatureC: 62,
      }),
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MINER, id: 1n },
        deviceIdentifier: miners[0].deviceIdentifier,
        details: { case: "miner", value: miners[0] },
      }),
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: 2n },
        name: "Container 1 T1 M2",
        facilityName: "Container 1",
        groupName: "Tank 1",
        state: "unknown",
      }),
    ],
    paddingLeft: { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" },
    overflowContainer: false,
  },
};

export const ExpandedHashboards: Story = {
  ...MinersAndModules,
  args: {
    ...MinersAndModules.args,
    inventoryItems: MinersAndModules.args?.inventoryItems?.map((item) =>
      item.ref?.kind === EntityKind.MODULE && item.ref.id === 1n
        ? create(InventoryItemSchema, {
            ...item,
            state: "attention",
            hashrateThs: 190,
            minTemperatureC: 35,
            maxTemperatureC: 73,
            moduleIssues: create(ModuleIssuesSchema, {
              complete: true,
              observedAt: timestampFromDate(new Date()),
              items: [
                {
                  component: "hashboard",
                  componentId: "aa:bb:cc:00:00:02",
                  cause: "HASHRAIL_FAULT",
                  severity: "fault",
                  observedAt: timestampFromDate(new Date()),
                },
              ],
            }),
          })
        : item,
    ),
  },
  beforeEach: () => {
    const request = spyOn(fleetInventoryClient, "getModuleHashboards").mockImplementation(async ({ moduleIds }) =>
      create(GetModuleHashboardsResponseSchema, {
        modules: (moduleIds ?? []).map((moduleId) => ({
          moduleId,
          slots:
            moduleId === 1n
              ? [
                  {
                    position: 1,
                    occupancy: "occupied",
                    board: {
                      hashboardId: 11n,
                      moduleId,
                      position: 1,
                      serial: "CT1-HB-001",
                      state: "hashing",
                      hashrateHs: 100e12,
                      temperatureC: 62,
                      hashrailOk: true,
                      refclksOk: true,
                      health: { errors: [], warnings: [] },
                    },
                  },
                  {
                    position: 2,
                    occupancy: "occupied",
                    board: {
                      hashboardId: 12n,
                      moduleId,
                      position: 2,
                      serial: "CT1-HB-002",
                      observedAt: timestampFromDate(new Date()),
                      state: "attention",
                      hashrateHs: 90e12,
                      temperatureC: 73,
                      hashrailOk: false,
                      refclksOk: true,
                      health: { errors: [{ cause: "HASHRAIL_FAULT" }], warnings: [] },
                    },
                  },
                  {
                    position: 3,
                    occupancy: "occupied",
                    board: {
                      hashboardId: 13n,
                      moduleId,
                      position: 3,
                      serial: "CT1-HB-003",
                      state: "isolated",
                      isolated: true,
                      hashrateHs: 0,
                      temperatureC: 35,
                      hashrailOk: true,
                      refclksOk: true,
                    },
                  },
                ]
              : [
                  { position: 1, occupancy: "empty" },
                  { position: 2, occupancy: "unknown", unavailableReason: "identity_conflict" },
                  {
                    position: 3,
                    occupancy: "occupied",
                    board: { hashboardId: 23n, moduleId, position: 3, serial: "CT1-HB-023", state: "unknown" },
                  },
                ],
        })),
      }),
    );
    return () => request.mockRestore();
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Expand hashboards for Container 1 T1 M1" }));
    await canvas.findByText("Hashboard 1 - CT1-HB-001");
    await userEvent.click(canvas.getByRole("button", { name: "Expand hashboards for Container 1 T1 M2" }));
    await canvas.findByText("Empty slot");
    await expect(canvas.getAllByTestId("list-child-row")).toHaveLength(6);
    for (const row of canvas.getAllByTestId("list-child-row"))
      await expect(within(row).queryByRole("checkbox")).toBeNull();
  },
};
