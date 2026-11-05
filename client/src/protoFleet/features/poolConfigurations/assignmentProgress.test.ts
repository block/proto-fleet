import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";
import { Code, ConnectError } from "@connectrpc/connect";

import { findPoolAssignment, trackPoolOperation, usePoolAssignmentProgress } from "./assignmentProgress";
import {
  ConfigurationSchema,
  type PoolConfigurationService,
  ReviewedEquipmentSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const toast = vi.hoisted(() => vi.fn());
vi.mock("@/shared/features/toaster", () => ({
  pushToast: toast,
  STATUSES: { success: "success", error: "error", queued: "queued" },
}));
const configuration = create(ConfigurationSchema, { configurationId: "config", name: "Production", version: 1n });
const equipment = [
  create(ReviewedEquipmentSchema, { kind: "device", targetId: 7n, deviceIdentifier: "miner-a", label: "Same name" }),
  create(ReviewedEquipmentSchema, { kind: "device", targetId: 8n, deviceIdentifier: "miner-b", label: "Same name" }),
];
const target = (index: number, state: string) => ({ ...equipment[index], state, resultCode: "" });
const setup = () => {
  const client = {
    getOperation: vi.fn().mockResolvedValue({ targets: equipment.map((_, i) => target(i, "verified")) }),
  };
  const track = (name = "Production") =>
    trackPoolOperation(
      client as unknown as Client<typeof PoolConfigurationService>,
      "account",
      create(ConfigurationSchema, { ...configuration, name }),
      "operation",
      equipment,
    );
  return { client, track };
};
beforeEach(() => {
  vi.useFakeTimers();
  toast.mockClear();
  usePoolAssignmentProgress.setState({ jobs: [], detailsJobId: undefined });
});
afterEach(() => {
  vi.useRealTimers();
});

describe("accepted pool operation progress", () => {
  it("tracks exact row identities while queued, then reports completion once", async () => {
    const { client, track } = setup();
    client.getOperation.mockResolvedValueOnce({ targets: [target(0, "pending"), target(1, "applying")] });
    const finished = track();
    expect(findPoolAssignment(usePoolAssignmentProgress.getState().jobs, "account", "miner-a")?.phase).toBe("pending");
    expect(findPoolAssignment(usePoolAssignmentProgress.getState().jobs, "other", "miner-a")).toBeUndefined();
    await vi.advanceTimersByTimeAsync(0);
    const job = findPoolAssignment(usePoolAssignmentProgress.getState().jobs, "account", undefined, 8n);
    expect(job?.phase).toBe("pending");
    expect(job?.targets[1].deviceIdentifier).toBe("miner-b");
    expect(job?.operationId).toBe("operation");
    expect(toast).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(3000);
    await finished;
    expect(toast).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ status: "success", message: "Pool configuration applied to 2 selected items." }),
    );
    toast.mock.calls[0][0].actions[0].onClick();
    expect(usePoolAssignmentProgress.getState().detailsJobId).toBe(job?.id);
    expect(client.getOperation).toHaveBeenCalledWith(
      { accountId: "account", operationId: "operation" },
      expect.anything(),
    );
  });

  it("keeps mixed results distinct instead of reporting a queued write as success", async () => {
    const { client, track } = setup();
    client.getOperation.mockResolvedValue({ targets: [target(0, "verified"), target(1, "different")] });
    await track();
    expect(toast).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ status: "error", message: "Pool configuration update: 1 verified, 1 need attention." }),
    );
  });

  it.each([
    { state: "applied_unverified", resultCode: "" },
    { state: "failed", resultCode: "write_uncertain" },
  ])("does not claim verified success for $state / $resultCode", async ({ state, resultCode }) => {
    const { client, track } = setup();
    client.getOperation.mockResolvedValue({ targets: [{ ...target(0, state), resultCode }] });
    await track();
    expect(toast).toHaveBeenCalledWith(
      expect.objectContaining({ status: "error", message: "Pool configuration could not be verified." }),
    );
  });

  it("retries result reads and reports an unavailable accepted operation", async () => {
    const { client, track } = setup();
    client.getOperation.mockRejectedValue(new ConnectError("unavailable", Code.Unavailable));
    const finished = track();
    await vi.advanceTimersByTimeAsync(6000);
    await finished;
    expect(client.getOperation).toHaveBeenCalledTimes(3);
    expect(usePoolAssignmentProgress.getState().jobs[0].phase).toBe("unknown");
    expect(toast).toHaveBeenCalledWith(
      expect.objectContaining({
        status: "error",
        message: "Pool configuration update submitted. Status is unavailable.",
      }),
    );
  });

  it("discards late results after the login session changes", async () => {
    const { client, track } = setup();
    let resolve!: (value: { targets: ReturnType<typeof target>[] }) => void;
    client.getOperation.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const finished = track();
    useFleetStore.setState((state) => ({
      auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
    }));
    resolve({ targets: [target(0, "verified")] });
    await finished;
    expect(usePoolAssignmentProgress.getState().jobs).toEqual([]);
    expect(toast).not.toHaveBeenCalled();
  });

  it("keeps the newest submission visible when an older operation finishes later", async () => {
    const older = setup();
    let resolveOlder!: (value: { targets: ReturnType<typeof target>[] }) => void;
    older.client.getOperation.mockReturnValue(
      new Promise((resolve) => {
        resolveOlder = resolve;
      }),
    );
    const first = older.track();
    await setup().track("Newer");
    resolveOlder({ targets: [target(0, "failed"), target(1, "failed")] });
    await first;
    expect(findPoolAssignment(usePoolAssignmentProgress.getState().jobs, "account", "miner-a")?.name).toBe("Newer");
  });
});
