import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "./clients";
import useUpdateWorkerNames from "./useUpdateWorkerNames";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { SortConfigSchema, SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  DeviceSelectorSchema,
  MinerNameConfigSchema,
  NamePropertySchema,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

vi.mock("./clients", () => ({
  fleetManagementClient: {
    updateWorkerNames: vi.fn(),
  },
}));

const mockHandleAuthErrors = vi.fn();

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
}));

describe("useUpdateWorkerNames", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("sends the expected request payload and wraps the sort in an array", async () => {
    vi.mocked(fleetManagementClient.updateWorkerNames).mockResolvedValue({ updatedCount: 1 } as never);

    const deviceSelector = create(DeviceSelectorSchema, {
      selectionType: {
        case: "includeDevices",
        value: create(DeviceIdentifierListSchema, { deviceIdentifiers: ["miner-1", "miner-2"] }),
      },
    });
    const nameConfig = create(MinerNameConfigSchema, {
      properties: [
        create(NamePropertySchema, {
          kind: {
            case: "stringValue",
            value: create(StringPropertySchema, { value: "worker-new" }),
          },
        }),
      ],
      separator: "",
    });
    const sort = create(SortConfigSchema, {
      field: SortField.NAME,
      direction: SortDirection.ASC,
    });

    const { result } = renderHook(() => useUpdateWorkerNames());

    await act(async () => {
      await result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass", sort);
    });

    expect(fleetManagementClient.updateWorkerNames).toHaveBeenCalledWith(
      expect.objectContaining({
        deviceSelector,
        nameConfig,
        sort: [sort],
        userUsername: "fleet-user",
        userPassword: "fleet-pass",
      }),
    );
  });

  it("sends an empty sort array when no sort is provided", async () => {
    vi.mocked(fleetManagementClient.updateWorkerNames).mockResolvedValue({ updatedCount: 1 } as never);

    const deviceSelector = create(DeviceSelectorSchema, {
      selectionType: {
        case: "includeDevices",
        value: create(DeviceIdentifierListSchema, { deviceIdentifiers: ["miner-1"] }),
      },
    });
    const nameConfig = create(MinerNameConfigSchema, {
      separator: "",
    });

    const { result } = renderHook(() => useUpdateWorkerNames());

    await act(async () => {
      await result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass");
    });

    expect(fleetManagementClient.updateWorkerNames).toHaveBeenCalledWith(
      expect.objectContaining({
        sort: [],
      }),
    );
  });

  it("handles auth errors and rethrows the original error", async () => {
    const testError = new Error("request failed");
    vi.mocked(fleetManagementClient.updateWorkerNames).mockRejectedValue(testError);

    const deviceSelector = create(DeviceSelectorSchema, {
      selectionType: {
        case: "includeDevices",
        value: create(DeviceIdentifierListSchema, { deviceIdentifiers: ["miner-1"] }),
      },
    });
    const nameConfig = create(MinerNameConfigSchema, {
      separator: "",
    });

    const { result } = renderHook(() => useUpdateWorkerNames());

    await expect(
      result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass"),
    ).rejects.toThrow(testError);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({
      error: testError,
    });
  });
});
