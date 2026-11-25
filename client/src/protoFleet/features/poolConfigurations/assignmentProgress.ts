import type { Client } from "@connectrpc/connect";
import { Code, ConnectError } from "@connectrpc/connect";
import { create } from "zustand";

import type {
  Configuration,
  GetOperationResponse,
  OperationTarget,
  PoolConfigurationService,
  ReviewedEquipment,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ACCOUNT_READ_TIMEOUT_MS } from "@/protoFleet/features/hostedAccounts/api";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Only accepted operations enter this session-scoped progress store.
// Uncertain confirmations are recovered separately by their review IDs.
// UI progress never contains credentials. Accepted operations
// and explicit verification survive reloads in the server's assignment records.
export type PoolAssignmentProgress = {
  id: string;
  accountId: string;
  name: string;
  deviceIdentifiers: string[];
  deviceIds: string[];
  operationId: string;
  acceptedAt: number;
  installationIds: string[];
  phase: "pending" | "done" | "unknown";
  targets: OperationTarget[];
};

export type PoolDetailsSelection = {
  accountId: string;
  deviceIdentifiers?: string[];
  deviceIds?: bigint[];
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  jobId?: string;
};
export const usePoolAssignmentProgress = create<{
  jobs: PoolAssignmentProgress[];
  assignmentsRevision: number;
  detailsJobId?: string;
  detailsSelection?: PoolDetailsSelection;
}>(() => ({
  jobs: [],
  assignmentsRevision: 0,
}));
useFleetStore.subscribe((state, previous) => {
  if (state.auth.sessionGeneration !== previous.auth.sessionGeneration)
    usePoolAssignmentProgress.setState({
      jobs: [],
      assignmentsRevision: 0,
      detailsJobId: undefined,
      detailsSelection: undefined,
    });
});
export const refreshPoolAssignments = () =>
  usePoolAssignmentProgress.setState((state) => ({ assignmentsRevision: state.assignmentsRevision + 1 }));

const update = (id: string, patch: Partial<PoolAssignmentProgress>) =>
  usePoolAssignmentProgress.setState(({ jobs }) => ({
    jobs: jobs.map((job) => (job.id === id ? { ...job, ...patch } : job)),
  }));
export const findPoolAssignment = (
  jobs: PoolAssignmentProgress[],
  accountId: string,
  deviceIdentifier?: string,
  deviceId?: bigint,
) => {
  for (let i = jobs.length - 1; i >= 0; i--) {
    const job = jobs[i];
    if (
      job.accountId === accountId &&
      ((deviceIdentifier && job.deviceIdentifiers.includes(deviceIdentifier)) ||
        (deviceId !== undefined && job.deviceIds.includes(String(deviceId))))
    )
      return job;
  }
  return undefined;
};
const terminal = new Set([
  "verified",
  "applied_unverified",
  "different",
  "failed",
  "superseded",
  "completed",
  "uncertain",
]);
const pause = () => new Promise<void>((resolve) => setTimeout(resolve, 3000));
type PoolClient = Client<typeof PoolConfigurationService>;
const notify = (id: string, message: string, status: (typeof STATUSES)[keyof typeof STATUSES]) =>
  pushToast({
    message,
    status,
    actions: [
      {
        label: "View details",
        onClick: () => usePoolAssignmentProgress.setState({ detailsJobId: id, detailsSelection: undefined }),
      },
    ],
  });

// Shared operation polling supports both background assignment feedback and a
// bounded modal lifetime. Ending observation never cancels accepted server work.
export async function* pollPoolOperation(
  client: PoolClient,
  accountId: string,
  operationId: string,
  current: () => boolean,
  maxPolls = Infinity,
): AsyncGenerator<GetOperationResponse> {
  let failures = 0;
  for (let poll = 0; poll < maxPolls && current(); poll++) {
    try {
      const result = await client.getOperation({ accountId, operationId }, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS });
      if (!current()) return;
      failures = 0;
      yield result;
      if (result.targets.length && result.targets.every((target) => terminal.has(target.state))) return;
    } catch (error) {
      if (!current()) return;
      if (
        ++failures >= 3 ||
        [Code.PermissionDenied, Code.Unauthenticated, Code.NotFound].includes(ConnectError.from(error).code)
      )
        throw error;
    }
    if (poll + 1 < maxPolls) await pause();
  }
}

async function monitorOperation(
  client: PoolClient,
  accountId: string,
  id: string,
  operationId: string,
  current: () => boolean,
) {
  try {
    for await (const result of pollPoolOperation(client, accountId, operationId, current)) {
      const complete = result.targets.length > 0 && result.targets.every((target) => terminal.has(target.state));
      update(id, { targets: result.targets, phase: complete ? "done" : "pending" });
      if (complete) {
        const targets = result.targets.filter((target) => target.state !== "superseded");
        if (!targets.length) return;
        const verified = targets.filter((target) => target.state === "verified").length;
        const attention = targets.length - verified;
        const uncertain = targets.some(
          (target) => target.resultCode === "write_uncertain" || target.state === "applied_unverified",
        );
        let message: string;
        if (targets.length === 1) {
          message = verified
            ? "Pool configuration applied."
            : uncertain
              ? "Pool configuration could not be verified."
              : targets[0].state === "different"
                ? "Pool settings do not match the configuration."
                : "Pool configuration update failed.";
        } else if (attention) {
          message = `Pool configuration update: ${verified ? `${verified} verified, ` : ""}${attention} need attention.`;
        } else {
          message = `Pool configuration applied to ${verified} ${targets.every((target) => target.kind === "hashboard_installation") ? "hashboards" : "selected items"}.`;
        }
        notify(id, message, attention ? STATUSES.error : STATUSES.success);
        return;
      }
    }
  } catch {
    if (!current()) return;
    update(id, { phase: "unknown" });
    notify(id, "Pool configuration update submitted. Status is unavailable.", STATUSES.error);
  }
}

const equipmentSelection = (equipment: ReviewedEquipment[]) => ({
  installationIds: equipment.filter((t) => t.kind === "hashboard_installation").map((t) => String(t.targetId)),
  deviceIdentifiers: equipment.filter((t) => t.kind === "device").map((t) => t.deviceIdentifier),
  deviceIds: equipment.filter((t) => t.kind === "device").map((t) => String(t.targetId)),
});

// Edits and retries use the same completion feedback as initial assignments.
export async function trackPoolOperation(
  client: PoolClient,
  accountId: string,
  configuration: Configuration,
  operationId: string,
  equipment: ReviewedEquipment[],
  signal?: AbortSignal,
) {
  const generation = useFleetStore.getState().auth.sessionGeneration;
  const current = () => !signal?.aborted && useFleetStore.getState().auth.sessionGeneration === generation;
  if (!current()) return;
  const id = crypto.randomUUID();
  usePoolAssignmentProgress.setState(({ jobs }) => ({
    jobs: [
      ...jobs,
      {
        id,
        accountId,
        name: configuration.name,
        acceptedAt: Date.now(),
        operationId,
        ...equipmentSelection(equipment),
        phase: "pending",
        targets: [],
      },
    ],
  }));
  await monitorOperation(client, accountId, id, operationId, current);
}
