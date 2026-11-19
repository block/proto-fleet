import { Code } from "@connectrpc/connect";
import { isAuthenticationError } from "../errors";
import type {
  AssignmentPool,
  PoolAssignmentAttempt,
  PoolAssignmentChoice,
  PoolAssignmentService,
  PoolAssignmentStatus,
  PoolAssignmentTarget,
  SavedPoolSet,
} from "../poolAssignment";
import { PoolDeletionUnconfirmed } from "../poolAssignment";
import type { MiningGroupPoolSettings } from "../poolConfiguration";
import { validateContainerPoolURL, validateMiningGroupPoolURL } from "../poolURL";
import type { AdapterRequestOptions } from "../types";
import { mapWithConcurrency } from "../utils/async";
import type { MdkV2Client } from "./connectClient";
import { withDeadline } from "./controllerRecords";
import type { V2MiningGroupRecord } from "./generated/generatedApi";
import { groupStamp, toPoolSettings } from "./miningGroupRecords";
import { ct1DeviceId } from "./normalize";
import { defaultPoolSetError, publicPools, readDefaultPools, saveDefaultPools } from "./poolEdits";
import { poolRecoveryFingerprint } from "./poolRecoveryEvidence";
import { slotPosition } from "./slotPosition";
import { CONTAINER_REFUSAL_CODES, containerWriteError } from "./writeErrors";
import { isMacAddress } from "@/protoOS/utils/macAddress";
import { newUUID } from "@/protoOS/utils/uuid";

