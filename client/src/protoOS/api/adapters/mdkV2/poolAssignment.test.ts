import { describe, expect, test, vi } from "vitest";
import type { PoolAssignmentAttempt } from "../poolAssignment";
import type { MdkV2Client } from "./connectClient";
import type { V2MiningGroupPoolRecord, V2MiningGroupRecord } from "./generated/generatedApi";
import { createPoolAssignment } from "./poolAssignment";

const A = "aa:bb:cc:00:00:01",
  B = "aa:bb:cc:00:00:02",
  C = "aa:bb:cc:00:00:03";
const pool = {
  name: "Primary",
  url: "stratum+tcp://pool.example:3333",
  username: "account.region.worker",
  password: "secret",
};
const publicPool = { id: 1, name: pool.name, url: pool.url, username: pool.username, priority: 0, enabled: true };
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));
const fixture = () => {
  const state = {
    capability: {
      name: "mining-groups-v1",
      version: 1,
      enabled: true,
      suspended: false,
      stateRevision: "1",
      maxGroups: 10,
      maxBoardsPerGroup: 3,
      maxPoolsPerGroup: 3,
      assignmentGranularity: "PHYSICAL_HASHBOARD",
    },
    groups: [{ id: "default", name: "Default", revision: "1", desiredMining: true }] as V2MiningGroupRecord[],
    members: new Map([
      [A, "default"],
      [B, "default"],
      [C, "default"],
    ]),
    pools: new Map<string, V2MiningGroupPoolRecord[]>([["default", [publicPool]]]),
    operations: new Map<string, { id: string; phase: string; macAddresses: string[] }>(),
    mining: true,
  };
  const group = (id: string) => state.groups.find((group) => group.id === id)!;
  const check = (id: string, expected: string) => {
    const value = group(id);
    if (value.revision !== expected) throw new Error("revision conflict");
    return value;
  };
  const touch = (value: V2MiningGroupRecord) => {
    value.revision = String(Number(value.revision) + 1);
    return value;
  };
  const response = <T>(data: T) => Promise.resolve({ data: clone(data) });
  let poolSequence = 3;
  const client = {
    poolServiceListPools: vi.fn(() =>
      response({ pools: (state.pools.get("default") ?? []).map((pool) => ({ ...pool, id: String(pool.id) })) }),
    ),
    poolServiceUpdatePool: vi.fn(({ poolId }, { pool, updateMask }) => {
      const pools = state.pools.get("default")!;
      const index = pools.findIndex((current) => String(current.id) === poolId);
      if (index < 0) throw new Error("Pool not found");
      const next = { ...pools[index] };
      for (const field of updateMask.split(",")) Object.assign(next, { [field]: pool[field] });
      pools[index] = next;
      return response({ pool: { ...next, id: String(next.id) } });
    }),
    poolServiceDeletePool: vi.fn(({ id }) => {
      state.pools.set(
        "default",
        state.pools.get("default")!.filter((pool) => String(pool.id) !== id),
      );
      return response({});
    }),
    poolServiceCreatePool: vi.fn(({ pool }) => {
      const created = { ...pool, id: ++poolSequence };
      state.pools.get("default")!.push(created);
      return response({ pool: { ...created, id: String(created.id) } });
    }),
    miningGroupServiceGetMiningGroupCapability: vi.fn(() => response({ capability: state.capability })),
    miningGroupServiceListMiningGroups: vi.fn(() => response({ groups: state.groups })),
    miningGroupServiceListMiningGroupMemberships: vi.fn(() =>
      response({ memberships: [...state.members].map(([macAddress, groupId]) => ({ macAddress, groupId })) }),
    ),
    hashboardServiceGetContainerMap: vi.fn(() =>
      response({
        containerMap: {
          tanks: [
            {
              id: 1,
              modules: [
                {
                  id: 1,
                  slots: [A, B, C].map((mac, index) => ({
                    occupied: true,
                    hashboard: { macAddress: mac, location: { hashboardPosition: index + 1 } },
                  })),
                },
              ],
            },
          ],
        },
      }),
    ),
    miningServiceGetMiningStatus: vi.fn(() => response({ mining: state.mining, activeBoards: state.mining ? 3 : 0 })),
    miningServiceStopMining: vi.fn(() => {
      state.mining = false;
      return response({});
    }),
    miningServiceStartMining: vi.fn(() => {
      state.mining = true;
      const defaultGroup = group("default");
      defaultGroup.desiredMining = true;
      touch(defaultGroup);
      return response({});
    }),
    miningGroupServiceActivateMiningGroups: vi.fn(() => {
      state.capability.enabled = true;
      state.capability.stateRevision = "2";
      group("default").desiredMining = false;
      return response({ capability: state.capability });
    }),
    miningGroupServiceUpdateMiningGroup: vi.fn(({ id }, { expectedRevision, metadata }) => {
      const value = check(id, expectedRevision);
      value.name = metadata.name;
      return response({ group: touch(value) });
    }),
    miningGroupServiceDeleteMiningGroup: vi.fn(({ id, expectedRevision }) => {
      check(id, expectedRevision);
      if ([...state.members.values()].includes(id)) throw new Error("members remain");
      state.groups = state.groups.filter((group) => group.id !== id);
      return response({});
    }),
    miningGroupServiceCreateMiningGroup: vi.fn(({ metadata }) => {
      const value = {
        id: "local",
        revision: "1",
        name: metadata.name,
        externalRef: metadata.externalRef,
        desiredMining: false,
      };
      state.groups.push(value);
      return response({ group: value });
    }),
    miningGroupServiceGetMiningGroup: vi.fn(({ id }) => response({ group: group(id) })),
    miningGroupServiceListMiningGroupPools: vi.fn(({ groupId }) => response({ pools: state.pools.get(groupId) ?? [] })),
    miningGroupServiceReplaceMiningGroupPools: vi.fn(({ groupId }, { expectedRevision, pools }) => {
      const value = check(groupId, expectedRevision);
      const saved = pools.map(({ pool }: { pool: typeof publicPool }) => ({ ...pool, id: pool.id || ++poolSequence }));
      state.pools.set(groupId, saved);
      return response({ group: touch(value), pools: saved });
    }),
    miningGroupServiceUpdateMiningGroupIntent: vi.fn(({ id }, { expectedRevision, desiredMining }) => {
      const value = check(id, expectedRevision);
      value.desiredMining = desiredMining;
      return response({ group: touch(value) });
    }),
    miningGroupServiceRequestMiningGroupTransfer: vi.fn(
      ({ sourceGroupId, destinationGroupId, sourceExpectedRevision, destinationExpectedRevision, macAddresses }) => {
        const destination = check(destinationGroupId, destinationExpectedRevision);
        if (sourceGroupId) touch(check(sourceGroupId, sourceExpectedRevision));
        for (const mac of macAddresses) {
          if (state.members.get(mac) !== sourceGroupId) throw new Error("membership conflict");
          state.members.set(mac, destinationGroupId);
        }
        touch(destination);
        const operation = { id: "op-" + state.operations.size, phase: "COMPLETE", macAddresses };
        state.operations.set(operation.id, operation);
        return response({ operation });
      },
    ),
    miningGroupServiceGetMiningGroupOperation: vi.fn(({ id }) => response({ operation: state.operations.get(id) })),
  };
  const service = createPoolAssignment(client as unknown as MdkV2Client, (params) => ({
    ...params,
    headers: { "X-Frigate-Actor": "proto-os" },
  }));
  const addLocal = (members: string[] = []) => {
    state.groups.push({
      id: "saved",
      revision: "2",
      name: "Saved pools",
      externalRef: "proto-os:pools:saved",
      desiredMining: true,
    });
    state.pools.set("saved", [publicPool]);
    for (const mac of members) state.members.set(mac, "saved");
  };
  return { state, client, service, addLocal };
};

