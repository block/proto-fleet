import { describe, expect, it, vi } from "vitest";

import { admitRolloutChild } from "./rolloutChildAdmission";
import type { RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

const child = {
  id: "child-1",
  revision: 3n,
  manufacturer: "Proto",
  model: "Alpha",
} as RolloutRecord;

describe("admitRolloutChild", () => {
  it("derives the admission key only from durable child and batch state", async () => {
    const updateState = vi.fn();
    const onAdmitted = vi.fn();
    const admitted = { ...child, revision: 4n };
    const admit = vi.fn().mockResolvedValue(admitted);

    await admitRolloutChild({
      rollout: child,
      batchId: 7n,
      admissionAttempt: 2,
      reason: "Start first batch",
      admit,
      updateState,
      onAdmitted,
    });

    expect(admit).toHaveBeenCalledWith(expect.objectContaining({ idempotencyKey: "child-1:7:admit:2" }));
    expect(updateState).toHaveBeenLastCalledWith("child-1", { loading: false });
    expect(onAdmitted).toHaveBeenCalledWith(admitted);
  });

  it("reconstructs the same key after all browser state is cleared", async () => {
    const updateState = vi.fn();
    const admit = vi
      .fn()
      .mockRejectedValueOnce(new Error("outcome unknown"))
      .mockResolvedValueOnce({ ...child, revision: 4n });
    const request = {
      rollout: child,
      batchId: 7n,
      admissionAttempt: 0,
      reason: "Start first batch",
      admit,
      updateState,
      onAdmitted: vi.fn(),
    };

    await expect(admitRolloutChild(request)).rejects.toThrow("outcome unknown");
    localStorage.clear();
    sessionStorage.clear();
    await admitRolloutChild(request);

    expect(admit).toHaveBeenNthCalledWith(1, expect.objectContaining({ idempotencyKey: "child-1:7:admit:0" }));
    expect(admit).toHaveBeenNthCalledWith(2, expect.objectContaining({ idempotencyKey: "child-1:7:admit:0" }));
  });
});
