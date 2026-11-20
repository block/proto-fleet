import { describe, expect, test, vi } from "vitest";
import { Code } from "@connectrpc/connect";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";

const MAC = "aa:bb:cc:00:00:01";
const actor = { headers: { "X-Frigate-Actor": "proto-os" }, signal: expect.any(AbortSignal) };
const capability = (overrides = {}) => ({
  name: "mining-groups-v1",
  version: 1,
  assignmentGranularity: "PHYSICAL_HASHBOARD",
  stateRevision: "3",
  enabled: true,
  ...overrides,
});
const fixture = (overrides: Partial<MdkV2Client> = {}) => {
  const client = {
    miningGroupServiceGetMiningGroupCapability: vi.fn().mockResolvedValue({ data: { capability: capability() } }),
    miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({
      data: {
        groups: [
          { id: "a", name: "Client A", revision: "4" },
          { id: "b", name: "Client B", revision: "2" },
        ],
        pendingOperations: [
          { id: "transfer-1", kind: "TRANSFER", phase: "STOPPING", macAddresses: [MAC], error: "Waiting for board" },
        ],
        inventoryIssues: [{ groupId: "a", macAddress: MAC, reason: "mapping unavailable" }],
      },
    }),
    miningGroupServiceListMiningGroupMemberships: vi.fn().mockResolvedValue({
      data: {
        memberships: [
          { groupId: "a", macAddress: MAC.toUpperCase() },
          { groupId: "b", macAddress: "aa:bb:cc:00:00:02" },
          { groupId: "missing", macAddress: "aa:bb:cc:00:00:03" },
        ],
      },
    }),
    hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
      data: {
        containerMap: {
          tanks: [
            {
              id: 1,
              displayLabel: "Door tank",
              modules: [
                {
                  id: 2,
                  slots: [
                    { occupied: true, hashboard: { macAddress: MAC, location: { hashboardPosition: 3 } } },
                    {
                      occupied: true,
                      hashboard: { macAddress: "aa:bb:cc:00:00:04", isolated: true },
                      configuredLocation: { hashboardPosition: 2 },
                    },
                  ],
                },
              ],
            },
          ],
        },
      },
    }),
    miningGroupServiceListMiningGroupPools: vi.fn().mockResolvedValue({
      data: {
        pools: [
          { id: 2, name: "Backup", priority: 1, username: "account.region.worker", url: "stratum+tcp://backup:3333" },
          {
            id: 1,
            name: "Primary",
            priority: 0,
            username: "opaque.username.with.dots",
            url: "stratum+tcp://primary:3333",
            enabled: true,
            shareWeight: 2,
          },
        ],
      },
    }),
    miningGroupServiceGetMiningGroup: vi.fn().mockResolvedValue({ data: { group: { id: "a", revision: "4" } } }),
    poolServiceCreatePool: vi.fn(),
    poolServiceUpdatePool: vi.fn(),
    poolServiceDeletePool: vi.fn(),
    poolServiceListPools: vi.fn().mockResolvedValue({
      data: {
        pools: [
          { id: "1", priority: 0 },
          { id: "2", priority: 1 },
        ],
      },
    }),
    ...overrides,
  };
  return { client, adapter: new MdkV2Adapter(client as unknown as MdkV2Client) };
};

