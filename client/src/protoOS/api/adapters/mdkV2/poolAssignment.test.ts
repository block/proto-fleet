import { describe, expect, test, vi } from "vitest";
import { Code } from "@connectrpc/connect";
import { type PoolAssignmentAttempt, type PoolAssignmentTarget, WorkerNameUpdateRolledBack } from "../poolAssignment";
import type { MdkV2Client } from "./connectClient";
import type {
  V2AuditEvent,
  V2MiningGroupInventoryIssue,
  V2MiningGroupPoolRecord,
  V2MiningGroupRecord,
  V2UpdateDefaultMiningGroupWorkerNamesRequest,
} from "./generated/generatedApi";
import { createPoolAssignment } from "./poolAssignment";
import { poolRecoveryFingerprint, recoveryPoolURL } from "./poolRecoveryEvidence";

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
      poolCopySupported: false,
      defaultPoolWritesSupported: true as boolean | undefined,
      defaultPoolWorkerNameUpdateSupported: true as boolean | undefined,
      maxBoardsPerGroup: 3,
      maxPoolsPerGroup: 3,
      assignmentGranularity: "PHYSICAL_HASHBOARD",
    },
    groups: [
      { id: "default", name: "Default", revision: "1", defaultPoolRevision: "1", desiredMining: true },
    ] as V2MiningGroupRecord[],
    inventoryIssues: [] as V2MiningGroupInventoryIssue[],
    members: new Map([
      [A, "default"],
      [B, "default"],
      [C, "default"],
    ]),
    boardSlots: [A, B, C],
    pools: new Map<string, V2MiningGroupPoolRecord[]>([["default", [publicPool]]]),
    operations: new Map<string, { id: string; phase: string; macAddresses: string[]; destinationGroupId?: string }>(),
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
  let groupSequence = 0;
  let poolSequence = 3;
  const client = {
    systemServiceGetSystemInfo: vi.fn(() => response({ info: { id: "ct1" } })),
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
    miningGroupServiceListMiningGroups: vi.fn(() =>
      response({
        groups: state.groups,
        inventoryIssues: state.inventoryIssues,
        pendingOperations: [...state.operations.values()].filter(
          ({ phase }) => !["COMPLETE", "FAILED"].includes(phase),
        ),
      }),
    ),
    systemServiceListAuditEvents: vi.fn((_query: { action?: string; target?: string }) =>
      response({ events: [] as V2AuditEvent[] }),
    ),
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
                  slots: state.boardSlots.map((mac, index) => ({
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
      group("default").desiredMining = false;
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
      const sequence = ++groupSequence;
      const value = {
        id: sequence === 1 ? "local" : "local-" + sequence,
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
    miningGroupServiceUpdateDefaultMiningGroupWorkerNames: vi.fn(
      ({
        expectedRevision,
        expectedDefaultPoolRevision,
        updates = [],
      }: V2UpdateDefaultMiningGroupWorkerNamesRequest) => {
        const value = check("default", expectedRevision!);
        if (expectedDefaultPoolRevision !== value.defaultPoolRevision) throw new Error("Default pools changed");
        const original = state.pools.get("default")!;
        const saved = original.map((pool) => ({
          ...pool,
          username: updates.find(({ poolId }) => poolId === String(pool.id))?.username ?? pool.username,
        }));
        state.pools.set("default", saved);
        value.defaultPoolRevision = String(Number(value.defaultPoolRevision) + updates.length);
        return response({ group: value, pools: saved.map((pool) => ({ ...pool, id: String(pool.id) })) });
      },
    ),
    miningGroupServiceCopyMiningGroupPools: vi.fn(
      (
        { groupId },
        { expectedRevision, sourceGroupId, sourceExpectedRevision, sourceExpectedDefaultPoolRevision, pools },
      ) => {
        const value = check(groupId, expectedRevision);
        const source = check(sourceGroupId, sourceExpectedRevision);
        if (sourceGroupId === "default" && sourceExpectedDefaultPoolRevision !== source.defaultPoolRevision)
          throw new Error("Default pools changed");
        const saved = pools.map(({ source, username }: { source: V2MiningGroupPoolRecord; username: string }) => ({
          ...source,
          username,
          id: ++poolSequence,
        }));
        state.pools.set(groupId, saved);
        return response({ group: touch(value), pools: saved });
      },
    ),
    miningGroupServiceUpdateMiningGroupIntent: vi.fn(({ id }, { expectedRevision, desiredMining }) => {
      const value = check(id, expectedRevision);
      value.desiredMining = desiredMining;
      return response({ group: touch(value) });
    }),
    miningGroupServiceRequestMiningGroupTransfer: vi.fn(
      ({
        sourceGroupId,
        destinationGroupId,
        sourceExpectedRevision,
        sourceExpectedDefaultPoolRevision,
        destinationExpectedRevision,
        macAddresses,
      }) => {
        const destination = check(destinationGroupId, destinationExpectedRevision);
        if (sourceGroupId) {
          const source = check(sourceGroupId, sourceExpectedRevision);
          if (sourceExpectedDefaultPoolRevision && sourceExpectedDefaultPoolRevision !== source.defaultPoolRevision)
            throw new Error("Default pools changed");
          touch(source);
        }
        for (const mac of macAddresses) {
          if ((state.members.get(mac) ?? "") !== sourceGroupId) throw new Error("membership conflict");
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
  test("copies pool settings for a partial module and leaves sibling usernames intact", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const backup = {
      ...publicPool,
      id: 2,
      name: "Failover",
      priority: 1,
      url: "stratum+tcp://backup.example:3333",
      username: "fixed",
      enabled: false,
      shareWeight: 1,
    };
    const sourcePools = [publicPool, backup];
    state.pools.set("saved", sourcePools);
    const target = { macs: [A], modules: [[A, B, C]] };
    const source = (await service.inspect(target)).savedPools.find(({ id }) => id === "saved")!;
    const username = "Fixed.Account..Region.Worker ";
    const plan = await service.review(target, {
      name: "Board one pools",
      usernameSource: source,
      pools: [
        { ...pool, username },
        { ...pool, username: backup.username, password: "" },
      ],
    });
    expect(plan.capacity).toEqual({ groupsUsed: 3, maxGroups: 10, maxBoards: 3, maxPools: 3, destinationBoards: 1 });
    expect(plan.transfers).toEqual([{ source: "saved", macs: [A] }]);
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();

    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    expect(await service.apply(plan, track)).toMatchObject({ state: "saved", confirmed: 1 });
    expect(client.miningGroupServiceReplaceMiningGroupPools).toHaveBeenCalledExactlyOnceWith(
      { groupId: "local" },
      expect.objectContaining({
        pools: [
          { pool: { ...publicPool, id: 0, username, shareWeight: undefined }, password: "secret" },
          { pool: { ...backup, id: 0 }, password: "" },
        ],
      }),
      expect.anything(),
    );
    expect(state.pools.get("saved")).toEqual(sourcePools);
    expect(state.members).toEqual(
      new Map([
        [A, "local"],
        [B, "saved"],
        [C, "default"],
      ]),
    );
    expect((await service.inspect({ macs: [A, B] })).mixedConfigurations).toBe(true);
    expect(await service.status(track.mock.lastCall![0])).toMatchObject({ state: "saved", confirmed: 1 });
    expect(client.miningGroupServiceReplaceMiningGroupPools).toHaveBeenCalledOnce();
  });
  test("rejects a duplicate weighted credential before creating the copy", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.pools.set("saved", [
      publicPool,
      { ...publicPool, id: 2, name: "Backup", priority: 1, username: "other.account", shareWeight: 1 },
    ]);
    const target = { macs: [A] };
    const usernameSource = (await service.inspect(target)).savedPools.find(({ id }) => id === "saved")!;
    await expect(service.review(target, { usernameSource, pools: [pool, pool] })).rejects.toThrow(
      "Weighted pools must use distinct URL and username pairs.",
    );
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });
  test("checks the Default source again before copying credentials", async () => {
    const { service, state, client } = fixture();
    const target = { macs: [A] };
    const [usernameSource] = (await service.inspect(target)).savedPools;
    const plan = await service.review(target, { usernameSource, pools: [{ ...pool, username: "fixed-new" }] });
    state.pools.set("default", [{ ...publicPool, username: "changed-without-group-revision" }]);
    const track = vi.fn();
    await expect(service.apply(plan, track)).rejects.toThrow("The source pools changed.");
    expect(track).not.toHaveBeenCalled();
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });
  test.each([
    { change: "the selected board moves", error: "The reviewed board locations changed." },
    { change: "Default pools change", error: "The source pools changed." },
  ])("stops subsequent writes when $change during group creation", async ({ change, error }) => {
    const { service, state, client } = fixture();
    const target = { macs: [A] };
    const [usernameSource] = (await service.inspect(target)).savedPools;
    const plan = await service.review(target, { usernameSource, pools: [{ ...pool, username: "fixed-new" }] });
    const create = client.miningGroupServiceCreateMiningGroup.getMockImplementation()!;
    client.miningGroupServiceCreateMiningGroup.mockImplementationOnce(async (request) => {
      const result = await create(request);
      if (change === "the selected board moves") state.boardSlots = [B, A, C];
      else state.pools.set("default", [{ ...publicPool, username: "changed-during-create" }]);
      return result;
    });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    await expect(service.apply(plan, track)).rejects.toThrow(error);
    expect(track.mock.lastCall?.[0]).toMatchObject({ destinationId: "local", configurationConfirmed: false });
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test.each([
    { change: "moves", slots: [B, A, C] },
    { change: "is replaced", slots: ["aa:bb:cc:00:00:04", B, C] },
  ])("requires fresh review when a selected physical board $change", async ({ slots }) => {
    const { service, state, client } = fixture();
    const target = { macs: [A] };
    const [usernameSource] = (await service.inspect(target)).savedPools;
    const plan = await service.review(target, { usernameSource, pools: [{ ...pool, username: "fixed-new" }] });
    state.boardSlots = slots;
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("Review the selection again");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test("counts existing and selected boards once and reuses a group when all group places are in use", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([C]);
    state.capability.maxGroups = 2;
    state.capability.maxBoardsPerGroup = 2;
    const target = { macs: [A, B] };
    const inspection = await service.inspect(target);
    expect(inspection.capacity).toMatchObject({ groupsUsed: 2, maxGroups: 2, maxBoards: 2 });
    expect(inspection.savedPools.map(({ id, projectedBoards }) => [id, projectedBoards])).toEqual([
      ["default", 2],
      ["saved", 3],
    ]);
    expect(client.miningGroupServiceGetMiningGroup).not.toHaveBeenCalled();
    await expect(service.review(target, { pools: [], savedPoolSetId: "saved" })).rejects.toThrow("hashboard limit (2)");
    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "saved" });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved", confirmed: 1 });
    expect(state.members).toEqual(
      new Map([
        [A, "saved"],
        [B, "default"],
        [C, "saved"],
      ]),
    );
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
  });
  test("adopts unassigned boards into an eligible saved group", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.members.delete(A);
    state.members.delete(B);
    const target = { macs: [A, B], unassignedOnly: true };
    expect((await service.inspect(target)).savedPools.map(({ id }) => id)).toEqual(["default", "saved"]);
    const plan = await service.review(target, { pools: [], savedPoolSetId: "saved" });
    const track = vi.fn();
    expect(await service.apply(plan, track)).toMatchObject({ state: "saved", confirmed: 2 });
    expect(track).toHaveBeenCalledWith(expect.objectContaining({ unassignedOnly: true }));
    expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledWith(
      expect.objectContaining({ sourceGroupId: "", sourceExpectedRevision: "0", macAddresses: [A, B] }),
      expect.anything(),
    );
    expect(state.members).toEqual(
      new Map([
        [C, "default"],
        [A, "saved"],
        [B, "saved"],
      ]),
    );
  });
  test.each([
    { destination: "default", members: [B] },
    { destination: "saved", members: [B] },
    { destination: "saved", members: [] },
  ])(
    "adopts into running $destination with existing members $members without changing intent",
    async ({ destination, members }) => {
      const { service, state, client, addLocal } = fixture();
      addLocal(members);
      state.members.delete(A);
      const target = { macs: [A], unassignedOnly: true };
      expect((await service.inspect(target)).savedPools).toContainEqual(expect.objectContaining({ id: destination }));
      const plan = await service.review(target, { pools: [], savedPoolSetId: destination });
      expect(plan.mining).toBe(true);
      expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved", confirmed: 1 });
      expect(state.members.get(A)).toBe(destination);
      expect(state.groups.find(({ id }) => id === destination)?.desiredMining).toBe(true);
      expect(client.miningGroupServiceUpdateMiningGroupIntent).not.toHaveBeenCalled();
    },
  );
  test("requires the selected boards to remain unassigned when the operator reviews the change", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.members.delete(A);
    const target = { macs: [A], unassignedOnly: true };
    await service.inspect(target);
    state.members.set(A, "default");
    await expect(service.review(target, { pools: [], savedPoolSetId: "saved" })).rejects.toThrow(
      "Group assignments changed",
    );
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test("blocks reported inventory issues only for the affected unassigned hashboards", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.members.delete(A);
    state.members.delete(B);
    state.inventoryIssues = [
      { macAddress: A, reason: "unassigned_verified_board" },
      { macAddress: B.toUpperCase(), reason: "missing_verified_feed" },
    ];

    expect((await service.inspect({ macs: [A], unassignedOnly: true })).savedPools.map(({ id }) => id)).toEqual([
      "default",
      "saved",
    ]);
    const target = { macs: [A, B], unassignedOnly: true };
    await expect(service.inspect(target)).rejects.toThrow(`${B} · Verify this hashboard's power feed`);
    await expect(service.review(target, { pools: [], savedPoolSetId: "saved" })).rejects.toThrow(
      `${B} · Verify this hashboard's power feed`,
    );
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test("rechecks inventory issues before the first write to adopt unassigned hashboards", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal();
    state.members.delete(A);
    const plan = await service.review({ macs: [A], unassignedOnly: true }, { pools: [], savedPoolSetId: "saved" });
    state.inventoryIssues = [{ macAddress: A, reason: "conflicting_psu_identity" }];
    const track = vi.fn();

    await expect(service.apply(plan, track)).rejects.toThrow("power supply identity conflict");
    expect(track).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateMiningGroupIntent).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });
  test("rechecks inventory issues after preparing the destination and before moving boards", async () => {
    const { service, state, client } = fixture();
    state.members.delete(A);
    state.members.delete(B);
    const plan = await service.review({ macs: [A, B], unassignedOnly: true }, { pools: [pool] });
    const replacePools = client.miningGroupServiceReplaceMiningGroupPools.getMockImplementation()!;
    client.miningGroupServiceReplaceMiningGroupPools.mockImplementation(async (...args) => {
      const response = await replacePools(...args);
      state.inventoryIssues = [{ macAddress: B, reason: "missing_current_binding" }];
      return response;
    });

    await expect(service.apply(plan, vi.fn())).rejects.toThrow(`${B} · Restore this hashboard's controller connection`);
    expect(client.miningGroupServiceReplaceMiningGroupPools).toHaveBeenCalledOnce();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(state.members).toEqual(new Map([[C, "default"]]));
  });
  test("lists Default and assigns a board to it using the controller's saved pools", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    expect((await service.inspect({ macs: [A] })).savedPools.map((set) => set.id)).toEqual(["default", "saved"]);

    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "default" });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved", confirmed: 1 });
    expect(state.members.get(A)).toBe("default");
    expect(state.members.get(B)).toBe("saved");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateMiningGroupIntent).not.toHaveBeenCalled();
    expect((await service.inspect({ macs: [A] })).currentPoolSetId).toBe("default");
  });
  test("checks saved pool identity from redacted recovery data", async () => {
    const { service, state, addLocal } = fixture();
    addLocal([A]);
    const url = "stratum+tcp://pool.example:3333?token=original";
    state.pools.set("default", [{ ...publicPool, url }]);
    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "default" });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    await service.apply(plan, track);
    const [attempt] = track.mock.lastCall!;
    const recovered = {
      ...attempt,
      pools: attempt.pools.map((pool) => ({ ...pool, url: recoveryPoolURL(pool.url) })),
      poolFingerprint: poolRecoveryFingerprint(attempt.pools),
    };
    expect((await service.status(recovered)).state).toBe("saved");
    state.pools.set("default", [{ ...publicPool, url: url.replace("original", "new") }]);
    expect((await service.status(recovered)).state).not.toBe("saved");
  });
  test("reuses an existing pool set without an ownership marker", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([C]);
    delete state.groups[1].externalRef;
    expect((await service.inspect({ macs: [A] })).savedPools.map((set) => set.id)).toContain("saved");

    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "saved" });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved", confirmed: 1 });
    expect(state.members.get(A)).toBe("saved");
    expect(client.miningGroupServiceUpdateMiningGroup).not.toHaveBeenCalled();
  });
  test("reports mixed assignments without choosing a current configuration", async () => {
    const { service, addLocal } = fixture();
    addLocal([C]);
    expect(await service.inspect({ macs: [A, C] })).toMatchObject({
      mixedConfigurations: true,
      currentPoolSetId: undefined,
    });
    expect(await service.inspect({ macs: [A, B] })).toMatchObject({
      mixedConfigurations: false,
      currentPoolSetId: "default",
    });
  });
  test("excludes a pool set managed outside ProtoOS", async () => {
    const { service, state, addLocal } = fixture();
    addLocal([C]);
    state.groups[1].externalRef = "fleet-configuration-id";
    expect((await service.inspect({ macs: [A] })).savedPools.map((set) => set.id)).toEqual(["default"]);
    await expect(service.review({ macs: [A] }, { pools: [], savedPoolSetId: "saved" })).rejects.toThrow(
      "no longer available",
    );
  });
  test("excludes Default if using it would change the selected board's mining intent", async () => {
    const { service, state, addLocal } = fixture();
    addLocal([A, B, C]);
    state.groups[0].desiredMining = false;
    expect((await service.inspect({ macs: [A] })).savedPools.map((set) => set.id)).toEqual(["saved"]);
    await expect(service.review({ macs: [A] }, { pools: [], savedPoolSetId: "default" })).rejects.toThrow(
      "no longer available",
    );
  });
  test("refuses to move a board if Default pool settings changed after review", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A]);
    const plan = await service.review({ macs: [A] }, { pools: [], savedPoolSetId: "default" });
    state.pools.set("default", [{ ...publicPool, url: "stratum+tcp://changed.example:3333" }]);
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("Default pool settings changed");
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
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
  test.each([
    ["maxGroups", "All mining groups are in use"],
    ["maxBoardsPerGroup", "hashboard limit (1)"],
    ["maxPoolsPerGroup", "pool limit (1)"],
  ] as const)("respects the reported %s limit before writing", async (limit, message) => {
    const { service, state, client } = fixture();
    state.capability[limit] = 1;
    const backup = { ...pool, name: "Backup", url: "stratum+tcp://backup.example:3333" };
    await expect(service.review({ macs: [A, B] }, { pools: [pool, backup] })).rejects.toThrow(message);
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });
  test("rejects malformed pool URLs before writes", async () => {
    const { service } = fixture();
    await expect(service.review({ macs: [A] }, { pools: [{ ...pool, url: "https://wrong.example" }] })).rejects.toThrow(
      "Each pool",
    );
  });
  test("pauses assignment after activation until an operator starts mining", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    expect(plan.requiresActivation).toBe(true);
    let attempt: PoolAssignmentAttempt | undefined;
    const track = (record: PoolAssignmentAttempt) => {
      attempt = record;
    };
    expect(await service.apply(plan, track)).toMatchObject({ state: "unknown" });
    expect(client.miningServiceStopMining.mock.invocationCallOrder[0]).toBeLessThan(
      client.miningGroupServiceActivateMiningGroups.mock.invocationCallOrder[0],
    );
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(state.groups.find((group) => group.id === "default")?.desiredMining).toBe(false);
    expect(await service.status(attempt!)).toMatchObject({ state: "unknown" });
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();

    await client.miningServiceStartMining();
    expect(await service.checkMiningRecovery(attempt!, "device:ct1", track)).toMatchObject({ state: "unknown" });
    expect(attempt?.restoreLegacyMining).toBe(false);
    expect(client.miningServiceStartMining).toHaveBeenCalledTimes(1);
    const reviewed = await service.review({ macs: [A] }, { pools: [pool] });
    expect(await service.apply(reviewed, track)).toMatchObject({ state: "saved" });
    expect(state.members.get(B)).toBe("default");
  });
  test("offers manual start for saved running intent even if no boards are active", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    state.mining = false;
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    expect(plan.mining).toBe(true);
    const track = vi.fn();
    await service.apply(plan, track);
    expect(track).toHaveBeenLastCalledWith(expect.objectContaining({ restoreLegacyMining: true }));
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
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
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "unknown" });
    expect(client.miningServiceStopMining).toHaveBeenCalledTimes(1);
    expect(client.miningGroupServiceActivateMiningGroups).toHaveBeenCalledTimes(1);
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
  });
  test("checks the original controller without starting mining after failed activation", async () => {
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
    const track = vi.fn();
    await expect(service.checkMiningRecovery(attempt!, "device:other-ct1", track)).rejects.toThrow(
      "The controller changed.",
    );
    expect(track).not.toHaveBeenCalled();
    const result = await service.checkMiningRecovery(attempt!, "device:ct1", (record) => {
      attempt = record;
    });
    expect(result.message).toContain("Default mining is set to stop.");
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
    expect(attempt?.restoreLegacyMining).toBe(false);
  });
  test("does not replay an unacknowledged start after a later operator stop", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const start = client.miningServiceStartMining.getMockImplementation()!;
    client.miningServiceStartMining.mockImplementationOnce(async () => {
      await start();
      throw new Error("start response lost");
    });
    const plan = await service.review({ macs: [A] }, { pools: [pool] });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    expect(await service.apply(plan, track)).toMatchObject({ state: "unknown" });
    await expect(client.miningServiceStartMining()).rejects.toThrow("start response lost");
    expect(state.groups[0].desiredMining).toBe(true);
    const [attempt] = track.mock.lastCall!;
    await client.miningServiceStopMining();

    const result = await service.checkMiningRecovery(attempt, "device:ct1", track);
    expect(result.message).toContain("Default mining is set to stop.");
    expect(client.miningServiceStartMining).toHaveBeenCalledTimes(1);
    expect(state.groups[0].desiredMining).toBe(false);
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
  test("requires board inventory before activating groups to create an unused configuration", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const target = { macs: [] };
    const choice = { name: "First configuration", pools: [pool], createOnly: true };
    client.hashboardServiceGetContainerMap.mockRejectedValueOnce(new Error("Board inventory unavailable"));
    await expect(service.review(target, choice)).rejects.toThrow("Board inventory unavailable");

    const plan = await service.review(target, choice);
    expect(plan.requiresActivation).toBe(true);
    client.hashboardServiceGetContainerMap.mockRejectedValueOnce(new Error("Board inventory unavailable"));
    await expect(service.apply(plan, vi.fn())).rejects.toThrow("Board inventory unavailable");
    expect(client.miningServiceStopMining).not.toHaveBeenCalled();
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
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
  test("saves an unused configuration during activation without starting mining", async () => {
    const { service, state, client } = fixture();
    state.capability.enabled = false;
    const plan = await service.review({ macs: [A] }, { name: "First configuration", pools: [pool], createOnly: true });
    const track = vi.fn();
    expect(await service.apply(plan, track)).toMatchObject({ state: "saved" });
    expect(track).toHaveBeenLastCalledWith(expect.objectContaining({ restoreLegacyMining: true }));
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateMiningGroupIntent).not.toHaveBeenCalled();
    expect(client.miningServiceStartMining).not.toHaveBeenCalled();
    expect([...state.members.values()]).toEqual(["default", "default", "default"]);
    expect(state.groups.find((group) => group.id === "default")?.desiredMining).toBe(false);
  });
});

