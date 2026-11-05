import { type ReactNode, useCallback, useEffect, useMemo } from "react";

import { usePoolConfigurationClient, usePoolConfigurationPermissions } from "./api";
import { PoolAssignmentsContext } from "./assignmentContext";
import { usePoolAssignmentProgress } from "./assignmentProgress";
import { poolTargetKey, useDevicePoolReadings } from "./useDevicePoolReadings";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { useFleetStore } from "@/protoFleet/store";

// Fetch once per visible table page, never once per row. Account/session changes
// discard the previous scope's response through useAccountQuery's load identity.
export const PoolAssignments = ({
  deviceIdentifiers = [],
  deviceIds = [],
  moduleIds = [],
  hashboardIds = [],
  enabled = true,
  children,
}: {
  deviceIdentifiers?: string[];
  deviceIds?: bigint[];
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  enabled?: boolean;
  children: ReactNode;
}) => {
  const { client, accountId, readOnly } = usePoolConfigurationClient();
  const { read, change } = usePoolConfigurationPermissions();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const selection = JSON.stringify({
    deviceIdentifiers: [...new Set(deviceIdentifiers)].sort(),
    deviceIds: [...new Set(deviceIds.map(String))].sort(),
    moduleIds: [...new Set(moduleIds.map(String))].sort(),
    hashboardIds: [...new Set(hashboardIds.map(String))].sort(),
  });
  const load = useCallback(
    async (signal: AbortSignal) => {
      const requestedAt = Date.now();
      const selected = JSON.parse(selection) as {
        deviceIdentifiers: string[];
        deviceIds: string[];
        moduleIds: string[];
        hashboardIds: string[];
      };
      const options = { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const responses = await Promise.all([
        ...(selected.deviceIdentifiers.length + selected.deviceIds.length
          ? [
              client.getAssignments(
                { accountId, deviceIdentifiers: selected.deviceIdentifiers, deviceIds: selected.deviceIds.map(BigInt) },
                options,
              ),
            ]
          : []),
        ...(selected.moduleIds.length + selected.hashboardIds.length
          ? [
              client.getAssignments(
                {
                  accountId,
                  boards: {
                    moduleIds: selected.moduleIds.map(BigInt),
                    hashboardIds: selected.hashboardIds.map(BigInt),
                  },
                },
                options,
              ),
            ]
          : []),
      ]);
      const response = { assignments: responses.flatMap((result) => result.assignments) };
      return { response, generation, requestedAt };
    },
    [client, accountId, selection, generation],
  );
  const query = useAccountQuery(
    load,
    enabled && read && deviceIdentifiers.length + deviceIds.length + moduleIds.length + hashboardIds.length > 0,
    true,
  );
  const reported = useDevicePoolReadings(
    client,
    accountId,
    query.data?.response.assignments ?? [],
    enabled && change && !readOnly,
  );
  const { poll: pollAssignments, refresh: refreshAssignments } = query;
  const { poll: pollReadings } = reported;
  const poll = useCallback(() => {
    pollAssignments();
    pollReadings();
  }, [pollAssignments, pollReadings]);
  const refresh = useCallback(() => {
    refreshAssignments();
    pollReadings();
  }, [refreshAssignments, pollReadings]);
  const progress = usePoolAssignmentProgress((state) => state.jobs);
  useEffect(() => {
    poll();
  }, [progress, poll]);
  useEffect(() => {
    if (!enabled || !read) return;
    const timer = window.setInterval(poll, POLL_INTERVAL_MS);
    window.addEventListener("focus", poll);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", poll);
    };
  }, [poll, enabled, read]);
  const assignments = useMemo(
    () =>
      query.data?.response.assignments.map((a) => {
        const check = !a.configurationId && reported.readings[poolTargetKey(a)];
        return check ? { ...a, state: check.state, resultCode: check.resultCode } : a;
      }) ?? [],
    [query.data, reported.readings],
  );
  const value = useMemo(
    () => ({
      byIdentifier: new Map(
        assignments
          .filter((assignment) => !assignment.installationId)
          .map((assignment) => [assignment.deviceIdentifier, assignment]),
      ),
      byId: new Map(
        assignments
          .filter((assignment) => !assignment.installationId)
          .map((assignment) => [String(assignment.deviceId), assignment]),
      ),
      boards: assignments.filter((assignment) => assignment.installationId) ?? [],
      readable: enabled && read,
      refresh,
      loadedAt: query.data?.requestedAt,
      loading: query.loading,
      error: query.error,
    }),
    [assignments, query.data, query.loading, query.error, refresh, enabled, read],
  );
  return <PoolAssignmentsContext.Provider value={value}>{children}</PoolAssignmentsContext.Provider>;
};
