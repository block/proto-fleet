import { expect, test, vi } from "vitest";
import { Code } from "@connectrpc/connect";
import type { MdkV2Client } from "./connectClient";
import { createPoolStatistics } from "./poolStatistics";
const configs = [
  { id: "default", name: "Default", revision: "1", assignedCount: 1 },
  { id: "client", name: "Client A", revision: "2", assignedCount: 1 },
];
const defaultPool = {
  id: "1",
  name: "Primary",
  url: "stratum+tcp://same.pool:3333",
  username: "default",
  enabled: true,
  priority: 0,
};
const fixture = () => {
  const groups = [
    ...configs.map((configuration) => ({ ...configuration })),
    { id: "unused", name: "Unused", revision: "1" },
  ];
  const capability = { name: "mining-groups-v1", version: 1, enabled: true, stateRevision: "2" };
  const response = <T>(data: T) => Promise.resolve({ data });
  const client = {
    poolServiceListPools: vi.fn(() => response({ pools: [defaultPool] })),
    miningGroupServiceGetMiningGroupCapability: vi.fn(() => response({ capability })),
    miningGroupServiceListMiningGroups: vi.fn(() => response({ groups })),
    miningGroupServiceListMiningGroupMemberships: vi.fn(() =>
      response({
        memberships: [
          { groupId: "default", macAddress: "aa:bb:cc:00:00:01" },
          { groupId: "client", macAddress: "aa:bb:cc:00:00:02" },
        ],
      }),
    ),
    miningGroupServiceGetMiningGroup: vi.fn(({ id }) => response({ group: groups.find((group) => group.id === id) })),
    miningGroupServiceListMiningGroupPools: vi.fn(({ groupId }) =>
      response({
        pools: [
          {
            id: groupId === "default" ? 1 : 4,
            name: "Primary",
            url: "stratum+tcp://same.pool:3333",
            username: groupId,
            enabled: true,
            priority: 0,
          },
        ],
      }),
    ),
    poolServiceGetPoolDiagnostics: vi.fn(() =>
      response({ pools: [{ poolId: "1", isActive: true, currentDifficulty: 20 }] }),
    ),
    miningGroupServiceGetMiningGroupPoolDiagnostics: vi.fn(() =>
      response({
        pools: [{ poolId: "4", isActive: true, currentDifficulty: 80 }],
        observedMacAddresses: ["aa:bb:cc:00:00:02"],
      }),
    ),
  };
  return {
    client,
    capability,
    service: createPoolStatistics(client as unknown as MdkV2Client, (params) => params ?? {}),
  };
};
test("reading mining-group mode does not load configurations or pool diagnostics", async () => {
  const { service, client } = fixture();
  expect(await service.getMiningGroupsEnabled()).toBe(true);
  expect(client.miningGroupServiceGetMiningGroupCapability).toHaveBeenCalledTimes(1);
  expect(client.miningGroupServiceListMiningGroups).not.toHaveBeenCalled();
  expect(client.miningGroupServiceListMiningGroupMemberships).not.toHaveBeenCalled();
  expect(client.poolServiceGetPoolDiagnostics).not.toHaveBeenCalled();
});
test("lists only assigned configurations and does not request board inventory", async () => {
  const { service, client } = fixture();
  expect((await service.list()).configurations).toEqual([configs[1], configs[0]]);
  expect(client.miningGroupServiceListMiningGroupPools).not.toHaveBeenCalled();
});
test("separates identical endpoints by controller pool identity and configuration", async () => {
  const { service, client } = fixture();
  const readings = await service.read(configs);
  expect(readings[0].pools[0]).toMatchObject({ id: "pool:1", username: "default", difficulty: 20 });
  expect(readings[1].pools[0]).toMatchObject({ id: "pool:4", username: "client", difficulty: 80 });
  expect(client.miningGroupServiceGetMiningGroupPoolDiagnostics).toHaveBeenCalledWith(
    { groupId: "client" },
    expect.anything(),
  );
  expect(client.poolServiceGetPoolDiagnostics).toHaveBeenCalledExactlyOnceWith(
    { includeLastRecordedShares: true },
    expect.anything(),
  );
  expect(client.poolServiceListPools).toHaveBeenCalledTimes(2);
  expect(client.miningGroupServiceListMiningGroups).toHaveBeenCalledTimes(1);
  expect(client.miningGroupServiceListMiningGroups).toHaveBeenCalledAfter(
    client.miningGroupServiceGetMiningGroupPoolDiagnostics,
  );
  expect(client.miningGroupServiceGetMiningGroup).not.toHaveBeenCalled();
  expect(client.miningGroupServiceListMiningGroupPools).not.toHaveBeenCalledWith(
    { groupId: "default" },
    expect.anything(),
  );
});
test("Default status-only reads omit the last-share lookup", async () => {
  const { service, client } = fixture();
  const [reading] = await service.read([configs[0]], { includeLastRecordedShares: false });

  expect(client.poolServiceGetPoolDiagnostics).toHaveBeenCalledExactlyOnceWith({}, expect.anything());
  expect(reading.pools[0]).toMatchObject({ id: "pool:1", status: "Active" });
});
test("withholds Default readings if the same pool ID changes during diagnostics", async () => {
  const { service, client } = fixture();
  client.poolServiceGetPoolDiagnostics.mockImplementationOnce(async () => {
    client.poolServiceListPools.mockResolvedValueOnce({
      data: { pools: [{ ...defaultPool, url: "stratum+tcp://new.pool:3333" }] },
    });
    return { data: { pools: [{ poolId: defaultPool.id, isActive: true, currentDifficulty: 50 }] } };
  });
  const [changed, unchanged] = await service.read(configs);
  expect(changed).toEqual({ ...configs[0], pools: [], stale: true });
  expect(unchanged.pools[0]).toMatchObject({ id: "pool:4", difficulty: 80 });
});
test("treats Default pool settings as unchanged when equally ranked records change order", async () => {
  const { service, client } = fixture();
  const disabledPool = { ...defaultPool, id: "2", name: "Backup", enabled: false };
  client.poolServiceListPools
    .mockResolvedValueOnce({ data: { pools: [defaultPool, disabledPool] } })
    .mockResolvedValueOnce({ data: { pools: [disabledPool, defaultPool] } });
  const [reading] = await service.read([configs[0]]);
  expect(reading.stale).toBeUndefined();
  expect(reading.pools[0]).toMatchObject({ id: "pool:1", difficulty: 20 });
});
test("marks the changed configuration stale and keeps the other configuration readings", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceListMiningGroups.mockResolvedValueOnce({
    data: { groups: [configs[0], { ...configs[1], revision: "3" }] },
  });
  const [unchanged, changed] = await service.read(configs);
  expect(unchanged.pools[0]).toMatchObject({ id: "pool:1", difficulty: 20 });
  expect(changed).toEqual({ ...configs[1], pools: [], stale: true });
});
test("keeps the other configuration readings when a configuration no longer exists", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceListMiningGroupPools.mockRejectedValueOnce({ status: 404 });
  client.miningGroupServiceListMiningGroups.mockResolvedValueOnce({ data: { groups: [configs[0]] } });
  const [unchanged, removed] = await service.read(configs);
  expect(unchanged.pools[0]).toMatchObject({ id: "pool:1", difficulty: 20 });
  expect(removed).toEqual({ ...configs[1], pools: [], stale: true });
});
test("refuses duplicate pool IDs rather than merging configuration statistics", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceListMiningGroupPools.mockImplementation(async () => ({
    data: { pools: [{ id: 1, name: "Primary", url: "same", username: "same", enabled: true, priority: 0 }] },
  }));
  await expect(service.read(configs)).rejects.toThrow("duplicate pool IDs");
});
test("missing group diagnostics remain unknown and never use default readings", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceGetMiningGroupPoolDiagnostics.mockRejectedValue({ status: 501 });
  const [reading] = await service.read([configs[1]]);
  expect(reading.error).toContain("unavailable");
  expect(reading.pools[0].status).toBe("Unknown");
  expect(reading.pools[0].difficulty).toBeUndefined();
  expect(client.poolServiceGetPoolDiagnostics).not.toHaveBeenCalled();
});
test("authentication failures reach sign-in handling", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceGetMiningGroupPoolDiagnostics.mockRejectedValue({ status: 401 });
  await expect(service.read([configs[1]])).rejects.toMatchObject({ status: 401 });
});
test("disabled and unsupported group mode use the legacy page; outages do not", async () => {
  const { service, capability, client } = fixture();
  capability.enabled = false;
  expect(await service.list()).toEqual({ enabled: false, configurations: [] });
  client.miningGroupServiceGetMiningGroupCapability.mockRejectedValueOnce({ error: { code: Code.Unimplemented } });
  expect(await service.list()).toEqual({ enabled: false, configurations: [] });
  client.miningGroupServiceGetMiningGroupCapability.mockRejectedValueOnce(new Error("offline"));
  await expect(service.list()).rejects.toThrow("offline");
});

test("unspecified connection observations do not turn protobuf numeric defaults into real zero readings", async () => {
  const { service, client } = fixture();
  client.miningGroupServiceGetMiningGroupPoolDiagnostics.mockResolvedValue({
    data: { pools: [{ poolId: "4", isActive: false, currentDifficulty: 0 }], observedMacAddresses: [] },
  });
  const [reading] = await service.read([configs[1]]);
  expect(reading.pools[0].status).toBe("Unknown");
  expect(reading.pools[0].difficulty).toBeUndefined();
  expect(reading.observedBoards).toBe(0);
});