describe("CT1 selected-board pool assignment", () => {
  test("splits selected physical boards without changing their siblings or exposing a group form", async () => {
    const { service, state, client } = fixture();
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    const track = vi.fn();
    expect(await service.apply(plan, track)).toMatchObject({ state: "saved", confirmed: 1 });
    expect(state.members.get(A)).toBe("local");
    expect(state.members.get(B)).toBe("default");
    expect(state.members.get(C)).toBe("default");
    expect(client.miningGroupServiceReplaceMiningGroupPools.mock.calls[0][1]).toMatchObject({
      pools: [{ pool: { username: "account.region.worker" }, password: "secret" }],
    });
    expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(track.mock.calls)).not.toContain('"password"');
  });
  test("reuses a saved pool set by identity and leaves its credentials and members unchanged", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([C]);
    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "saved" });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(state.members.get(A)).toBe("saved");
    expect(state.members.get(C)).toBe("saved");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
  });
  test.each([{ members: [] }, { members: [A] }])(
    "keeps a saved set with members $members when assigning new pools",
    async ({ members }) => {
      const { service, state, client, addLocal } = fixture();
      addLocal(members);
      const original = clone(state.pools.get("saved"));
      const plan = await service.review({ macs: [A] }, { pools: [{ ...pool, username: "another.full.username" }] });
      expect(plan.destinationId).toBeUndefined();
      expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
      expect(client.miningGroupServiceCreateMiningGroup).toHaveBeenCalledOnce();
      expect(state.pools.get("saved")).toEqual(original);
      expect(state.members.get(A)).toBe("local");
    },
  );
  test("does not overwrite a local set that still has an unselected member", async () => {
    const { service, addLocal } = fixture();
    addLocal([A, B]);
    expect((await service.review({ macs: [A] }, { pools: [pool] })).destinationId).toBeUndefined();
  });
  test("a stale review refuses all mutations", async () => {
    const { service, state, client } = fixture();
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    state.groups[0].revision = "9";
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("changed");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });
  test("a module selection is rejected when the currently reported board set changes", async () => {
    const { service } = fixture();
    await expect(service.review({ macs: [A], modules: [[A, B]] }, { pools: [pool] })).rejects.toThrow(
      "selected modules changed",
    );
  });
  test.each([
    { externalRef: "fleet-configuration-id" },
    { requestedBoardWatts: 500 },
    { pllAlgorithm: "POWER_TARGET" },
  ])("refuses Fleet ownership or custom policy: %j", async (overrides) => {
    const { service, state } = fixture();
    Object.assign(state.groups[0], overrides);
    await expect(service.review({ macs: [A] }, { pools: [pool] })).rejects.toThrow(/Fleet-managed|custom power/);
  });
  test("does not change mining intent for mixed running and stopped assignments", async () => {
    const { service, state } = fixture();
    state.groups.push({ id: "stopped", revision: "1", desiredMining: false });
    state.members.set(B, "stopped");
    await expect(service.review({ macs: [A, B] }, { pools: [pool] })).rejects.toThrow("same mining intent");
  });
  test.each(["maxGroups", "maxBoardsPerGroup", "maxPoolsPerGroup"] as const)(
    "respects the reported %s limit before writing",
    async (limit) => {
      const { service, state, client } = fixture();
      state.capability[limit] = 1;
      await expect(service.review({ macs: [A, B] }, { pools: [pool, { ...pool, name: "Backup" }] })).rejects.toThrow(
        /limit|room/,
      );
      expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    },
  );
  test("rejects malformed pool URLs before writes", async () => {
    const { service } = fixture();
    await expect(service.review({ macs: [A] }, { pools: [{ ...pool, url: "https://wrong.example" }] })).rejects.toThrow(
      "Each pool",
    );
  });
  test("enables scoped assignments only after stopping legacy mining, then restores default intent", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    expect(plan.requiresActivation).toBe(true);
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.miningServiceStopMining.mock.invocationCallOrder[0]).toBeLessThan(
      client.miningGroupServiceActivateMiningGroups.mock.invocationCallOrder[0],
    );
    expect(state.groups.find((group) => group.id === "default")?.desiredMining).toBe(true);
    expect(state.members.get(B)).toBe("default");
  });
  test("restores saved mining intent even if no boards are active", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    state.mining = false;
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    expect(plan.mining).toBe(true);
    await service.apply(plan, vi.fn());
    expect(client.miningServiceStartMining).toHaveBeenCalledTimes(1);
  });
  test("does not restart stopped mining when boards are still draining", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    state.groups[0].desiredMining = false;
    client.miningServiceGetMiningStatus.mockResolvedValueOnce({ data: { mining: true, activeBoards: 3 } });
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    expect(plan.mining).toBe(false);
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
  });
  test("waits for active boards to stop before activating mining groups", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    client.miningServiceGetMiningStatus
      .mockResolvedValueOnce({ data: { mining: true, activeBoards: 3 } })
      .mockResolvedValueOnce({ data: { mining: false, activeBoards: 2 } })
      .mockImplementationOnce(async () => {
        expect(client.miningGroupServiceActivateMiningGroups).not.toHaveBeenCalled();
        return { data: { mining: false, activeBoards: 0 } };
      });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.miningServiceStopMining).toHaveBeenCalledTimes(1);
    expect(client.miningGroupServiceActivateMiningGroups).toHaveBeenCalledTimes(1);
    expect(client.miningServiceStartMining).toHaveBeenCalledTimes(1);
  });
  test("persists a recovery action when activation fails after stopping legacy mining", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    client.miningGroupServiceActivateMiningGroups.mockRejectedValueOnce(new Error("activation failed"));
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    let attempt: PoolAssignmentAttempt | undefined;
    await expect(
      service.apply(plan, (record) => {
        attempt = record;
      }),
    ).rejects.toThrow("activation failed");
    expect(attempt?.restoreLegacyMining).toBe(true);
    await service.restoreMining(attempt!, (record) => {
      attempt = record;
    });
    expect(client.miningServiceStartMining).toHaveBeenCalledTimes(1);
    expect(attempt?.restoreLegacyMining).toBe(false);
  });
  test("refuses a second transfer after a concurrent destination edit", async () => {
    const { service, state, client } = fixture();
    state.groups.push({ id: "other", revision: "1", desiredMining: true });
    state.members.set(B, "other");
    const send = client.miningGroupServiceRequestMiningGroupTransfer.getMockImplementation()!;
    client.miningGroupServiceRequestMiningGroupTransfer.mockImplementation(async (request) => {
      const response = await send(request);
      const destination = state.groups.find((group) => group.id === request.destinationGroupId)!;
      destination.revision = String(Number(destination.revision) + 1);
      return response;
    });
    const plan = await service.review({ macs: [A, B] }, { pools: [pool] });
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("destination configuration changed");
    expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledTimes(1);
  });
  test("a lost transfer response is recovered by readback without replaying writes", async () => {
    const { service, client } = fixture();
    const send = client.miningGroupServiceRequestMiningGroupTransfer.getMockImplementation()!;
    client.miningGroupServiceRequestMiningGroupTransfer.mockImplementation(async (request) => {
      await send(request);
      throw new Error("response lost");
    });
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    let attempt: PoolAssignmentAttempt | undefined;
    await expect(
      service.apply(plan, (value) => {
        attempt = value;
      }),
    ).rejects.toThrow("response lost");
    expect(await service.status(attempt!)).toMatchObject({ state: "saved" });
    expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledTimes(1);
  });
  test("storage failure refuses the first write", async () => {
    const { service, client } = fixture();
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    await expect(
      service.apply(plan, () => {
        throw new Error("storage unavailable");
      }),
    ).rejects.toThrow("storage unavailable");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });
  test("a lost credential acknowledgement cannot be certified by matching public fields", async () => {
    const { service, client, addLocal } = fixture();
    addLocal([A]);
    const original = (await service.listConfigurations()).find((configuration) => configuration.id === "saved")!;
    const send = client.miningGroupServiceReplaceMiningGroupPools.getMockImplementation()!;
    client.miningGroupServiceReplaceMiningGroupPools.mockImplementation(async (path, body) => {
      await send(path, body);
      throw new Error("response lost");
    });
    const plan = await service.review(
      { macs: [] },
      {
        editPoolSetId: original.id,
        expectedRevision: original.revision,
        name: original.name,
        pools: [{ ...pool, id: original.pools[0].id, password: "new-secret" }],
      },
    );
    let attempt: PoolAssignmentAttempt | undefined;
    await expect(
      service.apply(plan, (value) => {
        attempt = value;
      }),
    ).rejects.toThrow("response lost");
    expect(attempt?.configurationConfirmed).toBe(false);
    expect(await service.status(attempt!)).toMatchObject({ state: "unknown" });
    expect((await service.listConfigurations()).find((configuration) => configuration.id === "saved")?.pools).toEqual(
      original.pools,
    );
    expect(client.miningGroupServiceReplaceMiningGroupPools).toHaveBeenCalledTimes(1);
  });
  test("keeps the acknowledged credential receipt when readback fails", async () => {
    const { service, client, addLocal } = fixture();
    addLocal([A]);
    const plan = await service.review({ macs: [A] }, { pools: [{ ...pool, password: "new-secret" }] });
    client.miningGroupServiceGetMiningGroup.mockRejectedValueOnce(new Error("readback failed"));
    let attempt: PoolAssignmentAttempt | undefined;
    await expect(
      service.apply(plan, (record) => {
        attempt = record;
      }),
    ).rejects.toThrow("readback failed");
    expect(attempt).toMatchObject({
      configurationConfirmed: true,
      pools: [{ id: expect.stringMatching(/^[1-9]\d*$/) }],
    });
  });
  test("activation refuses an oversized default group before stopping the container", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    state.capability.maxBoardsPerGroup = 2;
    await expect(service.review({ macs: [A] }, { pools: [pool] })).rejects.toThrow("default group's hashboard limit");
    expect(client.miningServiceStopMining).not.toHaveBeenCalled();
  });
  test("saving a named configuration creates a reusable pool set without changing board assignments", async () => {
    const { service, client, state } = fixture();
    const plan = await service.review({ macs: [A] }, { name: "Production pools", pools: [pool], createOnly: true });
    const track = vi.fn();
    expect(plan.configurationOnly).toBe(true);
    expect(await service.apply(plan, track)).toMatchObject({
      state: "saved",
      message: "Pool configuration saved. Hashboard assignments are unchanged.",
    });
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(state.members.get(A)).toBe("default");
    expect((await service.inspect({ macs: [A] })).savedPools).toEqual(
      expect.arrayContaining([expect.objectContaining({ name: "Production pools" })]),
    );
  });
  test("configuration creation during activation restores default intent without transferring boards", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const plan = await service.review({ macs: [A] }, { name: "First configuration", pools: [pool], createOnly: true });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect([...state.members.values()]).toEqual(["default", "default", "default"]);
    expect(state.groups.find((group) => group.id === "default")?.desiredMining).toBe(true);
  });
});