describe("CT1 worker names", () => {
  const review = async (
    service: ReturnType<typeof fixture>["service"],
    target: PoolAssignmentTarget,
    namesByMac: Record<string, string>,
  ) => {
    const sources = await service.inspectWorkerNames(target);
    return service.reviewWorkerNames(target, { namesByMac, sources });
  };

  test("changes the worker suffix on all pools without replacing credentials or moving boards", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const backup = {
      ...publicPool,
      id: 2,
      name: "Failover",
      priority: 1,
      url: "stratum+tcp://backup.example:3333",
      username: "Fixed.Account..Region.Worker",
      enabled: false,
      shareWeight: 1,
    };
    state.pools.set("saved", [publicPool, backup]);
    const result = await review(service, { macs: [A, B.toUpperCase()] }, { [A]: "night", [B]: "night" });
    expect(result).toMatchObject({ unchangedCount: 0, capacity: { groupsUsed: 2 } });
    expect(result.groups[0].assignment.target.macs).toEqual([A, B]);
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    const saved = vi.fn();
    expect(await service.applyWorkerNames(result, { track: vi.fn(), saved })).toMatchObject({
      state: "saved",
      confirmed: 2,
    });
    expect(client.miningGroupServiceReplaceMiningGroupPools).toHaveBeenCalledExactlyOnceWith(
      { groupId: "saved" },
      expect.objectContaining({
        pools: [
          { pool: { ...publicPool, username: "account.region.night", shareWeight: undefined } },
          { pool: { ...backup, username: "Fixed.Account..Region.night" } },
        ],
      }),
      expect.anything(),
    );
    expect(state.members.get(A)).toBe("saved");
    expect(state.members.get(B)).toBe("saved");
    expect(state.members.get(C)).toBe("default");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(saved).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ macs: [A, B] }));
  });

  test("uses a guarded Default write, preserves inactive drafts, and arms recovery from the receipt", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const inactive = { id: 2, name: "Unfinished", url: " unfinished ", username: "", enabled: false, priority: 8 };
    state.pools.set("default", [{ ...publicPool, username: "wallet" }, inactive]);
    const result = await review(service, { macs: [C] }, { [C]: "rig-01" });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    expect(await service.applyWorkerNames(result, { track, saved: vi.fn() })).toMatchObject({
      state: "saved",
      confirmed: 1,
    });
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateDefaultMiningGroupWorkerNames).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        expectedRevision: "1",
        expectedDefaultPoolRevision: "1",
        updates: [{ poolId: "1", username: "wallet.rig-01" }],
      }),
      expect.anything(),
    );
    expect(state.pools.get("default")![1]).toEqual(inactive);
    expect(state.members.get(C)).toBe("default");
    expect(await service.status(track.mock.lastCall![0])).toMatchObject({ state: "saved" });
    state.groups[0].defaultPoolRevision = "3";
    expect(await service.status(track.mock.lastCall![0])).toMatchObject({ state: "unknown" });
  });

  test.each([
    { defect: "an unchanged revision", group: { defaultPoolRevision: "1" } },
    { defect: "a different group ID", group: { id: "other" } },
    { defect: "a different username", username: "account.region.other" },
  ])("keeps the Default update unconfirmed when the receipt has $defect", async ({ group, username }) => {
    const { service, client } = fixture();
    const result = await review(service, { macs: [A, B, C] }, { [A]: "night", [B]: "night", [C]: "night" });
    const update = client.miningGroupServiceUpdateDefaultMiningGroupWorkerNames.getMockImplementation()!;
    client.miningGroupServiceUpdateDefaultMiningGroupWorkerNames.mockImplementationOnce(async (request) => {
      const { data } = await update(request);
      return {
        data: {
          group: { ...data.group, ...group },
          pools: username ? data.pools.map((pool) => ({ ...pool, username })) : data.pools,
        },
      };
    });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    const saved = vi.fn();

    await expect(service.applyWorkerNames(result, { track, saved })).rejects.toThrow(
      "The controller did not confirm the saved Default worker names.",
    );
    expect(saved).not.toHaveBeenCalled();
    const attempt = track.mock.lastCall![0];
    expect(attempt.configurationConfirmed).toBe(false);
    expect(await service.status(attempt)).toMatchObject({ state: "unknown" });
  });

  test("never overwrites an account changed after the last read for a Default rename", async () => {
    const { service, state, client } = fixture();
    const target = { macs: [A, B, C] };
    const result = await review(service, target, { [A]: "night", [B]: "night", [C]: "night" });
    const update = client.miningGroupServiceUpdateDefaultMiningGroupWorkerNames.getMockImplementation()!;
    client.miningGroupServiceUpdateDefaultMiningGroupWorkerNames.mockImplementationOnce((request) => {
      state.pools.set("default", [{ ...publicPool, username: "new-account.worker" }]);
      state.groups[0].defaultPoolRevision = "2";
      return update(request);
    });
    await expect(service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).rejects.toThrow(
      "Default pools changed",
    );
    expect(state.pools.get("default")![0].username).toBe("new-account.worker");
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });

  test("requires the guarded endpoint for an in-place Default rename", async () => {
    const { service, state, client } = fixture();
    state.capability.defaultPoolWorkerNameUpdateSupported = undefined;
    await expect(review(service, { macs: [A, B, C] }, { [A]: "night", [B]: "night", [C]: "night" })).rejects.toThrow(
      "Update the controller software",
    );
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });

  test.each<{ target: PoolAssignmentTarget; namesByMac: Record<string, string>; scenario: string }>([
    { target: { macs: [A] }, namesByMac: { [A]: "night" }, scenario: "only one member is selected" },
    {
      target: { macs: [A, B] },
      namesByMac: { [A]: "rig-01", [B]: "rig-02" },
      scenario: "members have different names",
    },
  ])("leaves credentials and assignments intact when $scenario", async ({ target, namesByMac }) => {
    const { service, client, addLocal } = fixture();
    addLocal([A, B]);
    await expect(review(service, target, namesByMac)).rejects.toThrow("Update the controller software");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });

  test("copies Default settings into a new group and moves only the selected hashboard", async () => {
    const { service, client, state } = fixture();
    state.capability.poolCopySupported = true;
    const backup = {
      ...publicPool,
      id: 2,
      name: "Disabled backup",
      username: "backup.old",
      enabled: false,
    };
    state.pools.set("default", [publicPool, backup]);

    const result = await review(service, { macs: [A] }, { [A]: "rig-01" });
    expect(result).toMatchObject({ unchangedCount: 0, capacity: { groupsUsed: 2 } });
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(await service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).toMatchObject({
      state: "saved",
      confirmed: 1,
    });

    expect(client.miningGroupServiceCopyMiningGroupPools).toHaveBeenCalledExactlyOnceWith(
      { groupId: "local" },
      {
        expectedRevision: "1",
        sourceGroupId: "default",
        sourceExpectedRevision: "1",
        sourceExpectedDefaultPoolRevision: "1",
        idempotencyKey: result.groups[0].assignment.id + ":copy-pools",
        pools: [
          { source: { ...publicPool, shareWeight: undefined }, username: "account.region.rig-01" },
          { source: { ...backup, shareWeight: undefined }, username: "backup.rig-01" },
        ],
      },
      expect.anything(),
    );
    expect(client.miningGroupServiceReplaceMiningGroupPools).not.toHaveBeenCalled();
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(client.miningGroupServiceUpdateMiningGroupIntent).toHaveBeenCalledWith(
      { id: "local" },
      expect.objectContaining({ expectedRevision: "2", desiredMining: true }),
      expect.anything(),
    );
    expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledWith(
      expect.objectContaining({
        destinationExpectedRevision: "3",
        sourceExpectedDefaultPoolRevision: "1",
        macAddresses: [A],
      }),
      expect.anything(),
    );
    expect([...state.members.values()]).toEqual(["local", "default", "default"]);
    expect(state.pools.get("default")).toEqual([publicPool, backup]);
  });

  test.each([false, undefined])("requires verified Default write support (%s)", async (supported) => {
    const { service, state, client } = fixture();
    state.capability.defaultPoolWritesSupported = supported;
    state.capability.poolCopySupported = true;

    await expect(service.inspectWorkerNames({ macs: [A] })).rejects.toThrow(
      supported === false ? "native DATUM" : "Update the controller software",
    );
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });

  test("checks a Default password-only rotation at transfer even if the public pools still match", async () => {
    const { service, state, client } = fixture();
    state.capability.poolCopySupported = true;
    const result = await review(service, { macs: [A] }, { [A]: "night" });
    const transfer = client.miningGroupServiceRequestMiningGroupTransfer.getMockImplementation()!;
    client.miningGroupServiceRequestMiningGroupTransfer.mockImplementationOnce((...args) => {
      state.groups.find(({ id }) => id === "default")!.defaultPoolRevision = "2";
      return transfer(...args);
    });

    await expect(service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).rejects.toThrow(
      "Default pools changed",
    );
    expect(state.pools.get("default")).toEqual([publicPool]);
    expect([...state.members.values()]).toEqual(["default", "default", "default"]);
  });

  test("updates differing backup workers and keeps a fixed backup username", async () => {
    const { service, state, addLocal } = fixture();
    addLocal([A, B]);
    state.pools.set("saved", [
      publicPool,
      { ...publicPool, id: 2, name: "Backup", priority: 1, username: "backup.old", url: "tcp://backup.example:3333" },
      {
        ...publicPool,
        id: 3,
        name: "Fixed backup",
        priority: 2,
        username: "fixed-account",
        url: "tcp://fixed.example:3333",
      },
    ]);

    const result = await review(service, { macs: [A, B] }, { [A]: "worker", [B]: "worker" });
    expect(result.unchangedCount).toBe(0);
    expect(result.groups[0].assignment.pools.map(({ username }) => username)).toEqual([
      "account.region.worker",
      "backup.worker",
      "fixed-account",
    ]);
    expect(await service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).toMatchObject({
      state: "saved",
    });
  });

  test("uses the new source revision for each split and groups equal names together", async () => {
    const { service, client, state, addLocal } = fixture();
    state.capability.poolCopySupported = true;
    addLocal([A, B, C]);
    const result = await review(service, { macs: [A, B, C] }, { [A]: "pair", [B]: "pair", [C]: "single" });

    expect(await service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).toMatchObject({
      state: "saved",
      confirmed: 3,
    });
    expect(
      client.miningGroupServiceCopyMiningGroupPools.mock.calls.map(([, request]) => request.sourceExpectedRevision),
    ).toEqual(["2", "3"]);
    expect([...state.members.values()]).toEqual(["local", "local", "local-2"]);
    expect(state.pools.get("saved")).toEqual([publicPool]);
  });

  test.each([
    { phase: "STOPPING", expectedState: "pending" },
    { phase: "FAILED", expectedState: "failed" },
  ])(
    "does not start the next worker-name split when the first transfer is $phase",
    async ({ phase, expectedState }) => {
      const { service, client, state } = fixture();
      state.capability.poolCopySupported = true;
      const result = await review(service, { macs: [A, B] }, { [A]: "first", [B]: "second" });
      expect(result.groups).toHaveLength(2);
      client.miningGroupServiceRequestMiningGroupTransfer.mockImplementationOnce(
        async ({ sourceGroupId, destinationGroupId, macAddresses }) => {
          for (const group of state.groups) {
            if (group.id === sourceGroupId || group.id === destinationGroupId)
              group.revision = String(Number(group.revision) + 1);
          }
          const operation = { id: "first-transfer", phase, macAddresses, destinationGroupId };
          state.operations.set(operation.id, operation);
          return { data: { operation } };
        },
      );
      const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
      const saved = vi.fn();

      vi.useFakeTimers();
      try {
        const applied = service.applyWorkerNames(result, { track, saved });
        const expected = expect(applied).resolves.toMatchObject({
          state: expectedState,
          message:
            phase === "FAILED"
              ? expect.stringContaining("The configuration “first” is saved without assigned hashboards.")
              : "Waiting for the controller to finish moving the selected hashboards.",
        });
        await vi.runAllTimersAsync();
        await expected;
      } finally {
        vi.useRealTimers();
      }
      expect(client.miningGroupServiceCreateMiningGroup).toHaveBeenCalledOnce();
      expect(client.miningGroupServiceCopyMiningGroupPools).toHaveBeenCalledOnce();
      expect(client.miningGroupServiceRequestMiningGroupTransfer).toHaveBeenCalledOnce();
      expect(saved).not.toHaveBeenCalled();
      expect(track.mock.lastCall![0]).toMatchObject({ macs: [A], unsubmittedWorkerNameBoards: 1 });
    },
  );

  test("rejects the whole review when the new names require too many groups", async () => {
    const { service, client, state } = fixture();
    state.capability.poolCopySupported = true;
    state.capability.maxGroups = 2;
    await expect(review(service, { macs: [A, B] }, { [A]: "first", [B]: "second" })).rejects.toThrow(
      "All mining groups are in use",
    );
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });

  test("keeps memberships unchanged when Frigate has not confirmed the copy", async () => {
    const { service, state, client } = fixture();
    state.capability.poolCopySupported = true;
    const copy = client.miningGroupServiceCopyMiningGroupPools.getMockImplementation()!;
    client.miningGroupServiceCopyMiningGroupPools.mockImplementationOnce(async (path, body) => {
      await copy(path, body);
      throw new Error("copy response lost");
    });
    const result = await review(service, { macs: [A] }, { [A]: "night" });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();

    await expect(service.applyWorkerNames(result, { track, saved: vi.fn() })).rejects.toThrow("copy response lost");
    expect(await service.status(track.mock.lastCall![0])).toMatchObject({ state: "unknown" });
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    expect([...state.members.values()]).toEqual(["default", "default", "default"]);
  });

  test("keeps the copy unconfirmed and submits no transfer after an incomplete copy receipt", async () => {
    const { service, state, client } = fixture();
    state.capability.poolCopySupported = true;
    const result = await review(service, { macs: [A] }, { [A]: "night" });
    const copy = client.miningGroupServiceCopyMiningGroupPools.getMockImplementation()!;
    client.miningGroupServiceCopyMiningGroupPools.mockImplementationOnce(async (...args) => {
      const { data } = await copy(...args);
      return { data: { ...data, pools: [] } };
    });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    const saved = vi.fn();

    await expect(service.applyWorkerNames(result, { track, saved })).rejects.toThrow("incomplete pool update receipt");
    expect(track.mock.lastCall![0].configurationConfirmed).toBe(false);
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    expect(saved).not.toHaveBeenCalled();
  });

  test("names the unused pool configuration in recovery if the controller refuses the transfer", async () => {
    const { service, state, client } = fixture();
    state.capability.poolCopySupported = true;
    const result = await review(service, { macs: [A] }, { [A]: "night" });
    client.miningGroupServiceRequestMiningGroupTransfer.mockRejectedValueOnce(
      Object.assign(new Error("Source pools changed"), { error: { code: Code.Aborted } }),
    );
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();

    await expect(service.applyWorkerNames(result, { track, saved: vi.fn() })).rejects.toThrow("Source pools changed");
    const attempt = track.mock.lastCall![0];
    expect(await service.status(attempt)).toMatchObject({
      state: "unknown",
      message: "The configuration “night” is saved without assigned hashboards. You can delete it in Settings → Pools.",
    });
    expect(client.miningGroupServiceDeleteMiningGroup).not.toHaveBeenCalled();
    expect(state.pools.get("local")?.[0].username).toBe("account.region.night");

    state.operations.set("another-transfer", {
      id: "another-transfer",
      phase: "STOPPING",
      macAddresses: [B],
      destinationGroupId: "local",
    });
    expect((await service.status(attempt)).message).not.toContain("You can delete it");
    state.operations.clear();
    state.members.set(B, "local");
    expect((await service.status(attempt)).message).not.toContain("You can delete it");
  });

  test("removes only the new empty group after a confirmed copy rejection", async () => {
    const { service, state, client } = fixture();
    state.capability.poolCopySupported = true;
    client.miningGroupServiceCopyMiningGroupPools.mockRejectedValueOnce(
      Object.assign(new Error("Source pools changed"), { error: { code: Code.Aborted } }),
    );
    const result = await review(service, { macs: [A] }, { [A]: "night" });
    await expect(service.applyWorkerNames(result, { track: vi.fn(), saved: vi.fn() })).rejects.toBeInstanceOf(
      WorkerNameUpdateRolledBack,
    );
    expect(client.miningGroupServiceDeleteMiningGroup).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ id: "local", expectedRevision: "1" }),
      expect.anything(),
    );
    expect(state.groups.map(({ id }) => id)).toEqual(["default"]);
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
  });

  test("refuses a generated name with periods before it can become an ambiguous stored suffix", async () => {
    const { service, client, addLocal } = fixture();
    addLocal([A, B]);
    await expect(review(service, { macs: [A] }, { [A]: "tank.board" })).rejects.toThrow("without spaces or periods");
    expect(client.miningGroupServiceCreateMiningGroup).not.toHaveBeenCalled();
  });

  test("rejects an old name preview after boards exchange slots in the same module", async () => {
    const { service, state } = fixture();
    const target = { macs: [A, B, C], modules: [[A, B, C]] };
    const sources = await service.inspectWorkerNames(target);
    state.boardSlots = [B, A, C];
    await expect(
      service.reviewWorkerNames(target, { namesByMac: { [A]: "1-1", [B]: "1-2", [C]: "1-3" }, sources }),
    ).rejects.toThrow("reviewed board locations changed");
  });

  test("leaves later groups untouched when the first write is unconfirmed", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const send = client.miningGroupServiceReplaceMiningGroupPools.getMockImplementation()!;
    client.miningGroupServiceReplaceMiningGroupPools.mockImplementationOnce(async (path, body) => {
      await send(path, body);
      throw new Error("response lost");
    });
    const result = await review(service, { macs: [A, B, C] }, { [A]: "night", [B]: "night", [C]: "other" });
    const track = vi.fn<(attempt: PoolAssignmentAttempt) => void>();
    const saved = vi.fn();
    await expect(service.applyWorkerNames(result, { track, saved })).rejects.toThrow("response lost");
    const attempt = track.mock.lastCall![0];
    expect(attempt).toMatchObject({ macs: [A, B], configurationConfirmed: false, unsubmittedWorkerNameBoards: 1 });
    expect(await service.status(attempt)).toMatchObject({ state: "unknown" });
    expect(saved).not.toHaveBeenCalled();
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(state.members.get(C)).toBe("default");
  });

  test("stops when the later group changes during a batch", async () => {
    const { service, state, client, addLocal } = fixture();
    addLocal([A, B]);
    const result = await review(service, { macs: [A, B, C] }, { [A]: "night", [B]: "night", [C]: "other" });
    const saved = vi.fn(() => {
      state.groups.find(({ id }) => id === "default")!.revision = "5";
    });
    await expect(service.applyWorkerNames(result, { track: vi.fn(), saved })).rejects.toThrow(/changed/i);
    expect(saved).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ macs: [A, B] }));
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });
});

