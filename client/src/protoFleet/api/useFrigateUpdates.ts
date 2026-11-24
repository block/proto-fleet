import { useCallback, useEffect, useMemo, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { frigateUpdateClient } from "@/protoFleet/api/clients";
import {
  FrigateUpdateState,
  type GetSupportResponse,
  type UpdateJob,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
import { usePoll } from "@/shared/hooks/usePoll";

export const FRIGATE_UPDATE_ACTIVE_POLL_MS = 5_000;
export const FRIGATE_UPDATE_IDLE_POLL_MS = 30_000;
const HISTORY_PAGE_SIZE = 10;
const REQUEST_TIMEOUT_MS = 30_000;

const ACTIVE_STATES = new Set<FrigateUpdateState>([
  FrigateUpdateState.STAGING,
  FrigateUpdateState.STAGED,
  FrigateUpdateState.STARTING,
  FrigateUpdateState.RUNNING,
]);

/** A job that has not reached a final state. A failed retryable job is not active. */
export function isFrigateUpdateActive(job: UpdateJob | undefined): boolean {
  return !!job && ACTIVE_STATES.has(job.state);
}

function usePageVisible(): boolean {
  const [visible, setVisible] = useState(() => document.visibilityState !== "hidden");
  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  return visible;
}

/**
 * Polls one container's update jobs, newest first. Polls every 5 s while the
 * newest job is active and every 30 s otherwise, and pauses while the page is
 * hidden.
 */
export function useFrigateUpdates(deviceIdentifier: string, enabled: boolean) {
  const { handleAuthErrors } = useAuthErrors();
  const visible = usePageVisible();
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ key: string; jobs?: UpdateJob[]; error?: string }>();
  const latest = result?.key === deviceIdentifier ? result.jobs?.[0] : undefined;
  const active = isFrigateUpdateActive(latest);

  usePoll({
    enabled: enabled && !!deviceIdentifier && visible,
    params: `${deviceIdentifier}:${revision}`,
    poll: true,
    pollIntervalMs: active ? FRIGATE_UPDATE_ACTIVE_POLL_MS : FRIGATE_UPDATE_IDLE_POLL_MS,
    fetchData: async (signal) => {
      try {
        const response = await frigateUpdateClient.listUpdates(
          { deviceIdentifier, pageSize: HISTORY_PAGE_SIZE },
          { signal, timeoutMs: REQUEST_TIMEOUT_MS },
        );
        if (!signal.aborted) setResult({ key: deviceIdentifier, jobs: response.jobs });
      } catch (error) {
        if (signal.aborted) return;
        // The flat permission list can't tell which site grants the permission, so
        // a container outside it is simply not updatable by this user.
        if (ConnectError.from(error).code === Code.PermissionDenied) {
          setResult({ key: deviceIdentifier, jobs: [] });
          return;
        }
        handleAuthErrors({
          error,
          // Keep the last jobs on screen: a failed read must not hide a running update.
          onError: () =>
            setResult((previous) => ({
              key: deviceIdentifier,
              jobs: previous?.key === deviceIdentifier ? previous.jobs : undefined,
              error: getErrorMessage(error, "Couldn't load controller software updates."),
            })),
        });
      }
    },
  });

  const refresh = useMountedCallback(() => setRevision((value) => value + 1));
  const current = result?.key === deviceIdentifier ? result : undefined;
  return useMemo(
    () => ({
      jobs: current?.jobs ?? [],
      error: current?.error,
      loading: enabled && !current,
      /** The newest job; it is the one that blocks a new update when it is not final. */
      latest: current?.jobs?.[0],
      refresh,
    }),
    [current, enabled, refresh],
  );
}

export interface StartFrigateUpdateInput {
  deviceIdentifier: string;
  packageId: string;
  idempotencyKey: string;
  stageOnly?: boolean;
}

/**
 * One-shot calls. The caller owns the idempotency key for each user action so a
 * resubmission of the same action reuses it.
 */
export function useFrigateUpdateActions() {
  const getSupport = useCallback(
    async (deviceIdentifier: string, packageId?: string, signal?: AbortSignal): Promise<GetSupportResponse> =>
      frigateUpdateClient.getSupport(
        { deviceIdentifier, packageId: packageId ?? "" },
        { signal, timeoutMs: REQUEST_TIMEOUT_MS },
      ),
    [],
  );

  const startUpdate = useCallback(
    async ({ deviceIdentifier, packageId, idempotencyKey, stageOnly = false }: StartFrigateUpdateInput) => {
      const response = await frigateUpdateClient.startUpdate(
        { deviceIdentifier, packageId, stageOnly, idempotencyKey },
        { timeoutMs: REQUEST_TIMEOUT_MS },
      );
      return response.job;
    },
    [],
  );

  const retryUpdate = useCallback(async (jobId: string, idempotencyKey: string) => {
    const response = await frigateUpdateClient.retryUpdate(
      { jobId, idempotencyKey },
      { timeoutMs: REQUEST_TIMEOUT_MS },
    );
    return response.job;
  }, []);

  const cancelUpdate = useCallback(async (jobId: string, idempotencyKey: string) => {
    const response = await frigateUpdateClient.cancelUpdate(
      { jobId, idempotencyKey },
      { timeoutMs: REQUEST_TIMEOUT_MS },
    );
    return response.job;
  }, []);

  return useMemo(
    () => ({ getSupport, startUpdate, retryUpdate, cancelUpdate }),
    [getSupport, startUpdate, retryUpdate, cancelUpdate],
  );
}