describe("CT1 pool configuration library", () => {
  test("loads pool details only for the requested library page", async () => {
    const { service, state, client } = fixture();
    for (let index = 0; index < 30; index++) {
      const id = `group-${index}`;
      state.groups.push({ id, revision: "1", name: id, desiredMining: false });
      state.pools.set(id, [{ ...publicPool, id: index + 10 }]);
    }
    const page = await service.listConfigurationPage(0, 25);
    expect(page).toMatchObject({ total: 31 });
    expect(page.configurations).toHaveLength(25);
    expect(client.miningGroupServiceListMiningGroupPools).toHaveBeenCalledTimes(24);
    expect(client.miningGroupServiceGetMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceListMiningGroups).toHaveBeenCalledTimes(3);
  });
  test("rejects a configuration page when a group changes during the pool reads", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A]);
    client.miningGroupServiceListMiningGroupPools.mockImplementationOnce(async () => {
      state.groups.find((group) => group.id === "saved")!.revision = "3";
      return { data: { pools: [publicPool] } };
    });
    await expect(service.listConfigurationPage(0, 25)).rejects.toThrow("Pool configurations changed");
  });
  test("lists configurations while paused without reading board inventory", async () => {
    const { service, state, addLocal, client } = fixture();
    addLocal([A]);
    state.capability.suspended = true;
    expect(await service.listConfigurations()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ id: "default", isDefault: true, assignedCount: 2 }),
        expect.objectContaining({ id: "saved", assignedCount: 1 }),
      ]),
    );
    expect(client.hashboardServiceGetContainerMap).not.toHaveBeenCalled();
  });
  test("creates an unused configuration without an equipment selection and allows later running-board assignment", async () => {
    const { service, state, client } = fixture();
    const plan = await service.review({ macs: [] }, { name: "Library pools", createOnly: true, pools: [pool] });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(state.members.get(A)).toBe("default");
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect((await service.inspect({ macs: [A] })).savedPools.some((set) => set.name === "Library pools")).toBe(true);
    const assign = await service.review({ macs: [A] }, { savedPoolSetId: "local", pools: [] });
    expect(await service.apply(assign, vi.fn())).toMatchObject({ state: "saved" });
    expect(state.members.get(A)).toBe("local");
  });
  test("edits assigned configuration with revision checks and preserves credentials by pool id", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const plan = await service.review(
      { macs: [] },
      {
        editPoolSetId: "saved",
        expectedRevision: "2",
        name: "Renamed",
        pools: [{ ...pool, id: "1", preservePassword: true, password: "" }],
      },
    );
    expect(plan.target.macs).toEqual([A, B]);
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.miningGroupServiceReplaceMiningGroupPools.mock.calls[0][1].pools[0]).not.toHaveProperty("password");
    expect(client.miningGroupServiceUpdateMiningGroup).toHaveBeenCalledWith(
      { id: "saved" },
      expect.objectContaining({ updateMask: "name", metadata: { name: "Renamed" } }),
      expect.anything(),
    );
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(state.members.get(C)).toBe("default");
  });
  test.each(["default", "saved"])(
    "edits %s using membership when boards are isolated or absent from inventory",
    async (id) => {
      const { service, state, client, addLocal } = fixture();
      if (id === "saved") addLocal([A, B, C]);
      const original = (await service.listConfigurations()).find((configuration) => configuration.id === id)!;
      const getMap = client.hashboardServiceGetContainerMap.getMockImplementation()!;
      client.hashboardServiceGetContainerMap.mockImplementationOnce(async () => {
        const response = await getMap();
        const module = response.data.containerMap.tanks[0].modules[0];
        const isolatedBoard = { ...module.slots[0].hashboard, isolated: true };
        module.slots = [
          { ...module.slots[0], hashboard: isolatedBoard },
          { ...module.slots[1], hashboard: { ...module.slots[1].hashboard, macAddress: "" } },
        ];
        return response;
      });
      const username = "updated.worker";
      const plan = await service.review(
        { macs: [] },
        {
          editPoolSetId: id,
          expectedRevision: original.revision,
          expectedPools: original.pools,
          name: original.name,
          pools: [{ ...pool, id: original.pools[0].id, username }],
        },
      );
      expect(plan.boards).toEqual([{ mac: A, location: "Tank 1 · Module 1 · Board 1" }, { mac: B }, { mac: C }]);
      expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
      expect(state.pools.get(id)?.[0].username).toBe(username);
      expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(1);
      expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    },
  );
  test("keeps controller pool names attached to their identities when priorities change", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A]);
    state.pools.set("saved", [
      { ...publicPool, id: 1, name: "Primary", priority: 0 },
      { ...publicPool, id: 2, name: "Backup 1", url: "stratum+tcp://backup.example:3333", priority: 1 },
    ]);
    const plan = await service.review(
      { macs: [] },
      {
        editPoolSetId: "saved",
        expectedRevision: "2",
        name: "Saved pools",
        pools: [
          { ...pool, id: "2", name: "Primary", url: "stratum+tcp://backup.example:3333", preservePassword: true },
          { ...pool, id: "1", name: "Backup 1", preservePassword: true },
        ],
      },
    );
    await service.apply(plan, vi.fn());
    expect(client.miningGroupServiceReplaceMiningGroupPools.mock.calls[0][1].pools).toMatchObject([
      { pool: { id: 2, name: "Backup 1", priority: 0 } },
      { pool: { id: 1, name: "Primary", priority: 1 } },
    ]);
  });
  test("does not preserve a password across username or URL changes", async () => {
    const { service, addLocal } = fixture();
    addLocal();
    await expect(
      service.review(
        { macs: [] },
        {
          editPoolSetId: "saved",
          expectedRevision: "2",
          pools: [{ ...pool, id: "1", preservePassword: true, username: "different" }],
        },
      ),
    ).rejects.toThrow("Enter a password");
  });
  test("refuses deletion with assigned boards and deletes an unused local configuration", async () => {
    const { service, client, state, addLocal } = fixture();
    addLocal([A]);
    await expect(service.deleteConfiguration("saved", "2")).rejects.toThrow("Reassign");
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    state.members.set(A, "default");
    await service.deleteConfiguration("saved", "2");
    expect(client.miningGroupServiceDeleteMiningGroup).toHaveBeenCalledWith(
      expect.objectContaining({ id: "saved", expectedRevision: "2" }),
      expect.anything(),
    );
    expect(client.hashboardServiceGetContainerMap).not.toHaveBeenCalled();
  });
  test.each(["fleet-config", undefined])(
    "edits configuration owned by %s without changing its policy or assignments",
    async (externalRef) => {
      const { service, state, client, addLocal } = fixture();
      addLocal([A, B]);
      const group = state.groups.find((group) => group.id === "saved")!;
      Object.assign(group, { externalRef, requestedBoardWatts: 500, pllAlgorithm: "POWER_TARGET" });
      state.pools.set("saved", [{ ...publicPool, enabled: false, shareWeight: 2 }]);
      const plan = await service.review(
        { macs: [] },
        {
          editPoolSetId: "saved",
          expectedRevision: "2",
          name: "Updated pools",
          pools: [{ ...pool, id: "1", preservePassword: true, password: "" }],
        },
      );
      expect(plan.target.macs).toEqual([A, B]);
      expect(plan.boards).toEqual([
        { mac: A, location: "Tank 1 · Module 1 · Board 1" },
        { mac: B, location: "Tank 1 · Module 1 · Board 2" },
      ]);
      expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
      const write = client.miningGroupServiceReplaceMiningGroupPools.mock.calls[0][1];
      expect(write).toMatchObject({
        expectedRevision: "2",
        pools: [{ pool: { id: 1, enabled: false, shareWeight: 2 } }],
      });
      expect(write.pools[0]).not.toHaveProperty("password");
      expect(group).toMatchObject({ name: "Updated pools", requestedBoardWatts: 500, pllAlgorithm: "POWER_TARGET" });
      expect(group.externalRef).toBe(externalRef);
      expect([...state.members]).toEqual([
        [A, "saved"],
        [B, "saved"],
        [C, "default"],
      ]);
      expect(client.miningGroupServiceUpdateMiningGroupIntent).not.toHaveBeenCalled();
      expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
      expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    },
  );
  test.each(["revision", "membership"])(
    "refuses a Fleet configuration edit when its %s changes after review",
    async (change) => {
      const { service, state, client, addLocal } = fixture();
      addLocal([A]);
      const group = state.groups.find((group) => group.id === "saved")!;
      group.externalRef = "fleet-config";
      const plan = await service.review(
        { macs: [] },
        { editPoolSetId: "saved", expectedRevision: "2", pools: [{ ...pool, id: "1" }] },
      );
      if (change === "revision") group.revision = "3";
      else state.members.set(B, "saved");
      await expect(service.apply(plan, vi.fn())).rejects.toThrow("changed");
      expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
      expect(client.miningGroupServiceUpdateMiningGroup).not.toHaveBeenCalled();
      expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    },
  );
  test("deletes an unused unowned configuration with its current revision", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.groups.find((group) => group.id === "saved")!.externalRef = undefined;
    await expect(service.deleteConfiguration("saved", "1")).rejects.toThrow("changed");
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    state.members.set(A, "saved");
    await expect(service.deleteConfiguration("saved", "2")).rejects.toThrow("Reassign");
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    state.members.set(A, "default");
    await service.deleteConfiguration("saved", "2");
    expect(client.miningGroupServiceDeleteMiningGroup).toHaveBeenCalledWith(
      expect.objectContaining({ id: "saved", expectedRevision: "2" }),
      expect.anything(),
    );
    expect(state.groups.some((group) => group.id === "saved")).toBe(false);
  });
  test("blocks local deletion of an empty Fleet-owned configuration", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.groups.find((group) => group.id === "saved")!.externalRef = "fleet-config";
    expect(await service.listConfigurations()).toContainEqual(
      expect.objectContaining({ id: "saved", deletable: false }),
    );
    await expect(service.deleteConfiguration("saved", "2")).rejects.toThrow("Delete this configuration in Proto Fleet");
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
  });
  test("the controller's fixed default configuration cannot be deleted", async () => {
    const { service, client } = fixture();
    await expect(service.deleteConfiguration("default", "1")).rejects.toThrow("cannot be deleted");
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
  });
  test("edits Default through the legacy pool API with all its assigned boards in review", async () => {
    const { service, client } = fixture();
    const original = (await service.listConfigurations()).find((set) => set.id === "default")!;
    const plan = await service.review(
      { macs: [] },
      {
        name: original.name,
        editPoolSetId: original.id,
        expectedRevision: original.revision,
        expectedPools: original.pools,
        pools: [{ ...pool, id: "1", url: "stratum+tcp://updated:3333", password: "new-secret" }],
      },
    );
    expect(plan.target.macs).toEqual([A, B, C]);
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(client.poolServiceUpdatePool).toHaveBeenCalledWith(
      { poolId: "1" },
      expect.objectContaining({ password: "new-secret", updateMask: "url" }),
      expect.anything(),
    );
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test("a Default pool change invalidates a form even if the group revision is unchanged", async () => {
    const { service, state, client } = fixture();
    const original = (await service.listConfigurations())[0];
    state.pools.get("default")![0].url = "stratum+tcp://changed-elsewhere:3333";
    await expect(
      service.review(
        { macs: [] },
        {
          name: original.name,
          editPoolSetId: original.id,
          expectedRevision: original.revision,
          expectedPools: original.pools,
          pools: [{ ...pool, id: "1", preservePassword: true }],
        },
      ),
    ).rejects.toThrow("Default pools changed");
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });
  test("Default edits cannot rename the group or pretend to clear an existing password", async () => {
    const { service, client } = fixture();
    const original = (await service.listConfigurations())[0];
    const choice = {
      name: original.name,
      editPoolSetId: original.id,
      expectedRevision: original.revision,
      expectedPools: original.pools,
      pools: [{ ...pool, id: "1", password: "" }],
    };
    await expect(service.review({ macs: [] }, { ...choice, name: "Renamed" })).rejects.toThrow("cannot be renamed");
    await expect(service.review({ macs: [] }, choice)).rejects.toThrow("cannot clear");
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });
  test("Default edits detect a pool change after preparing the save without a group revision change", async () => {
    const { service, state, client } = fixture();
    const original = (await service.listConfigurations())[0];
    const plan = await service.review(
      { macs: [] },
      {
        name: original.name,
        editPoolSetId: original.id,
        expectedRevision: original.revision,
        expectedPools: original.pools,
        pools: [{ ...pool, id: "1", username: "edited.worker", password: "new-secret" }],
      },
    );
    state.pools.get("default")![0].username = "changed-in-another-session";
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("Default pools changed");
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });
  test("a lost final Default write acknowledgement remains uncertain even if public fields match", async () => {
    const { service, client } = fixture();
    const original = (await service.listConfigurations())[0];
    const plan = await service.review(
      { macs: [] },
      {
        name: original.name,
        editPoolSetId: original.id,
        expectedRevision: original.revision,
        expectedPools: original.pools,
        pools: [{ ...pool, id: "1", username: "edited.worker", password: "new-secret" }],
      },
    );
    const update = client.poolServiceUpdatePool.getMockImplementation()!;
    client.poolServiceUpdatePool.mockImplementation(async (path, data) => {
      const response = await update(path, data);
      if (data.password) throw new Error("Lost acknowledgement");
      return response;
    });
    const track = vi.fn();
    await expect(service.apply(plan, track)).rejects.toThrow("Lost acknowledgement");
    expect(client.poolServiceUpdatePool).toHaveBeenCalledTimes(1);
    expect(await service.status(track.mock.lastCall![0])).toMatchObject({ state: "unknown" });
  });
});