describe("CT1 pool configuration inspection", () => {
  test.each([{ data: {} }, { data: { capability: capability({ version: 2 }) } }])(
    "missing or unsupported capabilities allow read-only pool inspection: %j",
    async (response) => {
      const { adapter, client } = fixture({
        miningGroupServiceGetMiningGroupCapability: vi.fn().mockResolvedValue(response),
      });
      expect(await adapter.getContainerPoolConfiguration()).toMatchObject({ mode: "unsupported" });
      expect(client.miningGroupServiceListMiningGroups).not.toHaveBeenCalled();
    },
  );

  test.each([Code.Unimplemented, Code.Unavailable, Code.Unauthenticated])(
    "only an unimplemented capability permits read-only fallback for error %s",
    async (code) => {
      const error = { error: { code } };
      const { adapter } = fixture({
        miningGroupServiceGetMiningGroupCapability: vi.fn().mockRejectedValue(error),
      });
      if (code === Code.Unimplemented)
        expect(await adapter.getContainerPoolConfiguration()).toMatchObject({ mode: "unsupported" });
      else await expect(adapter.getContainerPoolConfiguration()).rejects.toBe(error);
    },
  );

  test("a pool list that ignores cancellation still times out", async () => {
    vi.useFakeTimers();
    try {
      const list = vi.fn().mockImplementation(() => new Promise(() => {}));
      const { adapter } = fixture({ poolServiceListPools: list });
      const pending = adapter.getPoolSettings();
      const assertion = expect(pending).rejects.toThrow("did not respond within 10 seconds");
      await vi.advanceTimersByTimeAsync(10_000);
      await assertion;
      expect(list.mock.calls[0][0].signal.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  test("canceling a stalled pool list stops waiting without a retry", async () => {
    const list = vi.fn().mockImplementation(() => new Promise(() => {}));
    const { adapter } = fixture({ poolServiceListPools: list });
    const controller = new AbortController();
    const pending = adapter.getPoolSettings({ signal: controller.signal });
    controller.abort(new Error("Left pools"));
    await expect(pending).rejects.toBe(controller.signal.reason);
    expect(list).toHaveBeenCalledTimes(1);
  });

  test("transfer progress and group ordering do not invalidate stable membership", async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce({
        data: {
          groups: [
            { id: "a", revision: "4" },
            { id: "b", revision: "2" },
          ],
          pendingOperations: [
            { id: "op", phase: "QUIESCING", stopBoardsConfirmed: 0, updatedAt: "2026-11-17T10:00:00Z" },
          ],
        },
      })
      .mockResolvedValueOnce({
        data: {
          groups: [
            { id: "b", revision: "2" },
            { id: "a", revision: "4" },
          ],
          pendingOperations: [
            {
              id: "op",
              phase: "QUIESCING",
              stopBoardsConfirmed: 1,
              updatedAt: "2026-11-17T10:00:01Z",
              error: "Waiting for another board",
            },
          ],
        },
      });
    const { adapter } = fixture({ miningGroupServiceListMiningGroups: list });
    const result = await adapter.getContainerPoolConfiguration();
    expect(result.groups[0].boards).toHaveLength(1);
    expect(result.pendingOperations[0]).toMatchObject({ phase: "QUIESCING", error: "Waiting for another board" });
  });

  test("disabled mode ignores stale groups and needs no membership or topology reads", async () => {
    const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
    const list = vi.fn().mockResolvedValue({
      data: { groups: [{ id: "stale" }], pendingOperations: [{ id: "op", phase: "FAILED", error: "Old failure" }] },
    });
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroupCapability: read,
      miningGroupServiceListMiningGroups: list,
    });
    const result = await adapter.getContainerPoolConfiguration();
    expect(result).toMatchObject({
      mode: "container",
      groups: [],
      pendingOperations: [{ id: "op", phase: "FAILED", error: "Old failure" }],
    });
    expect(read).toHaveBeenCalledTimes(2);
    expect(list).toHaveBeenCalledTimes(1);
    expect(client.miningGroupServiceListMiningGroupMemberships).not.toHaveBeenCalled();
    expect(client.hashboardServiceGetContainerMap).not.toHaveBeenCalled();
  });

  test("disabled-mode reads still detect activation during the read", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({ data: { capability: capability({ enabled: false }) } })
      .mockResolvedValue({ data: { capability: capability({ stateRevision: "4" }) } });
    const { adapter } = fixture({ miningGroupServiceGetMiningGroupCapability: read });
    await expect(adapter.getContainerPoolConfiguration()).rejects.toThrow("mode changed");
  });

  test.each([0, "0"])("unreported observed position %j falls back to the configured position", async (position) => {
    const { adapter } = fixture({
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
        data: {
          containerMap: {
            tanks: [
              {
                id: 1,
                modules: [
                  {
                    id: 2,
                    slots: [
                      {
                        occupied: true,
                        hashboard: { macAddress: MAC, location: { hashboardPosition: position } },
                        configuredLocation: { hashboardPosition: 3 },
                      },
                    ],
                  },
                ],
              },
            ],
          },
        },
      }),
    });
    expect((await adapter.getContainerPoolConfiguration()).groups[0].boards[0].location).toBe(
      "Tank 1 · Module 2 · Board 3",
    );
  });
  test("joins physical board membership without dropping unlocated or orphaned boards", async () => {
    const { adapter, client } = fixture();
    const result = await adapter.getContainerPoolConfiguration();
    expect(result.mode).toBe("groups");
    expect(result.groups).toEqual([
      { id: "a", name: "Client A", revision: "4", boards: [{ mac: MAC, location: "Door tank · Module 2 · Board 3" }] },
      { id: "b", name: "Client B", revision: "2", boards: [{ mac: "aa:bb:cc:00:00:02" }] },
      { id: "missing", name: "Unknown group (missing)", revision: "", boards: [{ mac: "aa:bb:cc:00:00:03" }] },
    ]);
    expect(result.unassignedBoards).toEqual([
      { mac: "aa:bb:cc:00:00:04", location: "Door tank · Module 2 · Board 2", isolated: true },
    ]);
    expect(result.assignmentInventoryError).toBeUndefined();
    expect(result.pendingOperations).toEqual([
      { id: "transfer-1", kind: "TRANSFER", phase: "STOPPING", error: "Waiting for board", boards: [MAC] },
    ]);
    expect(result.inventoryIssues).toEqual([{ groupId: "a", mac: MAC, reason: "mapping unavailable" }]);
    expect(client.miningGroupServiceListMiningGroupMemberships).toHaveBeenCalledWith({}, actor);
    // Overview does not read credentials for every group in a large container.
    expect(client.miningGroupServiceListMiningGroupPools).not.toHaveBeenCalled();
  });

  test("missing topology preserves membership without inventing physical positions", async () => {
    const { adapter } = fixture({ hashboardServiceGetContainerMap: vi.fn().mockRejectedValue(new Error("offline")) });
    const result = await adapter.getContainerPoolConfiguration();
    expect(result.locationsAvailable).toBe(false);
    expect(result.groups[0].boards).toEqual([{ mac: MAC.toUpperCase() }]);
    expect(result.unassignedBoards).toEqual([]);
  });

  test("shows unassigned boards when inventory takes eight seconds to respond", async () => {
    vi.useFakeTimers();
    try {
      const { adapter, client } = fixture();
      const getMap = vi.mocked(client.hashboardServiceGetContainerMap);
      const map = await getMap();
      getMap.mockImplementationOnce(async () => {
        await new Promise((resolve) => setTimeout(resolve, 8_000));
        return map;
      });
      const result = adapter.getContainerPoolConfiguration();
      await vi.advanceTimersByTimeAsync(8_000);
      expect(await result).toMatchObject({
        locationsAvailable: true,
        unassignedBoards: [{ mac: "aa:bb:cc:00:00:04", location: "Door tank · Module 2 · Board 2" }],
      });
    } finally {
      vi.useRealTimers();
    }
  });

  test("an inventory timeout preserves group membership", async () => {
    vi.useFakeTimers();
    try {
      const getMap = vi.fn().mockImplementation(() => new Promise(() => {}));
      const { adapter } = fixture({ hashboardServiceGetContainerMap: getMap });
      const assertion = expect(adapter.getContainerPoolConfiguration()).resolves.toMatchObject({
        mode: "groups",
        groups: expect.arrayContaining([
          { id: "a", name: "Client A", revision: "4", boards: [{ mac: MAC.toUpperCase() }] },
        ]),
        locationsAvailable: false,
        unassignedBoards: [],
      });
      await vi.advanceTimersByTimeAsync(10_000);
      await assertion;
      expect(getMap.mock.calls[0][0].signal.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  test("a board reported at multiple positions has no claimed location", async () => {
    const { adapter } = fixture({
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
        data: {
          containerMap: {
            tanks: [
              {
                id: 1,
                modules: [1, 2].map((id) => ({
                  id,
                  slots: [{ occupied: true, hashboard: { macAddress: MAC, location: { hashboardPosition: 1 } } }],
                })),
              },
            ],
          },
        },
      }),
    });
    const result = await adapter.getContainerPoolConfiguration();
    expect(result.groups[0].boards[0].location).toBeUndefined();
    expect(result.assignmentInventoryError).toMatch(/incomplete or duplicated/);
  });

  test.each([
    { identity: "missing", slot: { occupied: true } },
    { identity: "invalid", slot: { occupied: true, hashboard: { macAddress: "unknown" } } },
  ])("blocks assignment when an occupied slot has a $identity MAC", async ({ slot }) => {
    const mac = "aa:bb:cc:00:00:04";
    const { adapter } = fixture({
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({
        data: {
          containerMap: {
            tanks: [{ modules: [{ slots: [slot, { occupied: true, hashboard: { macAddress: mac } }] }] }],
          },
        },
      }),
    });

    const result = await adapter.getContainerPoolConfiguration();
    expect(result.locationsAvailable).toBe(true);
    expect(result.assignmentInventoryError).toMatch(/incomplete or duplicated/);
    expect(result.unassignedBoards).toEqual([{ mac }]);
  });

  test("does not publish memberships read across a concurrent transfer", async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce({ data: { groups: [{ id: "a", revision: "4" }] } })
      .mockResolvedValue({ data: { groups: [{ id: "a", revision: "5" }] } });
    const { adapter } = fixture({ miningGroupServiceListMiningGroups: list });
    await expect(adapter.getContainerPoolConfiguration()).rejects.toThrow("assignments changed");
  });

  test("does not publish a view read across a mode transition", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({ data: { capability: capability() } })
      .mockResolvedValue({ data: { capability: capability({ enabled: false, stateRevision: "4" }) } });
    const { adapter } = fixture({ miningGroupServiceGetMiningGroupCapability: read });
    await expect(adapter.getContainerPoolConfiguration()).rejects.toThrow("mode changed");
  });

  test("reads complete usernames, disabled pools and weights without splitting worker suffixes", async () => {
    const { adapter, client } = fixture();
    expect(await adapter.getMiningGroupPoolSettings("a", "4")).toEqual([
      {
        id: "1",
        name: "Primary",
        url: "stratum+tcp://primary:3333",
        username: "opaque.username.with.dots",
        priority: 0,
        enabled: true,
        shareWeight: 2,
      },
      {
        id: "2",
        name: "Backup",
        url: "stratum+tcp://backup:3333",
        username: "account.region.worker",
        priority: 1,
        enabled: false,
      },
    ]);
    expect(client.miningGroupServiceListMiningGroupPools).toHaveBeenCalledWith({ groupId: "a" }, actor);
  });

  test("changed or removed groups do not display old credentials against new membership", async () => {
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroup: vi.fn().mockResolvedValue({ data: { group: { id: "a", revision: "5" } } }),
    });
    await expect(adapter.getMiningGroupPoolSettings("a", "4")).rejects.toThrow("group changed");
  });

  test("reads Default configuration details from the legacy pool service", async () => {
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroup: vi
        .fn()
        .mockResolvedValue({ data: { group: { id: "default", revision: "4" } } }),
    });
    expect(await adapter.getMiningGroupPoolSettings("default", "4")).toMatchObject([{ id: "1" }, { id: "2" }]);
    expect(client.poolServiceListPools).toHaveBeenCalledOnce();
    expect(client.miningGroupServiceListMiningGroupPools).not.toHaveBeenCalled();
  });

  test("aborting a read cancels the controller requests", async () => {
    const read = vi.fn<MdkV2Client["miningGroupServiceGetMiningGroupCapability"]>(async (options) => {
      options?.signal?.throwIfAborted();
      return { data: {} };
    });
    const { adapter, client } = fixture({ miningGroupServiceGetMiningGroupCapability: read });
    const controller = new AbortController();
    const reason = new Error("Pool inspection canceled");
    controller.abort(reason);
    // Real transports reject an already-aborted signal before dispatch.
    await expect(adapter.getContainerPoolConfiguration({ signal: controller.signal })).rejects.toBe(reason);
    expect(read).not.toHaveBeenCalled();
    expect(client.miningGroupServiceListMiningGroups).not.toHaveBeenCalled();
  });
});

