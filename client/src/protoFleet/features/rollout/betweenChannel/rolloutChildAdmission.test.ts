import { beforeEach, describe, expect, it, vi } from "vitest";

import { admitRolloutChild } from "./rolloutChildAdmission";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

const child = {
  id: "child-1",
  revision: 3n,
  manufacturer: "Proto",
  model: "Alpha",
} as RolloutRecord;

describe("admitRolloutChild", () => {
  beforeEach(() => localStorage.clear());

  it("cleans up the deterministic key after admission succeeds", async () => {
    const updateState = vi.fn();
    const onAdmitted = vi.fn();
    const admitted = { ...child, revision: 4n };
    const admit = vi.fn().mockResolvedValue(admitted);

    await admitRolloutChild({
      rollout: child,
      batchId: 7n,
      admissionAttempt: 2,
      keyPrefix: "model-start",
      reason: "Start first batch",
      admit,
      updateState,
      onAdmitted,
    });

    expect(admit).toHaveBeenCalledWith(expect.objectContaining({ idempotencyKey: "model-start:admit:2" }));
    expect(localStorage.getItem("protoFleet.rolloutAdmissionKeyPrefix.child-1")).toBeNull();
    expect(updateState).toHaveBeenLastCalledWith("child-1", { loading: false });
    expect(onAdmitted).toHaveBeenCalledWith(admitted);
  });

  it("preserves the key and records a local error for retry", async () => {
    const updateState = vi.fn();
    await expect(
      admitRolloutChild({
        rollout: child,
        batchId: 7n,
        admissionAttempt: 0,
        keyPrefix: "model-start",
        reason: "Start first batch",
        admit: vi.fn().mockRejectedValue(new Error("conflict")),
        updateState,
        onAdmitted: vi.fn(),
      }),
    ).rejects.toThrow("conflict");

    expect(localStorage.getItem("protoFleet.rolloutAdmissionKeyPrefix.child-1")).toBe("model-start");
    expect(updateState).toHaveBeenLastCalledWith("child-1", {
      loading: false,
      error: "Proto Alpha couldn't start: conflict",
    });
  });
});
