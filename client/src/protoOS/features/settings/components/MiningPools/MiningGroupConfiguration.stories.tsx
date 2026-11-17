import type { ReactNode } from "react";
import MiningGroupConfiguration from "./MiningGroupConfiguration";
import type { ContainerPoolConfiguration, MiningGroupPoolSettings } from "@/protoOS/api/adapters/poolConfiguration";

const configuration: ContainerPoolConfiguration = {
  mode: "groups",
  suspended: false,
  locationsAvailable: true,
  groups: [
    {
      id: "default",
      name: "Default",
      revision: "1",
      boards: [{ mac: "AA:BB:CC:00:00:01", location: "Tank 1 · Module 2 · Board 1" }],
    },
    {
      id: "client-a",
      name: "Client A",
      revision: "2",
      boards: [{ mac: "AA:BB:CC:00:00:02", location: "Tank 1 · Module 2 · Board 2" }, { mac: "AA:BB:CC:00:00:03" }],
    },
  ],
  unassignedBoards: [{ mac: "AA:BB:CC:00:00:04", location: "Tank 1 · Module 2 · Board 3" }],
  pendingOperations: [
    { id: "transfer", kind: "Transfer", phase: "Stopping previous group", boards: ["AA:BB:CC:00:00:03"] },
  ],
  inventoryIssues: [{ mac: "AA:BB:CC:00:00:03", reason: "Board location could not be confirmed" }],
};
const pools: MiningGroupPoolSettings[] = [
  {
    id: "1",
    name: "Primary",
    url: "stratum+tcp://primary.example.com:3333",
    username: "account.region.board-01",
    priority: 0,
    enabled: true,
  },
  {
    id: "2",
    name: "Backup",
    url: "stratum+ssl://backup.example.com:3334",
    username: "account.region.board-01",
    priority: 1,
    enabled: false,
  },
];
const adapter = { getMiningGroupPoolSettings: async () => pools };

export const Groups = () => <MiningGroupConfiguration configuration={configuration} adapter={adapter} />;
const manyBoards = Array.from({ length: 3000 }, (_, index) => ({
  mac: `02:00:${index.toString(16).padStart(8, "0").match(/../g)!.join(":")}`,
  location:
    index === 2999
      ? undefined
      : `Tank ${Math.floor(index / 48) + 1} · Module ${(Math.floor(index / 3) % 16) + 1} · Board ${(index % 3) + 1}`,
})).reverse();
export const ManyHashboards = () => (
  <MiningGroupConfiguration
    configuration={{
      ...configuration,
      groups: [{ id: "large", name: "Large group", revision: "1", boards: manyBoards }],
      unassignedBoards: [],
      pendingOperations: [],
      inventoryIssues: [],
    }}
    adapter={adapter}
  />
);
export const Suspended = () => (
  <MiningGroupConfiguration
    configuration={{ ...configuration, suspended: true, locationsAvailable: false }}
    adapter={adapter}
  />
);
export const PoolReadFailure = () => (
  <MiningGroupConfiguration
    configuration={configuration}
    adapter={{
      getMiningGroupPoolSettings: async () => {
        throw new Error("offline");
      },
    }}
  />
);
export const Empty = () => (
  <MiningGroupConfiguration
    configuration={{ ...configuration, groups: [], unassignedBoards: [], inventoryIssues: [], pendingOperations: [] }}
    adapter={adapter}
  />
);

export default {
  title: "Proto OS/Settings/Pool assignments",
  decorators: [
    (Story: () => ReactNode) => (
      <div className="mx-auto max-w-160 p-6 tablet:py-20">
        <Story />
      </div>
    ),
  ],
};
