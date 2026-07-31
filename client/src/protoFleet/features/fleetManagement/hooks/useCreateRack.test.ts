import { act } from "react";
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useCreateRack } from "./useCreateRack";
import { RackCoolingType, RackOrderIndex } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { type RackFormData } from "@/protoFleet/features/fleetManagement/components/ManageRackModal/types";

const mockSaveRack = vi.fn(({ onSuccess, onFinally }) => {
  onSuccess?.({ id: 7n }, 0);
  onFinally?.();
});

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ saveRack: mockSaveRack }),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const formData = (overrides: Partial<RackFormData> = {}): RackFormData => ({
  label: "R-1",
  zone: "Zone A",
  rows: 4,
  columns: 3,
  orderIndex: RackOrderIndex.BOTTOM_LEFT,
  coolingType: RackCoolingType.AIR,
  ...overrides,
});

describe("useCreateRack", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // An explicit 0 would make site_id/building_id present on RackInfo, which
  // SaveRack's handler reads as placement intent and gates behind site:manage.
  // A rack:manage-only operator has no placement controls, so their create must
  // not carry the fields at all.
  it("omits placement when the operator chose none", async () => {
    const { result } = renderHook(() => useCreateRack({ onCreated: vi.fn() }));

    await act(async () => {
      await result.current.createRack(formData());
    });

    expect(mockSaveRack).toHaveBeenCalledTimes(1);
    expect(mockSaveRack.mock.calls[0][0]).toMatchObject({ siteId: undefined, buildingId: undefined });
  });

  it("sends the placement the operator picked", async () => {
    const { result } = renderHook(() => useCreateRack({ onCreated: vi.fn() }));

    await act(async () => {
      await result.current.createRack(formData({ siteId: 3n, buildingId: 9n }));
    });

    expect(mockSaveRack.mock.calls[0][0]).toMatchObject({ siteId: 3n, buildingId: 9n });
  });
});
