import { useCallback, useMemo, useRef, useState } from "react";
import { create, type JsonObject } from "@bufbuild/protobuf";

import { rolloutClient } from "@/protoFleet/api/clients";
import {
  AbortRolloutRequestSchema,
  AdmitRolloutRequestSchema,
  CompleteRolloutRequestSchema,
  ContinueRolloutRequestSchema,
  CreateRolloutBatchSchema,
  CreateRolloutMemberSchema,
  CreateRolloutRequestSchema,
  GetRolloutRequestSchema,
  ListRolloutsRequestSchema,
  PauseRolloutRequestSchema,
  type Rollout as ProtoRollout,
  RolloutState as ProtoRolloutState,
  ResumeRolloutRequestSchema,
  RevertRolloutRequestSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { assertNotAborted, isAbortError, isAuthOrPermissionError, toError } from "@/protoFleet/api/requestErrors";
import { emitRolloutChanged } from "@/protoFleet/api/rolloutEvents";
import { mapRollout, mapRolloutStateToProto } from "@/protoFleet/api/rolloutMappers";
import type { RolloutLifecycleState, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
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
  canRead: boolean;
  canManage: boolean;
  canControl: boolean;
}

export interface UseRolloutApiResult {
  rollout: RolloutRecord | null;
  rollouts: RolloutRecord[];
  isLoading: boolean;
  isMutating: boolean;
  loadError: string | null;
  mutationError: string | null;
  permissions: RolloutPermissions;
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

function createRolloutRequest(input: CreateRolloutInput) {
  return create(CreateRolloutRequestSchema, {
    name: input.name,
    strategyKey: input.strategyKey,
    batches: input.batches.map((batch) =>
      create(CreateRolloutBatchSchema, {
        label: batch.label,
        members: batch.members.map((member) =>
          create(CreateRolloutMemberSchema, {
            deviceIdentifier: member.deviceIdentifier,
            sourceSnapshot: member.sourceSnapshot,
            targetSnapshot: member.targetSnapshot,
            revertSnapshot: member.revertSnapshot,
          }),
        ),
      }),
    ),
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

export function useRolloutApi(): UseRolloutApiResult {
  const { handleAuthErrors } = useAuthErrors();
  const canRead = useHasPermission("rollout:read");
  const canManage = useHasPermission("rollout:manage");
  const canControl = useHasPermission("rollout:control");
  const [rollout, setRollout] = useState<RolloutRecord | null>(null);
  const [rollouts, setRollouts] = useState<RolloutRecord[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [isMutating, setIsMutating] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const latestListRequestIdRef = useRef(0);
  const latestGetRequestIdRef = useRef(0);
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
      if (current?.id === nextRollout.id && current.revision > nextRollout.revision) {
        return current;
      }
      return nextRollout;
    });
    setRollouts((current) => {
      const existingIndex = current.findIndex((item) => item.id === nextRollout.id);
      if (existingIndex === -1) {
        return [nextRollout, ...current];
      }
      if (current[existingIndex].revision > nextRollout.revision) {
        return current;
      }
      return current.map((item, index) => (index === existingIndex ? nextRollout : item));
    });
  }, []);

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
          setRollouts(mappedRollouts);
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
      const requestId = ++latestGetRequestIdRef.current;
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
        if (requestId === latestGetRequestIdRef.current) {
          setRollout(mappedRollout);
        }
        return mappedRollout;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }
        const resolvedError = handleFailure(error, "Failed to load rollout.");
        if (requestId === latestGetRequestIdRef.current) {
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

  const runMutation = useCallback(
    async (
      request: () => Promise<RolloutMutationResponse>,
      signal: AbortSignal | undefined,
      fallbackMessage: string,
    ) => {
      assertNotAborted(signal);
      activeMutationCountRef.current += 1;
      setIsMutating(true);
      setMutationError(null);

      try {
        const response = await request();
        assertNotAborted(signal);
        if (!response.rollout) {
          throw new Error("Rollout mutation response was missing a rollout.");
        }
        const mappedRollout = mapRollout(response.rollout);
        applyMutationResult(mappedRollout);
        emitRolloutChanged();
        return mappedRollout;
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
    [applyMutationResult, handleFailure],
  );

  const createRollout = useCallback(
    (input: CreateRolloutInput) =>
      runMutation(
        () => rolloutClient.createRollout(createRolloutRequest(input), rpcOptions(input.signal)),
        input.signal,
        "Failed to create rollout.",
      ),
    [runMutation],
  );

  const admitRollout = useCallback(
    (input: AdmitRolloutInput) =>
      runMutation(
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
    [runMutation],
  );

  const runControl = useCallback(
    (operation: RolloutControlOperation, input: CompleteRolloutInput) =>
      runMutation(
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
    [runMutation],
  );

  const continueRollout = useCallback((input: RolloutControlInput) => runControl("continue", input), [runControl]);
  const pauseRollout = useCallback((input: RolloutControlInput) => runControl("pause", input), [runControl]);
  const resumeRollout = useCallback((input: RolloutControlInput) => runControl("resume", input), [runControl]);
  const abortRollout = useCallback((input: RolloutControlInput) => runControl("abort", input), [runControl]);
  const revertRollout = useCallback((input: RolloutControlInput) => runControl("revert", input), [runControl]);
  const completeRollout = useCallback((input: CompleteRolloutInput) => runControl("complete", input), [runControl]);

  const permissions = useMemo(() => ({ canRead, canManage, canControl }), [canControl, canManage, canRead]);

  return useMemo(
    () => ({
      rollout,
      rollouts,
      isLoading,
      isMutating,
      loadError,
      mutationError,
      permissions,
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
      createRollout,
      getRollout,
      isLoading,
      isMutating,
      listRollouts,
      loadError,
      mutationError,
      pauseRollout,
      permissions,
      resumeRollout,
      revertRollout,
      rollout,
      rollouts,
    ],
  );
}
