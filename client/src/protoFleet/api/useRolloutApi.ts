import { useCallback, useMemo, useRef, useState } from "react";
import { create, type JsonObject } from "@bufbuild/protobuf";

import { deviceSetClient, rolloutClient } from "@/protoFleet/api/clients";
import {
  AbortRolloutRequestSchema,
  AdmitRolloutRequestSchema,
  CompleteRolloutRequestSchema,
  ContinueRolloutRequestSchema,
  CreateRolloutLaneRequestSchema,
  CreateRolloutRequestSchema,
  GetRolloutLaneRequestSchema,
  GetRolloutRequestSchema,
  ListRolloutLanesRequestSchema,
  ListRolloutsRequestSchema,
  PauseRolloutRequestSchema,
  type Rollout as ProtoRollout,
  type RolloutLane as ProtoRolloutLane,
  RolloutState as ProtoRolloutState,
  ResumeRolloutRequestSchema,
  RevertRolloutRequestSchema,
  StartRolloutLaneRequestSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { assertNotAborted, isAbortError, isAuthOrPermissionError, toError } from "@/protoFleet/api/requestErrors";
import { emitRolloutChanged } from "@/protoFleet/api/rolloutEvents";
import { mapRollout, mapRolloutLane, mapRolloutStateToProto } from "@/protoFleet/api/rolloutMappers";
import type {
  RolloutLane,
  RolloutLaneReleaseTarget,
  RolloutLifecycleState,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";
import { useAuthErrors, useHasPermission } from "@/protoFleet/store";

interface RolloutRequestOptions {
  signal?: AbortSignal;
}

export interface ListRolloutsOptions extends RolloutRequestOptions {
  states?: RolloutLifecycleState[];
}

export interface GetRolloutOptions extends RolloutRequestOptions {
  rolloutId: string;
}

export interface GetRolloutLaneOptions extends RolloutRequestOptions {
  laneId: string;
}

export interface CreateRolloutMemberInput {
  deviceIdentifier: string;
  sourceSnapshot?: JsonObject;
  targetSnapshot?: JsonObject;
  revertSnapshot?: JsonObject;
}

export interface CreateRolloutBatchInput {
  label: string;
  members: CreateRolloutMemberInput[];
}

export interface CreateRolloutInput extends RolloutRequestOptions {
  name: string;
  strategyKey: string;
  batches: CreateRolloutBatchInput[];
  sourceChannelId?: bigint;
  targetChannelId?: bigint;
  sourceReleaseSetId?: bigint;
  targetReleaseSetId?: bigint;
  sourceSnapshot?: JsonObject;
  targetSnapshot?: JsonObject;
  revertSnapshot?: JsonObject;
  idempotencyKey: string;
  reason: string;
}

export interface CreateRolloutLaneInput extends RolloutRequestOptions {
  label: string;
  description: string;
  firmwareFileIds: string[];
  deviceIdentifiers: string[];
  idempotencyKey: string;
}

export interface StartRolloutLaneInput extends RolloutRequestOptions {
  laneId: string;
  name: string;
  firmwareFileIds: string[];
  batches: CreateRolloutBatchInput[];
  idempotencyKey: string;
  reason: string;
}

export interface StartRolloutLaneResult {
  lane: RolloutLane;
  rollout: RolloutRecord;
}

export interface RolloutControlInput extends RolloutRequestOptions {
  rolloutId: string;
  expectedRevision: bigint;
  idempotencyKey: string;
  reason: string;
}

export interface AdmitRolloutInput extends RolloutControlInput {
  batchId: bigint;
}

export interface CompleteRolloutInput extends RolloutControlInput {
  withFailures?: boolean;
}

export interface RolloutPermissions {
  canReadChannels: boolean;
  canManageChannels: boolean;
  canRead: boolean;
  canManage: boolean;
  canControl: boolean;
}

export interface UseRolloutApiResult {
  lane: RolloutLane | null;
  lanes: RolloutLane[];
  rollout: RolloutRecord | null;
  rollouts: RolloutRecord[];
  isLoading: boolean;
  isMutating: boolean;
  loadError: string | null;
  mutationError: string | null;
  permissions: RolloutPermissions;
  listRolloutLanes: (options?: RolloutRequestOptions) => Promise<RolloutLane[]>;
  getRolloutLane: (options: GetRolloutLaneOptions) => Promise<RolloutLane>;
  createRolloutLane: (input: CreateRolloutLaneInput) => Promise<RolloutLane>;
  startRolloutLane: (input: StartRolloutLaneInput) => Promise<StartRolloutLaneResult>;
  listRollouts: (options?: ListRolloutsOptions) => Promise<RolloutRecord[]>;
  getRollout: (options: GetRolloutOptions) => Promise<RolloutRecord>;
  createRollout: (input: CreateRolloutInput) => Promise<RolloutRecord>;
  admitRollout: (input: AdmitRolloutInput) => Promise<RolloutRecord>;
  continueRollout: (input: RolloutControlInput) => Promise<RolloutRecord>;
  pauseRollout: (input: RolloutControlInput) => Promise<RolloutRecord>;
  resumeRollout: (input: RolloutControlInput) => Promise<RolloutRecord>;
  abortRollout: (input: RolloutControlInput) => Promise<RolloutRecord>;
  revertRollout: (input: RolloutControlInput) => Promise<RolloutRecord>;
  completeRollout: (input: CompleteRolloutInput) => Promise<RolloutRecord>;
}

type RolloutMutationResponse = { rollout?: ProtoRollout };
type RolloutControlOperation = "continue" | "pause" | "resume" | "abort" | "revert" | "complete";

function rpcOptions(signal?: AbortSignal): { signal: AbortSignal } | undefined {
  return signal ? { signal } : undefined;
}

function newestRollout(existing: RolloutRecord | undefined, incoming: RolloutRecord): RolloutRecord {
  return existing && existing.revision > incoming.revision ? existing : incoming;
}

function createRolloutRequest(input: CreateRolloutInput) {
  return create(CreateRolloutRequestSchema, {
    name: input.name,
    strategyKey: input.strategyKey,
    batches: input.batches,
    sourceChannelId: input.sourceChannelId,
    targetChannelId: input.targetChannelId,
    sourceReleaseSetId: input.sourceReleaseSetId,
    targetReleaseSetId: input.targetReleaseSetId,
    sourceSnapshot: input.sourceSnapshot,
    targetSnapshot: input.targetSnapshot,
    revertSnapshot: input.revertSnapshot,
    idempotencyKey: input.idempotencyKey,
    reason: input.reason,
  });
}

async function hydrateRolloutLane(
  lane: ProtoRolloutLane,
  signal?: AbortSignal,
  includeMembers = false,
): Promise<RolloutLane> {
  assertNotAborted(signal);
  if (lane.currentChannelId <= 0n) {
    return mapRolloutLane(lane);
  }

  const deviceSetResponse = await deviceSetClient.getDeviceSet(
    { deviceSetId: lane.currentChannelId },
    rpcOptions(signal),
  );
  assertNotAborted(signal);
  const deviceSet = deviceSetResponse.deviceSet;
  if (!deviceSet || deviceSet.typeDetails.case !== "channelInfo") {
    throw new Error("Rollout lane current release is unavailable.");
  }
  const releaseTargets: RolloutLaneReleaseTarget[] = deviceSet.typeDetails.value.releaseTargets.map((target) => ({
    firmwareFileId: target.firmwareFileId,
    targetManufacturer: target.targetManufacturer,
    targetModel: target.targetModel,
    firmwareVersion: target.firmwareVersion,
    sha256: target.sha256,
  }));
  const memberIdentifiers: string[] = [];
  if (includeMembers) {
    let pageToken = "";
    do {
      const response = await deviceSetClient.listDeviceSetMembers(
        {
          deviceSetId: lane.currentChannelId,
          pageSize: 250,
          pageToken,
        },
        rpcOptions(signal),
      );
      assertNotAborted(signal);
      memberIdentifiers.push(...response.members.map((member) => member.deviceIdentifier));
      pageToken = response.nextPageToken;
    } while (pageToken);
  }

  return mapRolloutLane(lane, {
    memberCount: deviceSet.deviceCount,
    memberIdentifiers,
    releaseTargets,
  });
}

export function useRolloutApi(): UseRolloutApiResult {
  const { handleAuthErrors } = useAuthErrors();
  const canReadChannels = useHasPermission("channel:read");
  const canManageChannels = useHasPermission("channel:manage");
  const canRead = useHasPermission("rollout:read");
  const canManage = useHasPermission("rollout:manage");
  const canControl = useHasPermission("rollout:control");
  const [lane, setLane] = useState<RolloutLane | null>(null);
  const [lanes, setLanes] = useState<RolloutLane[]>([]);
  const [rollout, setRollout] = useState<RolloutRecord | null>(null);
  const [rollouts, setRollouts] = useState<RolloutRecord[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isMutating, setIsMutating] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const latestListRequestIdRef = useRef(0);
  const activeGetRequestByRolloutRef = useRef(new Map<string, symbol>());
  const latestLaneListRequestIdRef = useRef(0);
  const latestLaneGetRequestIdRef = useRef(0);
  const activeLoadCountRef = useRef(0);
  const activeMutationCountRef = useRef(0);

  const beginLoad = useCallback(() => {
    activeLoadCountRef.current += 1;
    setIsLoading(true);
  }, []);

  const finishLoad = useCallback(() => {
    activeLoadCountRef.current = Math.max(0, activeLoadCountRef.current - 1);
    setIsLoading(activeLoadCountRef.current > 0);
  }, []);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string) => {
      handleAuthErrors({ error });
      return toError(error, fallbackMessage);
    },
    [handleAuthErrors],
  );

  const applyMutationResult = useCallback((nextRollout: RolloutRecord) => {
    setRollout((current) => {
      return current?.id === nextRollout.id ? newestRollout(current, nextRollout) : nextRollout;
    });
    setRollouts((current) => {
      const existingIndex = current.findIndex((item) => item.id === nextRollout.id);
      if (existingIndex === -1) {
        return [nextRollout, ...current];
      }
      const newest = newestRollout(current[existingIndex], nextRollout);
      if (newest === current[existingIndex]) {
        return current;
      }
      return current.map((item, index) => (index === existingIndex ? newest : item));
    });
  }, []);

  const applyLaneResult = useCallback((nextLane: RolloutLane) => {
    setLane(nextLane);
    setLanes((current) => {
      const existing = current.findIndex((item) => item.id === nextLane.id);
      if (existing === -1) {
        return [nextLane, ...current];
      }
      if (current[existing].revision > nextLane.revision) {
        return current;
      }
      return current.map((item, index) => (index === existing ? nextLane : item));
    });
  }, []);

  const listRolloutLanes = useCallback(
    async ({ signal }: RolloutRequestOptions = {}) => {
      assertNotAborted(signal);
      const requestId = ++latestLaneListRequestIdRef.current;
      beginLoad();
      setLoadError(null);
      try {
        const response = await rolloutClient.listRolloutLanes(
          create(ListRolloutLanesRequestSchema),
          rpcOptions(signal),
        );
        const mapped = await Promise.all(response.lanes.map((item) => hydrateRolloutLane(item, signal)));
        assertNotAborted(signal);
        if (requestId === latestLaneListRequestIdRef.current) {
          setLanes(mapped);
        }
        return mapped;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, "Failed to load rollout lanes.");
        if (requestId === latestLaneListRequestIdRef.current) {
          if (isAuthOrPermissionError(error)) {
            setLane(null);
            setLanes([]);
          }
          setLoadError(resolvedError.message);
        }
        throw resolvedError;
      } finally {
        finishLoad();
      }
    },
    [beginLoad, finishLoad, handleFailure],
  );

  const getRolloutLane = useCallback(
    async ({ laneId, signal }: GetRolloutLaneOptions) => {
      assertNotAborted(signal);
      const requestId = ++latestLaneGetRequestIdRef.current;
      beginLoad();
      setLoadError(null);
      try {
        const response = await rolloutClient.getRolloutLane(
          create(GetRolloutLaneRequestSchema, { laneId }),
          rpcOptions(signal),
        );
        if (!response.lane) {
          throw new Error("Rollout lane response was missing a lane.");
        }
        const mapped = await hydrateRolloutLane(response.lane, signal, true);
        assertNotAborted(signal);
        if (requestId === latestLaneGetRequestIdRef.current) {
          applyLaneResult(mapped);
        }
        return mapped;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, "Failed to load rollout lane.");
        if (requestId === latestLaneGetRequestIdRef.current) {
          setLoadError(resolvedError.message);
        }
        throw resolvedError;
      } finally {
        finishLoad();
      }
    },
    [applyLaneResult, beginLoad, finishLoad, handleFailure],
  );

  const listRollouts = useCallback(
    async ({ states = [], signal }: ListRolloutsOptions = {}) => {
      assertNotAborted(signal);
      const requestId = ++latestListRequestIdRef.current;
      beginLoad();
      setLoadError(null);

      try {
        const response = await rolloutClient.listRollouts(
          create(ListRolloutsRequestSchema, {
            states: states.map(mapRolloutStateToProto).filter((state) => state !== ProtoRolloutState.UNSPECIFIED),
          }),
          rpcOptions(signal),
        );
        assertNotAborted(signal);
        const mappedRollouts = response.rollouts.map((item) => mapRollout(item));
        if (requestId === latestListRequestIdRef.current) {
          setRollouts((current) => {
            const currentById = new Map(current.map((item) => [item.id, item]));
            const listedIds = new Set(mappedRollouts.map((item) => item.id));
            return [
              ...mappedRollouts.map((item) => newestRollout(currentById.get(item.id), item)),
              ...current.filter((item) => !listedIds.has(item.id)),
            ];
          });
        }
        return mappedRollouts;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, "Failed to load rollouts.");
        if (requestId === latestListRequestIdRef.current) {
          if (isAuthOrPermissionError(error)) {
            setRollout(null);
            setRollouts([]);
          }
          setLoadError(resolvedError.message);
        }
        throw resolvedError;
      } finally {
        finishLoad();
      }
    },
    [beginLoad, finishLoad, handleFailure],
  );

  const getRollout = useCallback(
    async ({ rolloutId, signal }: GetRolloutOptions) => {
      assertNotAborted(signal);
      const requestToken = Symbol(rolloutId);
      activeGetRequestByRolloutRef.current.set(rolloutId, requestToken);
      beginLoad();
      setLoadError(null);

      try {
        const response = await rolloutClient.getRollout(
          create(GetRolloutRequestSchema, { rolloutId }),
          rpcOptions(signal),
        );
        assertNotAborted(signal);
        if (!response.rollout) {
          throw new Error("Rollout response was missing a rollout.");
        }
        const mappedRollout = mapRollout(response.rollout);
        if (requestToken === activeGetRequestByRolloutRef.current.get(rolloutId)) {
          applyMutationResult(mappedRollout);
        }
        return mappedRollout;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, "Failed to load rollout.");
        if (requestToken === activeGetRequestByRolloutRef.current.get(rolloutId)) {
          if (isAuthOrPermissionError(error)) {
            setRollout(null);
            setRollouts([]);
          }
          setLoadError(resolvedError.message);
        }
        throw resolvedError;
      } finally {
        if (requestToken === activeGetRequestByRolloutRef.current.get(rolloutId)) {
          activeGetRequestByRolloutRef.current.delete(rolloutId);
        }
        finishLoad();
      }
    },
    [applyMutationResult, beginLoad, finishLoad, handleFailure],
  );

  const executeMutation = useCallback(
    async <T>(operation: () => Promise<T>, signal: AbortSignal | undefined, fallbackMessage: string): Promise<T> => {
      assertNotAborted(signal);
      activeMutationCountRef.current += 1;
      setIsMutating(true);
      setMutationError(null);

      try {
        return await operation();
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, fallbackMessage);
        setMutationError(resolvedError.message);
        throw resolvedError;
      } finally {
        activeMutationCountRef.current = Math.max(0, activeMutationCountRef.current - 1);
        setIsMutating(activeMutationCountRef.current > 0);
      }
    },
    [handleFailure],
  );

  const runRolloutMutation = useCallback(
    (request: () => Promise<RolloutMutationResponse>, signal: AbortSignal | undefined, fallbackMessage: string) =>
      executeMutation(
        async () => {
          const response = await request();
          assertNotAborted(signal);
          if (!response.rollout) {
            throw new Error("Rollout mutation response was missing a rollout.");
          }
          const mappedRollout = mapRollout(response.rollout);
          applyMutationResult(mappedRollout);
          emitRolloutChanged();
          return mappedRollout;
        },
        signal,
        fallbackMessage,
      ),
    [applyMutationResult, executeMutation],
  );

  const createRollout = useCallback(
    (input: CreateRolloutInput) =>
      runRolloutMutation(
        () => rolloutClient.createRollout(createRolloutRequest(input), rpcOptions(input.signal)),
        input.signal,
        "Failed to create rollout.",
      ),
    [runRolloutMutation],
  );

  const runLaneMutation = useCallback(
    <T>(
      request: () => Promise<T>,
      signal: AbortSignal | undefined,
      fallbackMessage: string,
      mapResponse: (response: T) => Promise<RolloutLane>,
    ) =>
      executeMutation(
        async () => {
          const response = await request();
          assertNotAborted(signal);
          const mappedLane = await mapResponse(response);
          assertNotAborted(signal);
          applyLaneResult(mappedLane);
          emitRolloutChanged();
          return { response, lane: mappedLane };
        },
        signal,
        fallbackMessage,
      ),
    [applyLaneResult, executeMutation],
  );

  const createRolloutLane = useCallback(
    async (input: CreateRolloutLaneInput) => {
      const result = await runLaneMutation(
        () =>
          rolloutClient.createRolloutLane(
            create(CreateRolloutLaneRequestSchema, {
              label: input.label,
              description: input.description,
              firmwareFileIds: input.firmwareFileIds,
              deviceIdentifiers: input.deviceIdentifiers,
              idempotencyKey: input.idempotencyKey,
            }),
            rpcOptions(input.signal),
          ),
        input.signal,
        "Failed to create rollout lane.",
        async (response) => {
          if (!response.lane) {
            throw new Error("Create rollout lane response was missing a lane.");
          }
          return hydrateRolloutLane(response.lane, input.signal);
        },
      );
      return result.lane;
    },
    [runLaneMutation],
  );

  const startRolloutLane = useCallback(
    async (input: StartRolloutLaneInput): Promise<StartRolloutLaneResult> => {
      const result = await runLaneMutation(
        () =>
          rolloutClient.startRolloutLane(
            create(StartRolloutLaneRequestSchema, {
              laneId: input.laneId,
              name: input.name,
              firmwareFileIds: input.firmwareFileIds,
              batches: input.batches,
              idempotencyKey: input.idempotencyKey,
              reason: input.reason,
            }),
            rpcOptions(input.signal),
          ),
        input.signal,
        "Failed to start rollout lane.",
        async (response) => {
          if (!response.lane) {
            throw new Error("Start rollout lane response was missing a lane.");
          }
          return hydrateRolloutLane(response.lane, input.signal);
        },
      );
      if (!result.response.rollout) {
        throw new Error("Start rollout lane response was missing a rollout.");
      }
      const mappedRollout = mapRollout(result.response.rollout);
      applyMutationResult(mappedRollout);
      return { lane: result.lane, rollout: mappedRollout };
    },
    [applyMutationResult, runLaneMutation],
  );

  const admitRollout = useCallback(
    (input: AdmitRolloutInput) =>
      runRolloutMutation(
        () =>
          rolloutClient.admitRollout(
            create(AdmitRolloutRequestSchema, {
              rolloutId: input.rolloutId,
              batchId: input.batchId,
              expectedRevision: input.expectedRevision,
              idempotencyKey: input.idempotencyKey,
              reason: input.reason,
            }),
            rpcOptions(input.signal),
          ),
        input.signal,
        "Failed to admit rollout batch.",
      ),
    [runRolloutMutation],
  );

  const runControl = useCallback(
    (operation: RolloutControlOperation, input: CompleteRolloutInput) =>
      runRolloutMutation(
        () => {
          const request = {
            rolloutId: input.rolloutId,
            expectedRevision: input.expectedRevision,
            idempotencyKey: input.idempotencyKey,
            reason: input.reason,
          };
          const options = rpcOptions(input.signal);
          switch (operation) {
            case "continue":
              return rolloutClient.continueRollout(create(ContinueRolloutRequestSchema, request), options);
            case "pause":
              return rolloutClient.pauseRollout(create(PauseRolloutRequestSchema, request), options);
            case "resume":
              return rolloutClient.resumeRollout(create(ResumeRolloutRequestSchema, request), options);
            case "abort":
              return rolloutClient.abortRollout(create(AbortRolloutRequestSchema, request), options);
            case "revert":
              return rolloutClient.revertRollout(create(RevertRolloutRequestSchema, request), options);
            case "complete":
              return rolloutClient.completeRollout(
                create(CompleteRolloutRequestSchema, {
                  ...request,
                  withFailures: input.withFailures ?? false,
                }),
                options,
              );
          }
        },
        input.signal,
        `Failed to ${operation} rollout.`,
      ),
    [runRolloutMutation],
  );

  const continueRollout = useCallback((input: RolloutControlInput) => runControl("continue", input), [runControl]);
  const pauseRollout = useCallback((input: RolloutControlInput) => runControl("pause", input), [runControl]);
  const resumeRollout = useCallback((input: RolloutControlInput) => runControl("resume", input), [runControl]);
  const abortRollout = useCallback((input: RolloutControlInput) => runControl("abort", input), [runControl]);
  const revertRollout = useCallback((input: RolloutControlInput) => runControl("revert", input), [runControl]);
  const completeRollout = useCallback((input: CompleteRolloutInput) => runControl("complete", input), [runControl]);

  const permissions = useMemo(
    () => ({ canReadChannels, canManageChannels, canRead, canManage, canControl }),
    [canControl, canManage, canManageChannels, canRead, canReadChannels],
  );

  return useMemo(
    () => ({
      lane,
      lanes,
      rollout,
      rollouts,
      isLoading,
      isMutating,
      loadError,
      mutationError,
      permissions,
      listRolloutLanes,
      getRolloutLane,
      createRolloutLane,
      startRolloutLane,
      listRollouts,
      getRollout,
      createRollout,
      admitRollout,
      continueRollout,
      pauseRollout,
      resumeRollout,
      abortRollout,
      revertRollout,
      completeRollout,
    }),
    [
      abortRollout,
      admitRollout,
      completeRollout,
      continueRollout,
      createRolloutLane,
      createRollout,
      getRolloutLane,
      getRollout,
      isLoading,
      isMutating,
      lane,
      lanes,
      listRolloutLanes,
      listRollouts,
      loadError,
      mutationError,
      pauseRollout,
      permissions,
      resumeRollout,
      revertRollout,
      rollout,
      rollouts,
      startRolloutLane,
    ],
  );
}
