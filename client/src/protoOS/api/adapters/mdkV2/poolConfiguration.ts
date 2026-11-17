import { Code } from "@connectrpc/connect";
import { isAuthenticationError } from "../errors";
import {
  type ContainerPoolConfiguration,
  hasPendingPoolOperations,
  type MiningGroupPoolSettings,
  type PoolBoardAssignment,
} from "../poolConfiguration";
import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import { withDeadline } from "./controllerRecords";
import type { V2GetContainerMapResponse, V2MiningGroupOperationRecord } from "./generated/generatedApi";
import { slotPosition } from "./slotPosition";
import { containerWriteError } from "./writeErrors";

export const POOL_READ_TIMEOUT_MS = 10_000;
const macKey = (mac: string) => mac.trim().toLowerCase();
class UnsupportedGroupCapability extends Error {}

async function readGroupCapability(client: MdkV2Client, params: AdapterRequestOptions) {
  const { data } = await client.miningGroupServiceGetMiningGroupCapability(params);
  const value = data.capability;
  if (
    value?.name !== "mining-groups-v1" ||
    value.version !== 1 ||
    value.assignmentGranularity !== "PHYSICAL_HASHBOARD"
  ) {
    throw new UnsupportedGroupCapability(
      "The controller did not report mining-group mode. Pool editing is unavailable.",
    );
  }
  if (!/^\d+$/.test(value.stateRevision ?? ""))
    throw new Error("The controller did not report a mining-group revision.");
  return value;
}

function boardLocations(map: V2GetContainerMapResponse) {
  const boards = new Map<string, { mac: string; locations: Set<string> }>();
  for (const tank of map.containerMap?.tanks ?? []) {
    for (const module of tank.modules ?? []) {
      for (const slot of module.slots ?? []) {
        const mac = slot.hashboard?.macAddress;
        if (!slot.occupied || !mac?.trim()) continue;
        const key = macKey(mac);
        const board = boards.get(key) ?? { mac, locations: new Set<string>() };
        const { position } = slotPosition(slot);
        if ((tank.id ?? 0) > 0 && (module.id ?? 0) > 0 && (position ?? 0) > 0) {
          const label = tank.displayLabel?.trim() || `Tank ${tank.displayNumber || tank.id}`;
          board.locations.add(`${label} · Module ${module.id} · Board ${position}`);
        }
        boards.set(key, board);
      }
    }
  }
  return new Map<string, PoolBoardAssignment>(
    [...boards].map(([key, board]) => [
      key,
      {
        mac: board.mac,
        location: board.locations.size === 1 ? [...board.locations][0] : undefined,
      },
    ]),
  );
}

