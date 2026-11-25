import { expect, test, vi } from "vitest";
import type { AssignmentPool } from "../poolAssignment";
import type { MiningGroupPoolSettings } from "../poolConfiguration";
import type { MdkV2Client } from "./connectClient";
import { saveDefaultPools } from "./poolEdits";

const fixture = () => {
  let stored: MiningGroupPoolSettings[] = ["Primary", "Backup 1", "Backup 2"].map((name, priority) => ({
    id: String(priority + 1),
    name,
    url: `stratum+tcp://pool-${priority}:3333`,
    username: `account.${priority}.worker`,
    priority,
    enabled: true,
  }));
  const original = structuredClone(stored);
  const passwords = new Map(original.map((pool) => [pool.id, "saved-" + pool.id]));
  let sequence = 3;
  const result = (pool: MiningGroupPoolSettings) => Promise.resolve({ data: { pool: structuredClone(pool) } });
  const validate = () => {
    const enabled = stored.filter((pool) => pool.enabled);
    for (const field of ["url", "priority"] as const)
      if (new Set(enabled.map((pool) => pool[field])).size !== enabled.length) throw new Error("conflicting " + field);
    if (new Set(stored.map((pool) => pool.name)).size !== stored.length) throw new Error("conflicting name");
  };
  const client = {
    poolServiceListPools: vi.fn(async () => ({ data: { pools: structuredClone(stored) } })),
    poolServiceUpdatePool: vi.fn(async ({ poolId }, { pool, updateMask, password }) => {
      const current = stored.find((pool) => pool.id === poolId)!;
      for (const field of updateMask.split(",")) Object.assign(current, { [field]: pool[field] });
      validate();
      if (password) passwords.set(poolId, password);
      return result(current);
    }),
    poolServiceDeletePool: vi.fn(async ({ id }) => {
      stored = stored.filter((pool) => pool.id !== id);
      passwords.delete(id);
      return { data: {} };
    }),
    poolServiceCreatePool: vi.fn(async ({ pool, password }) => {
      const created = { ...pool, id: String(++sequence) };
      stored.push(created);
      passwords.set(created.id, password);
      validate();
      return result(created);
    }),
  };
  const draft = (pools = original): AssignmentPool[] =>
    pools.map((pool) => ({ ...pool, password: "", preservePassword: true }));
  const save = (pools: AssignmentPool[], check = async () => {}, expected = original) =>
    saveDefaultPools(client as unknown as MdkV2Client, expected, pools, {}, check, vi.fn());
  return { client, original, passwords, draft, save, stored: () => stored };
};

test("reorders Default pools and swaps URLs without collisions or password writes", async () => {
  const { client, original, passwords, draft, save } = fixture();
  const pools = draft([original[2], original[0], original[1]]);
  [pools[0].url, pools[1].url] = [pools[1].url, pools[0].url];
  expect(await save(pools)).toMatchObject([
    { id: "3", priority: 0, enabled: true, url: original[0].url },
    { id: "1", priority: 1, enabled: true, url: original[2].url },
    { id: "2", priority: 2, enabled: true },
  ]);
  expect([...passwords]).toEqual([
    ["1", "saved-1"],
    ["2", "saved-2"],
    ["3", "saved-3"],
  ]);
  expect(client.poolServiceUpdatePool.mock.calls.every(([, data]) => data.password === undefined)).toBe(true);
});

test("adds and removes Default pool slots while retaining controller identities", async () => {
  const { client, original, draft, save } = fixture();
  const pools = [
    ...draft([original[0], original[2]]),
    { name: "New backup", url: "stratum+tcp://new:3333", username: "full.account.worker", password: "new-secret" },
  ];
  expect(await save(pools)).toMatchObject([{ id: "1" }, { id: "3" }, { id: "4", username: "full.account.worker" }]);
  expect(client.poolServiceDeletePool).toHaveBeenCalledTimes(1);
  expect(client.poolServiceDeletePool).toHaveBeenCalledWith({ id: "2" }, expect.anything());
  expect(client.poolServiceCreatePool).toHaveBeenCalledTimes(1);
});