describe("CT1 pool configuration library", () => {
  test("returns recent audit updates for the selected group and leaves other group records out", async () => {
    const { service, client } = fixture();
    client.systemServiceListAuditEvents.mockImplementation(async ({ action }) => {
      if (action === "mining-group.pools.replace")
        return {
          data: {
            events: [
              { id: "10", action, target: "saved", time: "2026-11-10T10:00:00Z", result: "ok" },
              { id: "11", action, target: "other", time: "2026-11-10T10:00:00Z", result: "ok" },
            ],
          },
        };
      if (action === "mining-group.transfer")
        return {
          data: {
            events: [
              {
                id: "12",
                action,
                target: "other",
                time: "2026-11-10T10:00:00.001Z",
                result: "error",
                detailsJson: JSON.stringify({ source_group_id: "saved", mac_addresses: [A, A.toUpperCase(), "bad"] }),
              },
              { id: "13", action, target: "other", detailsJson: "not JSON" },
            ],
          },
        };
      return { data: { events: [] } };
    });

    const result = await service.listConfigurationUpdates?.("saved");
    expect(result?.updates).toEqual([
      { id: "12", action: "Transfer out of this group", result: "error", macs: [A], time: "2026-11-10T10:00:00.001Z" },
      { id: "10", action: "Pool settings update", result: "ok", macs: [], time: "2026-11-10T10:00:00Z" },
    ]);
    expect(client.systemServiceListAuditEvents).toHaveBeenCalledWith(
      expect.objectContaining({
        action: "mining-group.pools.replace",
        target: "saved",
        "range.start": expect.any(String),
        "range.end": expect.any(String),
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

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
  test("creates an unused configuration during an inventory outage and allows later running-board assignment", async () => {
    const { service, state, client } = fixture();
    const getMap = client.hashboardServiceGetContainerMap.getMockImplementation()!;
    client.hashboardServiceGetContainerMap.mockRejectedValue(new Error("Board inventory unavailable"));
    const plan = await service.review({ macs: [] }, { name: "Library pools", createOnly: true, pools: [pool] });
    expect(await service.apply(plan, vi.fn())).toMatchObject({ state: "saved" });
    expect(state.pools.get("local")).toEqual([expect.objectContaining({ url: pool.url, username: pool.username })]);
    expect([...state.members.values()]).toEqual(["default", "default", "default"]);
    expect(client.hashboardServiceGetContainerMap).not.toHaveBeenCalled();
    expect(client.miningServiceStopMining).not.toHaveBeenCalled();
    expect(client.miningGroupServiceRequestMiningGroupTransfer).not.toHaveBeenCalled();
    await expect(service.inspect({ macs: [A] })).rejects.toThrow("Board inventory unavailable");
    client.hashboardServiceGetContainerMap.mockImplementation(getMap);
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
      expect(plan.boards).toMatchObject([{ mac: A, location: "Tank 1 · Module 1 · Board 1" }, { mac: B }, { mac: C }]);
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
      expect(plan.boards).toMatchObject([
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