/** Read group membership once; pool credentials are loaded only for groups the operator opens. */
export function readContainerPoolConfiguration(client: MdkV2Client, params: AdapterRequestOptions) {
  return withDeadline(params, POOL_READ_TIMEOUT_MS, async (options): Promise<ContainerPoolConfiguration> => {
    let before;
    try {
      before = await readGroupCapability(client, options);
    } catch (error) {
      const failure = error as { status?: number; error?: { code?: number } } | undefined;
      if (
        !isAuthenticationError(error) &&
        (error instanceof UnsupportedGroupCapability ||
          failure?.status === 501 ||
          failure?.error?.code === Code.Unimplemented)
      ) {
        return {
          mode: "unsupported",
          suspended: false,
          groups: [],
          unassignedBoards: [],
          locationsAvailable: false,
          inventoryIssues: [],
          pendingOperations: [],
        };
      }
      throw error;
    }
    const { data: groupsBefore } = await client.miningGroupServiceListMiningGroups(options);
    const configuration: ContainerPoolConfiguration = {
      mode: before.enabled ? "groups" : "container",
      suspended: before.suspended === true,
      groups: [],
      unassignedBoards: [],
      locationsAvailable: false,
      inventoryIssues: [],
      pendingOperations: toOperations(groupsBefore.pendingOperations),
    };
    // In legacy mode, leftover group records and topology do not describe the active pool configuration.
    if (!before.enabled) {
      requireUnchangedMode(before, await readGroupCapability(client, options));
      return configuration;
    }
    const [membership, map] = await Promise.all([
      client.miningGroupServiceListMiningGroupMemberships({}, options),
      client.hashboardServiceGetContainerMap(options).catch((error) => {
        if (options.signal?.aborted) throw error;
        return undefined;
      }),
    ]);
    // Membership changes advance group revisions, not the capability's state revision.
    // Bound membership reads with two group-list reads; progress is not an assignment revision.
    const { data: groupsAfter } = await client.miningGroupServiceListMiningGroups(options);
    const after = await readGroupCapability(client, options);
    requireUnchangedMode(before, after);
    const revisions = new Map((groupsBefore.groups ?? []).map(({ id, revision }) => [id, revision]));
    if (
      revisions.size !== (groupsAfter.groups ?? []).length ||
      (groupsAfter.groups ?? []).some(({ id, revision }) => !revisions.has(id) || revisions.get(id) !== revision)
    )
      throw new Error("Group assignments changed during the read. Reload pools.");
    const locations = boardLocations(map?.data ?? {});
    const memberBoards = new Set<string>();
    const groups: ContainerPoolConfiguration["groups"] = (groupsBefore.groups ?? []).map(({ id, name, revision }) => {
      if (!id || !revision || !/^\d+$/.test(revision))
        throw new Error("The controller reported an incomplete mining group.");
      return {
        id,
        name: name || id,
        revision,
        boards: [],
      };
    });
    for (const member of membership.data.memberships ?? []) {
      if (!member.macAddress?.trim() || !member.groupId)
        throw new Error("The controller reported an incomplete pool assignment.");
      const key = macKey(member.macAddress);
      memberBoards.add(key);
      let group = groups.find((group) => group.id === member.groupId);
      // Retain orphan memberships instead of presenting their boards as unassigned.
      if (!group) {
        group = { id: member.groupId, name: `Unknown group (${member.groupId})`, revision: "", boards: [] };
        groups.push(group);
      }
      if (!group.boards.some((board) => macKey(board.mac) === key)) {
        group.boards.push(locations.get(key) ?? { mac: member.macAddress });
      }
    }
    return {
      ...configuration,
      groups,
      unassignedBoards: [...locations].filter(([key]) => !memberBoards.has(key)).map(([, board]) => board),
      locationsAvailable: Boolean(map?.data.containerMap),
      pendingOperations: toOperations(groupsAfter.pendingOperations),
      inventoryIssues: (groupsAfter.inventoryIssues ?? []).map((issue) => ({
        groupId: issue.groupId || undefined,
        mac: issue.macAddress || undefined,
        reason: issue.reason || "Unknown inventory issue",
      })),
    };
  });
}

export function readMiningGroupPools(
  client: MdkV2Client,
  groupId: string,
  revision: string,
  params: AdapterRequestOptions,
) {
  return withDeadline(params, POOL_READ_TIMEOUT_MS, async (options): Promise<MiningGroupPoolSettings[]> => {
    if (!groupId || !revision) throw new Error("Reload the group assignments before reading pools.");
    const { data } = await client.miningGroupServiceListMiningGroupPools({ groupId }, options);
    const current = (await client.miningGroupServiceGetMiningGroup({ id: groupId }, options)).data.group;
    if (current?.revision !== revision) throw new Error("This group changed. Reload the group assignments.");
    return (data.pools ?? [])
      .map((pool) => ({
        id: String(pool.id ?? ""),
        name: pool.name ?? "",
        url: pool.url ?? "",
        username: pool.username ?? "",
        priority: pool.priority ?? 0,
        enabled: pool.enabled === true,
        shareWeight: pool.shareWeight,
      }))
      .sort((a, b) => a.priority - b.priority);
  });
}