describe("legacy container pool write admission", () => {
  const pool = { name: "Pool", url: "stratum+tcp://pool:3333", username: "account.worker", password: "", priority: 0 };
  const writes = [
    (adapter: MdkV2Adapter) => adapter.savePoolSettings(pool),
    (adapter: MdkV2Adapter) => adapter.savePoolSettings(pool, "1"),
    (adapter: MdkV2Adapter) => adapter.deletePoolSettings("1"),
    (adapter: MdkV2Adapter) => adapter.reorderPoolSettings(["2", "1"]),
  ];
  test.each(writes)("preserves authentication errors before a write", async (write) => {
    const auth = Object.assign(new Error("expired"), { status: 401, error: { code: Code.Unauthenticated } });
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroupCapability: vi.fn().mockRejectedValue(auth),
    });
    await expect(write(adapter)).rejects.toBe(auth);
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
  });

  test.each([Code.InvalidArgument, Code.Unimplemented, Code.FailedPrecondition, Code.Unavailable])(
    "classifies controller pool error %s without mistaking saved reload failures for refusals",
    async (code) => {
      for (const write of writes) {
        const error = {
          status: 400,
          error: {
            code,
            message: code === Code.FailedPrecondition ? "pool saved but live reload failed" : "request failed",
          },
        };
        const { adapter } = fixture({
          miningGroupServiceGetMiningGroupCapability: vi
            .fn()
            .mockResolvedValue({ data: { capability: capability({ enabled: false }) } }),
          miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
          poolServiceCreatePool: vi.fn().mockRejectedValue(error),
          poolServiceUpdatePool: vi.fn().mockRejectedValue(error),
          poolServiceDeletePool: vi.fn().mockRejectedValue(error),
        });
        await expect(write(adapter)).rejects.toMatchObject({
          writeOutcome: [Code.InvalidArgument, Code.Unimplemented].includes(code) ? "refused" : "unknown",
          cause: error,
        });
      }
    },
  );

  test.each(writes)("preserves authentication errors returned by the pool write", async (write) => {
    const auth = Object.assign(new Error("expired"), { status: 401, error: { code: Code.Unauthenticated } });
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroupCapability: vi
        .fn()
        .mockResolvedValue({ data: { capability: capability({ enabled: false }) } }),
      miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
      poolServiceCreatePool: vi.fn().mockRejectedValue(auth),
      poolServiceUpdatePool: vi.fn().mockRejectedValue(auth),
      poolServiceDeletePool: vi.fn().mockRejectedValue(auth),
    });
    await expect(write(adapter)).rejects.toBe(auth);
  });

  test("three-pool reorders use one full preflight and one capability read per step", async () => {
    const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
    const list = vi.fn().mockResolvedValue({ data: {} });
    const update = vi.fn().mockResolvedValue({ data: {} });
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroupCapability: read,
      miningGroupServiceListMiningGroups: list,
      poolServiceListPools: vi
        .fn()
        .mockResolvedValue({ data: { pools: [1, 2, 3].map((id, priority) => ({ id: String(id), priority })) } }),
      poolServiceUpdatePool: update,
    });
    await adapter.reorderPoolSettings(["2", "3", "1"]);
    expect(update).toHaveBeenCalledTimes(6);
    expect(read).toHaveBeenCalledTimes(8);
    expect(list).toHaveBeenCalledTimes(1);
  });

  test.each([Code.Unavailable, Code.DeadlineExceeded])(
    "a transient capability failure between reorder passes is retried once: %s",
    async (code) => {
      const response = { data: { capability: capability({ enabled: false }) } };
      const read = vi.fn().mockResolvedValue(response);
      const update = vi.fn().mockImplementation(async () => {
        if (update.mock.calls.length === 2) read.mockRejectedValueOnce({ error: { code } });
      });
      const { adapter } = fixture({
        miningGroupServiceGetMiningGroupCapability: read,
        miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
        poolServiceUpdatePool: update,
      });
      await adapter.reorderPoolSettings(["2", "1"]);
      expect(read).toHaveBeenCalledTimes(7);
      expect(update.mock.calls.map(([, body]) => body.pool.priority)).toEqual([2, 3, 0, 1]);
    },
  );

  test("two capability failures after parking stop the reorder without replaying writes", async () => {
    const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
    const update = vi.fn().mockImplementation(async () => {
      if (update.mock.calls.length === 2) read.mockRejectedValue({ status: 503 });
    });
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroupCapability: read,
      miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
      poolServiceUpdatePool: update,
    });
    await expect(adapter.reorderPoolSettings(["2", "1"])).rejects.toMatchObject({ writeOutcome: "unknown" });
    expect(read).toHaveBeenCalledTimes(6);
    expect(update).toHaveBeenCalledTimes(2);
  });

  test("a local capability timeout between passes is retried with a fresh deadline", async () => {
    vi.useFakeTimers();
    try {
      const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
      const update = vi.fn().mockImplementation(async () => {
        if (update.mock.calls.length === 2) read.mockImplementationOnce(() => new Promise(() => {}));
      });
      const { adapter } = fixture({
        miningGroupServiceGetMiningGroupCapability: read,
        miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
        poolServiceUpdatePool: update,
      });
      const pending = adapter.reorderPoolSettings(["2", "1"]);
      await vi.advanceTimersByTimeAsync(10_000);
      await pending;
      expect(read).toHaveBeenCalledTimes(7);
      expect(update).toHaveBeenCalledTimes(4);
    } finally {
      vi.useRealTimers();
    }
  });

  test.each(["revision", "authentication"])(
    "a %s change after a saved priority stops without replaying",
    async (change) => {
      const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
      const auth = Object.assign(new Error("expired"), { status: 401, error: { code: Code.Unauthenticated } });
      const update = vi.fn().mockImplementation(async () => {
        if (change === "authentication") read.mockRejectedValue(auth);
        else read.mockResolvedValue({ data: { capability: capability({ enabled: false, stateRevision: "4" }) } });
      });
      const { adapter } = fixture({
        miningGroupServiceGetMiningGroupCapability: read,
        miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
        poolServiceUpdatePool: update,
      });
      const failure = await adapter.reorderPoolSettings(["2", "1"]).catch((error) => error);
      expect(failure).toMatchObject({
        writeOutcome: "unknown",
        message: expect.stringContaining("Some pool priorities were saved"),
      });
      if (change === "authentication") expect(failure).toMatchObject({ status: 401, error: auth.error, cause: auth });
      expect(read).toHaveBeenCalledTimes(4);
      expect(update).toHaveBeenCalledTimes(1);
    },
  );

  test("terminal receipts do not block legacy writes, but nonterminal errors do", async () => {
    const list = vi.fn().mockResolvedValue({
      data: { pendingOperations: [{ phase: "FAILED", error: "Old failure" }, { phase: "COMPLETE" }] },
    });
    const create = vi.fn().mockResolvedValue({ data: {} });
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroupCapability: vi
        .fn()
        .mockResolvedValue({ data: { capability: capability({ enabled: false }) } }),
      miningGroupServiceListMiningGroups: list,
      poolServiceCreatePool: create,
    });
    await adapter.savePoolSettings(pool);
    expect(create).toHaveBeenCalledTimes(1);
    list.mockResolvedValue({ data: { pendingOperations: [{ phase: "QUIESCING", error: "Waiting for a board" }] } });
    await expect(adapter.savePoolSettings(pool)).rejects.toThrow("pending");
    expect(create).toHaveBeenCalledTimes(1);
  });
  test.each(writes)("blocks a legacy write while mining groups are enabled", async (write) => {
    const { adapter, client } = fixture();
    await expect(write(adapter)).rejects.toMatchObject({ writeOutcome: "refused" });
    expect(client.poolServiceCreatePool).not.toHaveBeenCalled();
    expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
    expect(client.poolServiceDeletePool).not.toHaveBeenCalled();
  });

  test.each([
    { capability: undefined },
    { capability: capability({ name: "future-mode", enabled: false }) },
    { capability: capability({ enabled: false, suspended: true }) },
  ])("blocks unknown or suspended mode: %j", async (data) => {
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroupCapability: vi.fn().mockResolvedValue({ data }),
    });
    await expect(adapter.savePoolSettings(pool)).rejects.toMatchObject({ writeOutcome: "refused" });
    expect(client.poolServiceCreatePool).not.toHaveBeenCalled();
  });

  test("queued group work prevents legacy writes even when enabled is false", async () => {
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroupCapability: vi
        .fn()
        .mockResolvedValue({ data: { capability: capability({ enabled: false }) } }),
    });
    await expect(adapter.deletePoolSettings("1")).rejects.toThrow("pending");
    expect(client.poolServiceDeletePool).not.toHaveBeenCalled();
  });

  test("a mode transition during preflight prevents dispatch", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({ data: { capability: capability({ enabled: false }) } })
      .mockResolvedValue({ data: { capability: capability({ stateRevision: "4" }) } });
    const { adapter, client } = fixture({
      miningGroupServiceGetMiningGroupCapability: read,
      miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
    });
    await expect(adapter.savePoolSettings(pool)).rejects.toThrow("mode changed");
    expect(client.poolServiceCreatePool).not.toHaveBeenCalled();
  });

  test("a mode change during a reorder stops later writes without retrying or rolling back", async () => {
    const read = vi.fn().mockResolvedValue({ data: { capability: capability({ enabled: false }) } });
    const update = vi.fn().mockImplementation(async () => {
      read.mockResolvedValue({ data: { capability: capability() } });
    });
    const { adapter } = fixture({
      miningGroupServiceGetMiningGroupCapability: read,
      miningGroupServiceListMiningGroups: vi.fn().mockResolvedValue({ data: {} }),
      poolServiceUpdatePool: update,
    });
    await expect(adapter.reorderPoolSettings(["2", "1"])).rejects.toMatchObject({
      writeOutcome: "unknown",
      message: expect.stringContaining("Some pool priorities were saved"),
      cause: expect.objectContaining({ writeOutcome: "refused", message: expect.stringContaining("enabled") }),
    });
    expect(update).toHaveBeenCalledTimes(1);
  });
});