const LOCAL_REF = "proto-os:pools:";
const TIMEOUT = 10_000;
const OPTIONAL_INVENTORY_TIMEOUT_MS = 3_000;
const DELETE_REFUSAL_CODES = new Set([...CONTAINER_REFUSAL_CODES, Code.Aborted, Code.PermissionDenied]);
const STOP_WAIT_MS = 30_000;
const waitForNextStatus = (signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, 1000);
    const abort = () => {
      clearTimeout(timer);
      reject(signal?.reason);
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
const macKey = (mac: string) => mac.trim().toLowerCase();
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
const macs = (values: string[]) => [...new Set(values.map(macKey))].sort();
const owned = (group: V2MiningGroupRecord) => group.externalRef?.startsWith(LOCAL_REF) === true;
const customPolicy = (group: V2MiningGroupRecord) =>
  group.requestedBoardWatts !== undefined || !["", "NONE", "PLL_ALGORITHM_NONE"].includes(group.pllAlgorithm ?? "");
const mayMove = (group: V2MiningGroupRecord) => !group.externalRef || owned(group);
const revision = (group?: V2MiningGroupRecord) => {
  if (!group?.id || !/^[1-9]\d*$/.test(group.revision ?? ""))
    throw new Error("The controller did not report a complete pool assignment. Reload and review again.");
  return group.revision!;
};
const groupMemberMacs = (membership: Map<string, string>, groupId?: string) =>
  [...membership].filter(([, id]) => id === groupId).map(([mac]) => mac);
const groupHasMembers = (membership: Map<string, string>, groupId?: string) =>
  groupMemberMacs(membership, groupId).length > 0;
const assignedGroup = (enabled: boolean | undefined, membership: Map<string, string>, mac: string) =>
  enabled ? (membership.get(mac) ?? "") : "default";
const newPools = (pools: AssignmentPool[]): MiningGroupPoolSettings[] =>
  pools.map((pool, priority) => ({
    id: pool.id ?? "0",
    name: pool.name,
    url: pool.url,
    username: pool.username,
    priority,
    enabled: pool.enabled ?? true,
    shareWeight: pool.shareWeight,
  }));

const prepareEditedPools = (editing: V2MiningGroupRecord, original: SavedPoolSet, choice: PoolAssignmentChoice) => {
  if (editing.id === "default") {
    if (choice.name !== editing.name) throw new Error("The controller's Default configuration cannot be renamed.");
    if (!choice.expectedPools || !same(publicPools(original.pools), publicPools(choice.expectedPools)))
      throw new Error("Default pools changed. Reload pools before editing them.");
    if (choice.pools.some((pool) => pool.id && !pool.preservePassword && !pool.password))
      throw new Error("CT1 cannot clear a saved pool password. Keep the saved password or enter a new one.");
  }

  const poolIds = choice.pools.flatMap((pool) => (pool.id ? [pool.id] : []));
  if (new Set(poolIds).size !== poolIds.length || poolIds.some((id) => !original.pools.some((pool) => pool.id === id)))
    throw new Error("Pool slots changed. Reload the configuration.");
  if (
    choice.pools.some(
      (pool) =>
        pool.preservePassword &&
        !original.pools.some(
          (saved) => saved.id === pool.id && saved.url === pool.url && saved.username === pool.username,
        ),
    )
  )
    throw new Error("Enter a password after changing a pool URL or username.");

  const names = new Set(original.pools.map((pool) => pool.name));
  const pools = choice.pools.map((pool) => {
    const saved = original.pools.find((saved) => saved.id === pool.id);
    let name = saved?.name ?? pool.name;
    if (!saved) {
      let suffix = 2;
      while (names.has(name)) name = `${pool.name} (${suffix++})`;
      names.add(name);
    }
    const enabled = editing.id === "default" ? (pool.enabled ?? saved?.enabled) : saved?.enabled;
    return { ...pool, name, enabled: enabled ?? true, shareWeight: saved?.shareWeight };
  });

  if (editing.id === "default") {
    const error = defaultPoolSetError(pools);
    if (error) throw new Error(error);
  }
  return pools;
};

export function createPoolAssignment(
  client: MdkV2Client,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): PoolAssignmentService {
  const read = <T>(
    params: AdapterRequestOptions | undefined,
    request: (options: AdapterRequestOptions) => Promise<T>,
  ) => withDeadline(decorate(params), TIMEOUT, request);
  const snapshot = (
    params?: AdapterRequestOptions,
    {
      allowBlocked = false,
      inventory = "required",
    }: {
      allowBlocked?: boolean;
      inventory?: "required" | "optional" | "none";
    } = {},
  ) =>
    read(params, async (options) => {
      const before = (await client.miningGroupServiceGetMiningGroupCapability(options)).data.capability;
      if (
        before?.name !== "mining-groups-v1" ||
        before.version !== 1 ||
        before.assignmentGranularity !== "PHYSICAL_HASHBOARD" ||
        !/^[1-9]\d*$/.test(before.stateRevision ?? "")
      )
        throw new Error("This controller does not support local pool assignments.");
      if (before.suspended && !allowBlocked)
        throw new Error("Container mining is paused. Resume it before changing pool assignments.");
      if (!before.maxGroups || !before.maxPoolsPerGroup || !before.maxBoardsPerGroup)
        throw new Error("The controller did not report pool assignment limits.");
      const first = (await client.miningGroupServiceListMiningGroups(options)).data;
      const getInventory = () =>
        withDeadline(options, OPTIONAL_INVENTORY_TIMEOUT_MS, (request) =>
          client.hashboardServiceGetContainerMap(request),
        ).catch((error: unknown) => {
          if (options.signal?.aborted || isAuthenticationError(error)) throw error;
          return undefined;
        });
      let inventoryRequest;
      if (inventory === "required") inventoryRequest = client.hashboardServiceGetContainerMap(options);
      if (inventory === "optional") inventoryRequest = getInventory();
      const [members, map] = await Promise.all([
        client.miningGroupServiceListMiningGroupMemberships({}, options),
        inventoryRequest,
      ]);
      const groups = first.groups ?? [];
      const defaultIntent = groups.find((group) => group.id === "default")?.desiredMining;
      if (!allowBlocked && !before.enabled && typeof defaultIntent !== "boolean")
        throw new Error("The controller did not report the saved mining intent. Reload and review again.");
      const boards = new Map<string, { mac: string; location?: string }>();
      const modules: string[][] = [];
      const isolated = new Set<string>();
      const duplicates = new Set<string>();
      for (const tank of map?.data.containerMap?.tanks ?? [])
        for (const module of tank.modules ?? []) {
          const moduleBoards: string[] = [];
          for (const slot of module.slots ?? []) {
            if (!slot.occupied) continue;
            const mac = macKey(slot.hashboard?.macAddress ?? "");
            const duplicate = boards.has(mac) || duplicates.has(mac);
            if (duplicate || !isMacAddress(mac)) {
              if (inventory === "required")
                throw new Error("Board identity is incomplete or duplicated. Refresh the board inventory.");
              if (duplicate) {
                boards.delete(mac);
                duplicates.add(mac);
              }
              continue;
            }
            const position = slotPosition(slot).position;
            const location =
              tank.id && module.id && position
                ? `${tank.displayLabel || `Tank ${tank.displayNumber || tank.id}`} · Module ${module.id} · Board ${position}`
                : undefined;
            if (slot.hashboard?.isolated) isolated.add(mac);
            boards.set(mac, { mac, location });
            moduleBoards.push(mac);
          }
          modules.push(macs(moduleBoards));
        }
      if (inventory === "required" && !map?.data.containerMap)
        throw new Error("The controller did not report the board inventory.");
      const membership = new Map<string, string>();
      for (const member of members.data.memberships ?? []) {
        const mac = macKey(member.macAddress ?? "");
        if (!mac || !member.groupId || membership.has(mac))
          throw new Error("Board assignments are incomplete or duplicated. Reload pools.");
        membership.set(mac, member.groupId);
      }
      const last = (await client.miningGroupServiceListMiningGroups(options)).data;
      const after = (await client.miningGroupServiceGetMiningGroupCapability(options)).data.capability;
      for (const group of [...groups, ...(last.groups ?? [])]) revision(group);
      if (!same(before, after) || !same(groupStamp(groups), groupStamp(last.groups ?? [])))
        throw new Error("Pool assignments changed. Review the selection again.");
      const pending = (last.pendingOperations ?? []).some(
        (operation) => !["COMPLETE", "FAILED"].includes(operation.phase ?? ""),
      );
      if (!allowBlocked && pending)
        throw new Error("A pool assignment is still in progress. Check its status before starting another change.");
      const stamp = JSON.stringify([
        before,
        groupStamp(groups),
        [...membership].sort(),
        inventory === "required" ? [...boards.keys()].sort() : [],
        inventory === "required" ? modules : [],
      ]);
      return {
        capability: before,
        groups,
        boards,
        modules,
        membership,
        stamp,
        isolated,
        mining: defaultIntent === true,
        pending,
      };
    });
  type Snapshot = Awaited<ReturnType<typeof snapshot>>;
  const canAssignTo = (state: Snapshot, group: V2MiningGroupRecord, mining: boolean) =>
    mayMove(group) &&
    !customPolicy(group) &&
    ((group.desiredMining ?? false) === mining ||
      (group.id !== "default" && !groupHasMembers(state.membership, group.id)));
  const selected = (state: Snapshot, target: PoolAssignmentTarget) => {
    const selectedMacs = macs(target.macs);
    if (
      !selectedMacs.length ||
      selectedMacs.length !== target.macs.length ||
      selectedMacs.some((mac) => !state.boards.has(mac))
    )
      throw new Error("Select exact, currently reported physical hashboards.");
    if (selectedMacs.some((mac) => state.isolated.has(mac)))
      throw new Error("Remove isolation before changing pools for these hashboards.");
    if (target.modules?.some((module) => !state.modules.some((current) => same(current, macs(module)))))
      throw new Error("The selected modules changed. Close this dialog and select them again.");
    const sources = [...new Set(selectedMacs.map((mac) => state.membership.get(mac) ?? ""))];
    const mining = new Set<boolean>();
    for (const id of sources) {
      if (!state.capability.enabled) {
        mining.add(state.mining);
        continue;
      }
      if (!id) {
        mining.add(false);
        continue;
      }
      const group = state.groups.find((group) => group.id === id);
      revision(group);
      if (!mayMove(group!))
        throw new Error("These hashboards have Fleet-managed assignments. Change their pools in Proto Fleet.");
      if (customPolicy(group!))
        throw new Error("These hashboards use a custom power or tuning policy. Review that policy before moving them.");
      mining.add(group!.desiredMining === true);
    }
    if (mining.size !== 1)
      throw new Error(
        "Select hashboards with the same mining intent. Configure running and stopped assignments separately.",
      );
    return { macs: selectedMacs, mining: [...mining][0] };
  };
  const readPoolSet = async (group: V2MiningGroupRecord, options: AdapterRequestOptions): Promise<SavedPoolSet> => {
    const expected = revision(group);
    const pools =
      group.id === "default"
        ? await readDefaultPools(client, options)
        : ((await client.miningGroupServiceListMiningGroupPools({ groupId: group.id! }, options)).data.pools ?? []).map(
            toPoolSettings,
          );
    return {
      id: group.id!,
      name: group.name || "Saved pools",
      revision: expected,
      mining: group.desiredMining === true,
      pools: publicPools(pools),
    };
  };
  const poolSet = (group: V2MiningGroupRecord, params?: AdapterRequestOptions) =>
    read(params, async (options) => {
      const saved = await readPoolSet(group, options);
      const current = (await client.miningGroupServiceGetMiningGroup({ id: group.id! }, options)).data.group;
      if (revision(current) !== saved.revision) throw new Error("The saved pool set changed. Review it again.");
      return saved;
    });
  const status = async (
    attempt: PoolAssignmentAttempt,
    params?: AdapterRequestOptions,
    justSavedDefault = false,
  ): Promise<PoolAssignmentStatus> => {
    return read(params, async (options) => {
      const groups = (await client.miningGroupServiceListMiningGroups(options)).data;
      const destination = groups.groups?.find(
        (group) => group.id === attempt.destinationId || group.externalRef === attempt.destinationRef,
      );
      const members = (await client.miningGroupServiceListMiningGroupMemberships({}, options)).data.memberships ?? [];
      const assigned = new Set(
        members
          .filter((member) => destination && member.groupId === destination.id)
          .map((member) => macKey(member.macAddress ?? "")),
      );
      const remainingMacs = attempt.macs.filter((mac) => !assigned.has(mac));
      const confirmed = attempt.macs.length - remainingMacs.length;
      const operations = await mapWithConcurrency(
        attempt.operationIds,
        4,
        async (id) => {
          try {
            return (await client.miningGroupServiceGetMiningGroupOperation({ id }, options)).data.operation;
          } catch (error) {
            const failure = error as { status?: number; error?: { code?: number } };
            if (failure?.status === 404 || failure?.error?.code === Code.NotFound) return undefined;
            throw error;
          }
        },
        options.signal ?? undefined,
      );
      const blockers = operations.flatMap((operation) => [
        ...(operation?.stopBlockers ?? []).map((blocker) => ({
          mac: blocker.macAddress,
          reason: "Waiting for the previous owner to confirm that the board stopped.",
        })),
        ...(operation?.error ? [{ reason: operation.error }] : []),
      ]);
      const common = {
        confirmed: attempt.configurationOnly ? 0 : confirmed,
        total: attempt.configurationOnly ? 0 : attempt.macs.length,
        blockers,
      };
      if (operations.some((operation) => operation?.phase === "FAILED"))
        return {
          ...common,
          state: "failed",
          message: "A pool transfer failed. Review the board status before making another change.",
        };
      if (
        operations.some((operation) => operation && !["COMPLETE", "FAILED"].includes(operation.phase ?? "")) ||
        groups.pendingOperations?.some(
          (operation) =>
            operation.macAddresses?.some((mac) => attempt.macs.includes(macKey(mac))) &&
            !["COMPLETE", "FAILED"].includes(operation.phase ?? ""),
        )
      )
        return {
          ...common,
          state: "pending",
          message: "Waiting for the controller to finish moving the selected hashboards.",
        };
      if (destination && (attempt.configurationOnly || confirmed === attempt.macs.length)) {
        const currentRevision = revision(destination);
        const editingDefault = destination.id === "default" && attempt.configurationOnly;
        const currentCredentials = editingDefault
          ? justSavedDefault
          : currentRevision === attempt.destinationRevision ||
            (!attempt.configurationOnly && currentRevision === attempt.unconfirmedTransferRevision);
        if (!attempt.configurationConfirmed || !currentCredentials) {
          let message = "Board assignments match, but the latest pool credentials could not be confirmed.";
          if (editingDefault)
            message =
              "Some Default pool changes may be saved. Reload pools and check which pools are enabled before editing again.";
          else if (attempt.configurationOnly)
            message = "The latest pool update could not be confirmed. Reload pools before making another change.";
          return {
            ...common,
            state: "unknown",
            message,
          };
        }
        const saved = await poolSet(destination, options);
        const poolsMatch = attempt.poolFingerprint
          ? poolRecoveryFingerprint(saved.pools) === attempt.poolFingerprint
          : same(publicPools(saved.pools), publicPools(attempt.pools));
        if (poolsMatch) {
          if (attempt.name && saved.name !== attempt.name)
            return {
              ...common,
              state: "unknown",
              message:
                "Pool settings are saved, but the configuration name is not confirmed. Reload pools before making another change.",
            };
          if (attempt.configurationOnly)
            return {
              ...common,
              state: "saved",
              message: "Pool configuration saved. Hashboard assignments are unchanged.",
            };
          return {
            ...common,
            state: "saved",
            message: "Pool assignment saved.",
          };
        }
      }
      return {
        ...common,
        state: confirmed ? "partial" : "unknown",
        ...(confirmed && destination && attempt.configurationConfirmed ? { remainingMacs } : {}),
        message:
          !destination && attempt.restoreLegacyMining
            ? "Review the pool change again when you are ready to continue."
            : "The final assignment is not confirmed. Some changes may be saved. Check status before making another change.",
      };
    });
  };
  const configurations = (state: Snapshot, groups: V2MiningGroupRecord[], params?: AdapterRequestOptions) =>
    read(params, async (options) => {
      const configurations = await mapWithConcurrency(
        groups,
        4,
        async (group) => ({
          ...(await readPoolSet(group, options)),
          assignedCount: groupMemberMacs(state.membership, group.id).length,
          deletable: group.id !== "default" && mayMove(group),
          isDefault: group.id === "default",
        }),
        options.signal ?? undefined,
      );
      const current = (await client.miningGroupServiceListMiningGroups(options)).data.groups ?? [];
      if (!same(groupStamp(current), groupStamp(state.groups)))
        throw new Error("Pool configurations changed. Reload pools.");
      return configurations;
    });
  return {
    async listConfigurations(params) {
      const state = await snapshot(params, { allowBlocked: true, inventory: "none" });
      return configurations(state, state.groups, params);
    },
    async listConfigurationPage(offset, limit, params) {
      const state = await snapshot(params, { allowBlocked: true, inventory: "none" });
      const page = state.groups.slice(Math.max(0, offset), Math.max(0, offset) + Math.max(0, limit));
      return { configurations: await configurations(state, page, params), total: state.groups.length };
    },
    async deleteConfiguration(id, expectedRevision, params) {
      const state = await snapshot(params, { inventory: "none" });
      const group = state.groups.find((group) => group.id === id);
      if (!group || group.id === "default" || revision(group) !== expectedRevision)
        throw new Error("The configuration changed or cannot be deleted. Reload pools.");
      if (!mayMove(group)) throw new Error("Delete this configuration in Proto Fleet.");
      if ([...state.membership.values()].includes(id))
        throw new Error("Reassign this configuration's equipment before deleting it.");
      try {
        await read(params, (options) =>
          client.miningGroupServiceDeleteMiningGroup({ id, expectedRevision, idempotencyKey: newUUID() }, options),
        );
      } catch (error) {
        if (params?.signal?.aborted) throw error;
        const failure = containerWriteError(error, "The configuration was not deleted.", "", DELETE_REFUSAL_CODES);
        if ((failure as { writeOutcome?: string }).writeOutcome === "unknown") throw new PoolDeletionUnconfirmed(error);
        throw failure;
      }
      try {
        const remaining =
          (await read(params, (options) => client.miningGroupServiceListMiningGroups(options))).data.groups ?? [];
        if (remaining.some((group) => group.id === id)) throw new PoolDeletionUnconfirmed();
      } catch (error) {
        if (params?.signal?.aborted || error instanceof PoolDeletionUnconfirmed) throw error;
        throw new PoolDeletionUnconfirmed(error);
      }
    },
    async checkMiningRecovery(attempt, controllerId, track, params) {
      if (!attempt.restoreLegacyMining) return status(attempt, params);
      const mining = await read(params, async (options) => {
        const { data } = await client.systemServiceGetSystemInfo(options);
        if (!data.info?.id || ct1DeviceId(data.info.id) !== controllerId)
          throw new Error("The controller changed. Reconnect to the original controller to check mining.");
        const { data: current } = await client.miningGroupServiceGetMiningGroup({ id: "default" }, options);
        if (current.group?.id !== "default" || typeof current.group.desiredMining !== "boolean")
          throw new Error("The controller did not report its mining state. Check mining on the controller.");
        return current.group.desiredMining;
      });
      const checked = { ...attempt, restoreLegacyMining: false, phase: "Checking saved pools and membership" };
      const result = await status(checked, params);
      params?.signal?.throwIfAborted();
      track(checked);
      return mining
        ? result
        : {
            ...result,
            message: `${result.message} Default mining is set to stop. Resume it on the controller if needed.`,
          };
    },
    async inspect(target, params) {
      const state = await snapshot(params, { inventory: target.macs.length ? "required" : "none" });
      const selection = target.macs.length ? selected(state, target) : { macs: [], mining: false };
      const savedPools =
        state.capability.enabled && target.macs.length
          ? await read(params, (options) =>
              mapWithConcurrency(
                state.groups.filter((group) => canAssignTo(state, group, selection.mining)),
                4,
                (group) => poolSet(group, options),
                options.signal ?? undefined,
              ),
            )
          : [];
      const assignablePools = savedPools.filter((set) => set.pools.length > 0);
      const currentIds = new Set(selection.macs.map((mac) => state.membership.get(mac)));
      return {
        boards: selection.macs.map((mac) => state.boards.get(mac)!),
        savedPools: assignablePools,
        currentPoolSetId: currentIds.size === 1 ? assignablePools.find((set) => currentIds.has(set.id))?.id : undefined,
        mixedConfigurations: state.capability.enabled === true && currentIds.size > 1,
        maxPools: state.capability.maxPoolsPerGroup!,
        requiresActivation: !state.capability.enabled,
      };
    },
    async review(target, choice, params) {
      const state = await snapshot(params, { inventory: choice.editPoolSetId ? "optional" : "required" });
      const editing = choice.editPoolSetId
        ? state.groups.find((group) => group.id === choice.editPoolSetId)
        : undefined;
      if (choice.editPoolSetId && !editing)
        throw new Error("This configuration cannot be edited locally. Reload pools.");
      let selection = { macs: [] as string[], mining: !state.capability.enabled && state.mining };
      if (editing) {
        selection = {
          macs: macs(groupMemberMacs(state.membership, editing.id)),
          mining: state.capability.enabled ? editing.desiredMining === true : state.mining,
        };
      } else if (target.macs.length) {
        selection = selected(state, target);
      } else if (!choice.createOnly) {
        throw new Error("Select hashboards before assigning pools.");
      }
      let originalPools: MiningGroupPoolSettings[] | undefined;
      let selectedPools = choice.pools;
      if (editing) {
        if (revision(editing) !== choice.expectedRevision)
          throw new Error("The configuration changed. Reload pools before editing it.");
        const original = await poolSet(editing, params);
        originalPools = original.pools;
        selectedPools = prepareEditedPools(editing, original, choice);
      }
      if (!state.capability.enabled && state.isolated.size)
        throw new Error("Remove hashboard isolation before enabling pool assignments.");
      if (!state.capability.enabled && state.boards.size > state.capability.maxBoardsPerGroup!)
        throw new Error("Enabling assignments would exceed the default group's hashboard limit.");
      const id = newUUID();
      let savedPoolSet: SavedPoolSet | undefined;
      if (choice.savedPoolSetId) {
        const group = state.groups.find((group) => group.id === choice.savedPoolSetId);
        if (!state.capability.enabled || !group || !canAssignTo(state, group, selection.mining))
          throw new Error("The saved pool set is no longer available for this selection.");
        savedPoolSet = await poolSet(group, params);
        if (!savedPoolSet.pools.length) throw new Error("The saved pool set no longer contains any pools.");
      }
      const maxPools =
        editing?.id === "default"
          ? Math.max(state.capability.maxPoolsPerGroup!, originalPools?.length ?? 0)
          : state.capability.maxPoolsPerGroup!;
      if (!savedPoolSet && (!selectedPools.length || selectedPools.length > maxPools))
        throw new Error("Use between one pool and the controller's reported pool limit.");
      if (
        !savedPoolSet &&
        selectedPools.some(
          (pool) =>
            !pool.name.trim() ||
            Boolean((editing?.id === "default" ? validateContainerPoolURL : validateMiningGroupPoolURL)(pool.url)) ||
            !pool.username.trim(),
        )
      )
        throw new Error("Each pool needs a name, URL, and complete username.");
      const destination = editing ?? state.groups.find((group) => group.id === savedPoolSet?.id);
      const destinationMembers = groupMemberMacs(state.membership, destination?.id);
      if (new Set([...destinationMembers, ...selection.macs]).size > state.capability.maxBoardsPerGroup!)
        throw new Error("The selected pool set would exceed the controller's hashboard limit.");
      if (!destination && state.groups.length >= state.capability.maxGroups!)
        throw new Error("The controller has no room for another pool set. Reuse a saved set or reduce the selection.");
      const transfers = [
        ...new Set(selection.macs.map((mac) => assignedGroup(state.capability.enabled, state.membership, mac))),
      ]
        .filter((source) => source !== destination?.id)
        .map((source) => ({
          source,
          macs: selection.macs.filter(
            (mac) => assignedGroup(state.capability.enabled, state.membership, mac) === source,
          ),
        }));
      return {
        id,
        configurationOnly: choice.createOnly === true || Boolean(editing),
        editingRevision: editing ? revision(editing) : undefined,
        originalPools,
        name: savedPoolSet?.name || choice.name?.trim() || selectedPools[0]?.name || "Pool configuration",
        target: editing ? { macs: selection.macs } : { ...target, macs: selection.macs },
        boards: selection.macs.map((mac) => state.boards.get(mac) ?? { mac }),
        stamp: state.stamp,
        pools: savedPoolSet ? [] : selectedPools.map((pool) => ({ ...pool, url: pool.url.trim() })),
        savedPoolSet,
        mining: selection.mining,
        requiresActivation: !state.capability.enabled,
        destinationId: destination?.id,
        destinationRef: destination?.externalRef || LOCAL_REF + id,
        transfers,
      };
    },
    status,
    async apply(plan, track, params) {
      const snapshotForPlan = () => snapshot(params, { inventory: plan.editingRevision ? "none" : "required" });
      let state = await snapshotForPlan();
      if (state.stamp !== plan.stamp)
        throw new Error("Pool assignments changed. Review the selection again before saving.");
      if (!plan.editingRevision && plan.target.macs.length && selected(state, plan.target).mining !== plan.mining)
        throw new Error("The mining intent changed. Review the selection again.");
      let attempt: PoolAssignmentAttempt = {
        id: plan.id,
        name: plan.name,
        configurationOnly: plan.configurationOnly,
        macs: plan.target.macs,
        destinationId: plan.destinationId,
        destinationRevision: plan.savedPoolSet?.revision,
        destinationRef: plan.destinationRef,
        pools: plan.savedPoolSet?.pools ?? newPools(plan.pools),
        configurationConfirmed: Boolean(plan.savedPoolSet),
        operationIds: [],
        phase: "Preparing pool assignment",
        restoreLegacyMining: plan.requiresActivation && plan.mining,
      };
      const write = async <T>(phase: string, request: (options: AdapterRequestOptions) => Promise<T>) => {
        params?.signal?.throwIfAborted();
        attempt = { ...attempt, phase };
        track(attempt);
        return read(params, request);
      };
      const refresh = async (checkMining = true) => {
        state = await snapshotForPlan();
        if (
          checkMining &&
          !plan.configurationOnly &&
          plan.target.macs.length &&
          selected(state, plan.target).mining !== plan.mining
        )
          throw new Error("The selected boards' mining intent changed. Check status and review again.");
        return state;
      };
      if (plan.requiresActivation) {
        await write("Stopping container mining before enabling pool assignments", (options) =>
          client.miningServiceStopMining(options),
        );
        const stopDeadline = Date.now() + STOP_WAIT_MS;
        while (true) {
          const remaining = stopDeadline - Date.now();
          if (remaining <= 0)
            throw new Error("Container mining is still stopping. Check its status before continuing.");
          const { data } = await withDeadline(decorate(params), Math.min(remaining, TIMEOUT), (options) =>
            client.miningServiceGetMiningStatus(options),
          );
          if (data.mining !== true && (data.activeBoards ?? 0) === 0) break;
          await waitForNextStatus(params?.signal ?? undefined);
        }
        await write("Enabling pool assignments", (options) =>
          client.miningGroupServiceActivateMiningGroups(
            { expectedStateRevision: state.capability.stateRevision, idempotencyKey: plan.id + ":activate" },
            options,
          ),
        );
        await refresh(false);
        if (attempt.restoreLegacyMining && !plan.configurationOnly) return status(attempt, params);
      }
      let destination = state.groups.find((group) => group.id === plan.destinationId);
      if (!destination) {
        if (plan.editingRevision) throw new Error("The configuration was removed. Reload pools.");
        if (state.groups.length >= state.capability.maxGroups!)
          throw new Error("The controller's pool-set limit was reached. Review again.");
        const response = await write("Creating the selected pool set", (options) =>
          client.miningGroupServiceCreateMiningGroup(
            {
              metadata: { name: plan.name, externalRef: plan.destinationRef },
              idempotencyKey: plan.id + ":create",
            },
            options,
          ),
        );
        destination = response.data.group;
        revision(destination);
        attempt = { ...attempt, destinationId: destination!.id };
        track(attempt);
      }
      const destinationId = destination!.id!;
      await refresh();
      destination = state.groups.find((group) => group.id === destinationId);
      if (!destination || (!plan.editingRevision && !canAssignTo(state, destination, plan.mining)))
        throw new Error("The destination configuration is no longer available. Check status.");
      if (
        plan.editingRevision &&
        (revision(destination) !== plan.editingRevision ||
          !same(macs(groupMemberMacs(state.membership, destinationId)), plan.target.macs))
      )
        throw new Error("The configuration changed. Reload and review it again.");
      if (destination.id === "default" && plan.editingRevision) {
        if (!plan.configurationOnly || !plan.originalPools || plan.name !== destination.name)
          throw new Error("Reload Default pools and review the edit again.");
        const assignments = (current: Snapshot) =>
          JSON.stringify([current.capability, groupStamp(current.groups), [...current.membership].sort()]);
        const expectedAssignments = assignments(state);
        const saved = await saveDefaultPools(
          client,
          plan.originalPools,
          plan.pools,
          decorate(params),
          async () => {
            const current = await snapshot(params, { allowBlocked: true, inventory: "none" });
            if (assignments(current) !== expectedAssignments || current.capability.suspended || current.pending)
              throw new Error("Pool assignments changed. Some pool settings may be saved. Reload and review again.");
          },
          (phase, pools) => {
            attempt = { ...attempt, phase, pools };
            track(attempt);
          },
        );
        attempt = { ...attempt, pools: saved, configurationConfirmed: true };
        track(attempt);
        return status(attempt, params, true);
      }
      if (!plan.savedPoolSet) {
        if (groupMemberMacs(state.membership, destinationId).some((mac) => !plan.target.macs.includes(mac)))
          throw new Error("Unselected hashboards now use this pool set. Review again.");
        if (plan.pools.length > state.capability.maxPoolsPerGroup!)
          throw new Error("The controller's pool limit changed. Review again.");
        const response = await write("Saving pool credentials", (options) =>
          client.miningGroupServiceReplaceMiningGroupPools(
            { groupId: destinationId },
            {
              expectedRevision: revision(destination),
              idempotencyKey: plan.id + ":pools",
              pools: newPools(plan.pools).map((pool, index) => ({
                pool: { ...pool, id: Number(pool.id) },
                ...(plan.pools[index].preservePassword ? {} : { password: plan.pools[index].password }),
              })),
            },
            options,
          ),
        );
        destination = response.data.group;
        revision(destination);
        const withoutIds = (pools: MiningGroupPoolSettings[]) => publicPools(pools).map(({ id: _id, ...pool }) => pool);
        const receipt = publicPools((response.data.pools ?? []).map(toPoolSettings));
        if (!receipt.length || !same(withoutIds(receipt), withoutIds(newPools(plan.pools))))
          throw new Error("The controller returned an incomplete pool update receipt. Check status.");
        attempt = {
          ...attempt,
          pools: receipt,
          configurationConfirmed: true,
          destinationRevision: revision(destination),
        };
        track(attempt);
        const saved = await poolSet(destination!, params);
        if (!same(withoutIds(saved.pools), withoutIds(newPools(plan.pools))))
          throw new Error("The controller did not confirm the saved pool configuration. Check status.");
        attempt = { ...attempt, pools: saved.pools };
        track(attempt);
      } else if (revision(destination) !== plan.savedPoolSet.revision)
        throw new Error("The saved pools changed. Review them again.");
      if (destination!.name !== plan.name) {
        const response = await write("Saving configuration name", (options) =>
          client.miningGroupServiceUpdateMiningGroup(
            { id: destinationId },
            {
              expectedRevision: revision(destination),
              metadata: { name: plan.name },
              updateMask: "name",
              idempotencyKey: plan.id + ":name",
            },
            options,
          ),
        );
        destination = response.data.group;
        attempt = { ...attempt, destinationRevision: revision(destination) };
        track(attempt);
      }
      if (!plan.configurationOnly && (destination!.desiredMining ?? false) !== plan.mining) {
        const response = await write("Preserving the selected boards' mining intent", (options) =>
          client.miningGroupServiceUpdateMiningGroupIntent(
            { id: destinationId },
            {
              expectedRevision: revision(destination),
              desiredMining: plan.mining,
              updateMask: "desiredMining",
              idempotencyKey: plan.id + ":intent",
            },
            options,
          ),
        );
        destination = response.data.group;
        attempt = { ...attempt, destinationRevision: revision(destination) };
        track(attempt);
      }
      let expectedDestinationRevision = revision(destination);
      for (const [index, transfer] of (plan.configurationOnly ? [] : plan.transfers).entries()) {
        await refresh();
        destination = state.groups.find((group) => group.id === destinationId);
        if (
          !destination ||
          !mayMove(destination) ||
          customPolicy(destination) ||
          (destination.desiredMining ?? false) !== plan.mining
        )
          throw new Error("The destination policy changed. Check the saved assignments.");
        if (revision(destination) !== expectedDestinationRevision)
          throw new Error("The destination configuration changed. Check the saved assignments.");
        // Default pool settings can change through the legacy API without a group revision change.
        if (
          plan.savedPoolSet?.id === "default" &&
          !same((await poolSet(destination, params)).pools, plan.savedPoolSet.pools)
        )
          throw new Error("Default pool settings changed. Reload and review again.");
        const source = state.groups.find((group) => group.id === transfer.source);
        if (transfer.macs.some((mac) => (state.membership.get(mac) ?? "") !== transfer.source))
          throw new Error("Board membership changed. Check the saved assignments.");
        const count = groupMemberMacs(state.membership, destinationId).length;
        if (count + transfer.macs.length > state.capability.maxBoardsPerGroup!)
          throw new Error("The destination hashboard limit was reached. Check the saved assignments.");
        const nextRevision = String(BigInt(expectedDestinationRevision) + 1n);
        attempt = { ...attempt, unconfirmedTransferRevision: nextRevision };
        const response = await write("Moving selected hashboards", (options) =>
          client.miningGroupServiceRequestMiningGroupTransfer(
            {
              sourceGroupId: transfer.source,
              destinationGroupId: destinationId,
              sourceExpectedRevision: source ? revision(source) : "0",
              destinationExpectedRevision: expectedDestinationRevision,
              idempotencyKey: plan.id + ":transfer:" + index,
              macAddresses: transfer.macs,
            },
            options,
          ),
        );
        if (!response.data.operation?.id)
          throw new Error(
            "The controller did not return a transfer receipt. Check status before making another change.",
          );
        expectedDestinationRevision = nextRevision;
        attempt = {
          ...attempt,
          destinationRevision: expectedDestinationRevision,
          unconfirmedTransferRevision: undefined,
          operationIds: [...attempt.operationIds, response.data.operation.id],
        };
        track(attempt);
        let operation = response.data.operation;
        for (let poll = 0; operation.phase !== "COMPLETE" && operation.phase !== "FAILED" && poll < 30; poll++) {
          attempt = {
            ...attempt,
            phase: `Waiting for hashboards to stop: ${operation.stopBoardsConfirmed ?? 0}/${operation.stopBoardsTotal ?? transfer.macs.length}`,
          };
          track(attempt);
          await waitForNextStatus(params?.signal ?? undefined);
          operation = (
            await read(params, (options) =>
              client.miningGroupServiceGetMiningGroupOperation({ id: operation.id! }, options),
            )
          ).data.operation!;
          if (!operation?.id)
            throw new Error(
              "The controller did not return transfer status. Check status before making another change.",
            );
        }
        if (operation.phase !== "COMPLETE") return status(attempt, params);
      }
      if (!plan.configurationOnly && plan.transfers.length) {
        await refresh();
        if (revision(state.groups.find((group) => group.id === destinationId)) !== expectedDestinationRevision)
          throw new Error("The destination configuration changed. Check the saved assignments.");
      }
      attempt = { ...attempt, phase: "Checking saved pools and membership" };
      track(attempt);
      return status(attempt, params);
    },
  };
}
