import { beforeEach, describe, expect, it, vi } from "vitest";

import { fetchAllSelectableMinerIds } from "./fetchAllSelectableMinerIds";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";

const mockFetchAllMinerSnapshots = vi.fn();
vi.mock("@/protoFleet/api/fetchAllMinerSnapshots", () => ({
  fetchAllMinerSnapshots: (...args: unknown[]) => mockFetchAllMinerSnapshots(...args),
}));

// Unplaced snapshot: no rack/building/site, so eligible for any target.
const snapshot = (deviceIdentifier: string): MinerStateSnapshot => ({ deviceIdentifier }) as MinerStateSnapshot;

describe("fetchAllSelectableMinerIds", () => {
  beforeEach(() => {
    mockFetchAllMinerSnapshots.mockReset();
  });

  it("resolves select-all with the visible pairing set so non-paired members aren't dropped", async () => {
    // Regression for #777: the rack list fetches the visible set, so the
    // select-all resolver must too — otherwise Save would evict a rack's
    // auth-needed / default-password members.
    mockFetchAllMinerSnapshots.mockResolvedValue({
      "paired-1": snapshot("paired-1"),
      "auth-needed-1": snapshot("auth-needed-1"),
    });

    const ids = await fetchAllSelectableMinerIds({});

    expect(mockFetchAllMinerSnapshots).toHaveBeenCalledWith(
      expect.objectContaining({ pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES }),
    );
    expect(ids).toEqual(["paired-1", "auth-needed-1"]);
  });

  it("preserves the user's active list filter while overriding pairing statuses", async () => {
    mockFetchAllMinerSnapshots.mockResolvedValue({});

    await fetchAllSelectableMinerIds({}, { models: ["S21"] } as never);

    expect(mockFetchAllMinerSnapshots).toHaveBeenCalledWith(
      expect.objectContaining({ models: ["S21"], pairingStatuses: FLEET_VISIBLE_PAIRING_STATUSES }),
    );
  });
});