type GroupCapability = Awaited<ReturnType<typeof readGroupCapability>>;
function requireUnchangedMode(before: GroupCapability, after: GroupCapability) {
  if (
    before.stateRevision !== after.stateRevision ||
    Boolean(before.enabled) !== Boolean(after.enabled) ||
    Boolean(before.suspended) !== Boolean(after.suspended)
  )
    throw new Error("Mining-group mode changed during the read. Reload pools.");
}

const toOperations = (
  operations: V2MiningGroupOperationRecord[] = [],
): ContainerPoolConfiguration["pendingOperations"] =>
  operations.map((operation) => ({
    id: operation.id ?? "",
    kind: operation.kind || "Group change",
    phase: operation.phase || "Unknown",
    error: operation.error || undefined,
    boards: operation.macAddresses ?? [],
  }));

/** Full preflight once; later reorder steps need only a fresh capability with the same revision. */
export async function requireContainerPoolMode(
  client: MdkV2Client,
  params: AdapterRequestOptions,
  expectedRevision?: string,
) {
  for (let attempt = 0; ; attempt++) {
    try {
      return await withDeadline(params, POOL_READ_TIMEOUT_MS, async (options) => {
        const state = await readGroupCapability(client, options);
        if (state.enabled)
          throw new Error("Mining groups are enabled. Container-wide pool editing is unavailable here.");
        if (state.suspended) throw new Error("Mining groups are suspended. Reload pools before making changes.");
        if (expectedRevision !== undefined) {
          if (state.stateRevision !== expectedRevision)
            throw new Error("Mining-group mode changed. Reload pools before making changes.");
          return state.stateRevision!;
        }
        const { data } = await client.miningGroupServiceListMiningGroups(options);
        if (hasPendingPoolOperations(toOperations(data.pendingOperations)))
          throw new Error("A mining-group change is pending. Wait for it to finish and reload pools.");
        const after = await readGroupCapability(client, options);
        if (state.stateRevision !== after.stateRevision || after.enabled || after.suspended)
          throw new Error("Mining-group mode changed. Reload pools before making changes.");
        return after.stateRevision!;
      });
    } catch (error) {
      if (params.signal?.aborted) throw error;
      if (isAuthenticationError(error)) throw error;
      // Only repeat a read between reorder steps. A failed mutation may already be saved.
      if (expectedRevision !== undefined && attempt === 0 && isTransientReadError(error)) continue;
      const message =
        error instanceof Error
          ? error.message
          : "Could not confirm mining-group mode. Reload pools before making changes.";
      throw Object.assign(new Error(message), { writeOutcome: "refused" as const });
    }
  }
}

const isTransientReadError = (error: unknown) => {
  const failure = error as { status?: number; error?: { code?: number }; cause?: unknown } | undefined;
  return (
    [408, 502, 503, 504].includes(failure?.status ?? 0) ||
    [Code.Unavailable, Code.DeadlineExceeded].includes(failure?.error?.code ?? 0) ||
    (failure?.cause instanceof DOMException && failure.cause.name === "TimeoutError")
  );
};

// Pool reloads can fail with FailedPrecondition after persistence. That code is not a refusal here.
const POOL_REFUSAL_CODES = new Set<number>([
  Code.InvalidArgument,
  Code.Unimplemented,
  Code.NotFound,
  Code.AlreadyExists,
  Code.PermissionDenied,
]);
export function poolWriteError(error: unknown, savedPriorities = 0) {
  if (savedPriorities > 0) {
    const failure = error as { status?: number; error?: unknown } | undefined;
    return Object.assign(
      new Error(
        "Some pool priorities were saved. The final order is not confirmed. Review the reloaded configuration before trying again.",
      ),
      {
        cause: error,
        writeOutcome: "unknown" as const,
        status: failure?.status,
        error: failure?.error,
      },
    );
  }
  return containerWriteError(
    error,
    "The pool settings were not changed.",
    "The pool change may have been saved. Review the reloaded configuration before trying again.",
    POOL_REFUSAL_CODES,
  );
}