test("preserves disabled pools and weights", async () => {
  const { original, stored, draft, save } = fixture();
  Object.assign(original[1], { enabled: false, shareWeight: 2 });
  Object.assign(stored()[1], original[1]);
  const pools = draft();
  pools[1].username = "changed.account.worker";
  pools[1].password = "new-secret";
  pools[1].preservePassword = false;
  expect((await save(pools))[1]).toMatchObject({ enabled: false, shareWeight: 2, username: "changed.account.worker" });
});

test("does not disable a pool for a password-only update", async () => {
  const { client, draft, save } = fixture();
  const pools = draft();
  pools[0].preservePassword = false;
  pools[0].password = "replacement";
  await save(pools);
  expect(client.poolServiceUpdatePool).toHaveBeenCalledWith(
    { poolId: "1" },
    expect.objectContaining({ password: "replacement", updateMask: "id" }),
    expect.anything(),
  );
  expect(client.poolServiceUpdatePool.mock.calls.some(([, request]) => request.updateMask === "enabled,priority")).toBe(
    false,
  );
  expect(client.poolServiceUpdatePool.mock.calls.some(([, request]) => request.updateMask === "enabled")).toBe(false);
});

test("detects a concurrent pool change after a partial save and does not retry writes", async () => {
  const { client, original, stored, draft, save } = fixture();
  const check = vi.fn(async () => {
    if (check.mock.calls.length === 2) stored()[0].username = "changed-in-other-session";
  });
  const pools = draft([original[0], original[2]]);
  await expect(save(pools, check)).rejects.toThrow("Some changes may be saved");
  expect(client.poolServiceDeletePool).not.toHaveBeenCalled();
  expect(client.poolServiceUpdatePool).toHaveBeenCalledTimes(1);
  expect(stored()[0].username).toBe("changed-in-other-session");
});

test("reorders priorities without disabling any pool", async () => {
  const { client, original, draft, save } = fixture();
  await save(draft([original[2], original[0], original[1]]));
  expect(client.poolServiceUpdatePool.mock.calls.every(([, request]) => request.updateMask === "priority")).toBe(true);
});

test("uses the controller receipt for an equivalent normalized URL before writing the next pool", async () => {
  const { client, draft, save } = fixture();
  const write = client.poolServiceUpdatePool.getMockImplementation()!;
  client.poolServiceUpdatePool.mockImplementationOnce((path, request) =>
    write(path, { ...request, pool: { ...request.pool, url: "stratum+tcp://changed:3333" } }),
  );
  const pools = draft();
  pools[0] = { ...pools[0], url: "changed:3333", preservePassword: false, password: "replacement" };
  pools[1] = { ...pools[1], username: "changed-worker", preservePassword: false, password: "replacement" };
  const updated = await save(pools);
  expect(updated[0]).toMatchObject({ url: "stratum+tcp://changed:3333" });
  expect(updated[1]).toMatchObject({ username: "changed-worker" });
  expect(client.poolServiceUpdatePool).toHaveBeenCalledTimes(2);
});

test("rejects invalid weighted endpoints before writing", async () => {
  const { client, draft, save } = fixture();
  const pools = draft();
  pools[0].shareWeight = 1;
  pools[1].url = pools[0].url.toUpperCase();
  pools[1].username = pools[0].username;
  await expect(save(pools)).rejects.toThrow("distinct URL and username pairs");
  expect(client.poolServiceUpdatePool).not.toHaveBeenCalled();
});

test("an interrupted endpoint swap leaves a live pool and can re-enable the parked pool", async () => {
  const { client, draft, save, stored } = fixture();
  const pools = draft();
  [pools[0].url, pools[1].url] = [pools[1].url, pools[0].url];
  const write = client.poolServiceUpdatePool.getMockImplementation()!;
  client.poolServiceUpdatePool.mockImplementationOnce(async (path, request) => {
    const result = await write(path, request);
    if (!result.data.pool.enabled) throw new Error("Acknowledgement lost after disabling the pool");
    return result;
  });
  await expect(save(pools)).rejects.toThrow("Acknowledgement lost");
  const current = structuredClone(stored());
  expect(current.filter((pool) => pool.enabled)).toHaveLength(2);
  const recovery = draft(current).map((pool) => ({ ...pool, enabled: true }));
  const saved = await save(recovery, undefined, current);
  expect(saved.every((pool) => pool.enabled)).toBe(true);
});