test("a configuration editor based on an old revision cannot overwrite newer pool settings", async () => {
  const { service, client, addLocal } = fixture();
  addLocal();
  await expect(
    service.review({ macs: [] }, { editPoolSetId: "saved", expectedRevision: "1", pools: [pool] }),
  ).rejects.toThrow("configuration changed");
  expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
});

test("new pool IDs come from the controller and are kept in the recovery record", async () => {
  const { service, client } = fixture();
  const plan = await service.review({ macs: [] }, { name: "New configuration", createOnly: true, pools: [pool] });
  const track = vi.fn();
  expect(await service.apply(plan, track)).toMatchObject({ state: "saved" });
  expect(client.miningGroupServiceReplaceMiningGroupPools.mock.calls[0][1].pools[0].pool.id).toBe(0);
  expect(track.mock.lastCall?.[0].pools[0].id).toBe("4");
});

test("recovery does not confirm pool credentials after a concurrent password rotation", async () => {
  const { service, client, state, addLocal } = fixture();
  addLocal();
  const plan = await service.review(
    { macs: [] },
    {
      name: "Saved pools",
      editPoolSetId: "saved",
      expectedRevision: "2",
      pools: [{ ...pool, id: "1", password: "new-password" }],
    },
  );
  const track = vi.fn();
  expect(await service.apply(plan, track)).toMatchObject({ state: "saved" });
  const attempt = track.mock.lastCall![0];
  expect(attempt.destinationRevision).toBeDefined();
  const samePools = state.pools.get("saved");
  await client.miningGroupServiceReplaceMiningGroupPools(
    { groupId: "saved" },
    {
      expectedRevision: attempt.destinationRevision,
      pools: samePools!.map((pool) => ({ pool, password: "other-password" })),
    },
  );
  expect(state.pools.get("saved")).toEqual(samePools);
  expect(await service.status(attempt)).toMatchObject({ state: "unknown" });
});
