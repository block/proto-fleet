import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  type InfrastructureDevice,
  InfrastructureDeviceSchema,
} from "@/protoFleet/api/generated/infrastructure/v1/infrastructure_pb";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";

const {
  mockCreateInfrastructureDevice,
  mockDeleteInfrastructureDevice,
  mockHandleAuthErrors,
  mockListInfrastructureDevices,
  mockUpdateInfrastructureDevice,
} = vi.hoisted(() => ({
  mockCreateInfrastructureDevice: vi.fn(),
  mockDeleteInfrastructureDevice: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListInfrastructureDevices: vi.fn(),
  mockUpdateInfrastructureDevice: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  infrastructureClient: {
    createInfrastructureDevice: mockCreateInfrastructureDevice,
    deleteInfrastructureDevice: mockDeleteInfrastructureDevice,
    listInfrastructureDevices: mockListInfrastructureDevices,
    updateInfrastructureDevice: mockUpdateInfrastructureDevice,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const driverConfig = JSON.stringify({
  endpoint: "10.12.1.21",
  port: 502,
  unit_id: 17,
  register_address: 2001,
  write_mode: "coil",
});

function apiDevice(overrides: Partial<InfrastructureDevice> = {}): InfrastructureDevice {
  const device = create(InfrastructureDeviceSchema, {
    id: 101n,
    siteId: 8n,
    siteLabel: "Austin",
    buildingName: "Building 1",
    name: "Roof exhaust",
    deviceKind: "fan_group",
    fanCount: 12,
    enabled: true,
    driverType: "modbus_tcp",
    driverConfig,
  });

  return Object.assign(device, overrides);
}

describe("useInfrastructureDevices", () => {
  beforeEach(() => {
    mockCreateInfrastructureDevice.mockReset();
    mockDeleteInfrastructureDevice.mockReset();
    mockHandleAuthErrors.mockReset();
    mockListInfrastructureDevices.mockReset();
    mockUpdateInfrastructureDevice.mockReset();
  });

  it("fetches on mount and maps devices to the UI shape", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });

    const { result } = renderHook(() => useInfrastructureDevices());

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.devices).toEqual([
      {
        id: "101",
        siteId: "8",
        siteName: "Austin",
        buildingName: "Building 1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        enabled: true,
        driverType: "modbus_tcp",
        driverConfig,
      },
    ]);
    expect(result.current.loadError).toBeNull();
  });

  it("falls back to a generated site name when the site label is unresolved", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice({ siteLabel: "" })] });

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    expect(result.current.devices[0].siteName).toBe("Site 8");
  });

  it("does not fetch when disabled", () => {
    renderHook(() => useInfrastructureDevices(false));

    expect(mockListInfrastructureDevices).not.toHaveBeenCalled();
  });

  it("records the load error and rethrows on list failure", async () => {
    mockListInfrastructureDevices.mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await expect(result.current.listDevices()).rejects.toThrow("boom");
    });

    expect(result.current.loadError).toBe("boom");
    expect(mockHandleAuthErrors).toHaveBeenCalled();
  });

  it("clears a previous load error when a retry starts", async () => {
    mockListInfrastructureDevices.mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await expect(result.current.listDevices()).rejects.toThrow("boom");
    });

    expect(result.current.loadError).toBe("boom");

    let resolveRetry: (value: unknown) => void = () => {};
    mockListInfrastructureDevices.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveRetry = resolve;
      }),
    );

    let retryPromise: Promise<unknown> = Promise.resolve();
    act(() => {
      retryPromise = result.current.listDevices();
    });

    expect(result.current.loadError).toBeNull();

    await act(async () => {
      resolveRetry({ devices: [apiDevice()] });
      await retryPromise;
    });
  });

  it("ignores a stale list response that resolves after a newer request", async () => {
    let resolveFirst: (value: unknown) => void = () => {};
    let resolveSecond: (value: unknown) => void = () => {};
    mockListInfrastructureDevices.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveFirst = resolve;
      }),
    );

    const { result } = renderHook(() => useInfrastructureDevices(false));

    let firstPromise: Promise<unknown> = Promise.resolve();
    act(() => {
      firstPromise = result.current.listDevices().catch(() => {});
    });

    mockListInfrastructureDevices.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSecond = resolve;
      }),
    );
    let secondPromise: Promise<unknown> = Promise.resolve();
    act(() => {
      secondPromise = result.current.listDevices();
    });

    // The newer (second) request resolves first with real devices...
    await act(async () => {
      resolveSecond({ devices: [apiDevice()] });
      await secondPromise;
    });

    expect(result.current.devices).toHaveLength(1);

    // ...and the stale (first) request resolving afterward must not
    // clobber the newer result.
    await act(async () => {
      resolveFirst({ devices: [] });
      await firstPromise;
    });

    expect(result.current.devices).toHaveLength(1);
  });

  it("creates a device and prepends it to the list", async () => {
    mockCreateInfrastructureDevice.mockResolvedValueOnce({ device: apiDevice() });

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.createDevice({
        siteId: "8",
        buildingName: "Building 1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig,
      });
    });

    expect(mockCreateInfrastructureDevice).toHaveBeenCalledWith(
      expect.objectContaining({
        siteId: 8n,
        buildingName: "Building 1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig,
      }),
    );
    expect(result.current.devices).toHaveLength(1);
    expect(result.current.devices[0].id).toBe("101");
  });

  it("updates a device in place with the full row", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    mockUpdateInfrastructureDevice.mockResolvedValueOnce({
      device: apiDevice({ name: "Renamed exhaust" }),
    });

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    await act(async () => {
      await result.current.updateDevice({
        id: "101",
        siteId: "8",
        buildingName: "Building 1",
        name: "Renamed exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        enabled: true,
        driverType: "modbus_tcp",
        driverConfig,
      });
    });

    expect(mockUpdateInfrastructureDevice).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 101n,
        siteId: 8n,
        name: "Renamed exhaust",
        enabled: true,
      }),
    );
    expect(result.current.devices).toHaveLength(1);
    expect(result.current.devices[0].name).toBe("Renamed exhaust");
  });

  it("omits enabled from the update request when the caller doesn't set it", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    mockUpdateInfrastructureDevice.mockResolvedValueOnce({
      device: apiDevice({ name: "Renamed exhaust" }),
    });

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    await act(async () => {
      await result.current.updateDevice({
        id: "101",
        siteId: "8",
        buildingName: "Building 1",
        name: "Renamed exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        driverType: "modbus_tcp",
        driverConfig,
      });
    });

    const sentRequest = mockUpdateInfrastructureDevice.mock.calls[0][0] as { enabled?: boolean };
    expect(sentRequest.enabled).toBeUndefined();
  });

  it("toggles enabled by resending the device's current fields", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    mockUpdateInfrastructureDevice.mockResolvedValueOnce({
      device: apiDevice({ enabled: false }),
    });

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    await act(async () => {
      await result.current.setDeviceEnabled(result.current.devices[0], false);
    });

    expect(mockUpdateInfrastructureDevice).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 101n,
        siteId: 8n,
        buildingName: "Building 1",
        name: "Roof exhaust",
        deviceKind: "fan_group",
        fanCount: 12,
        enabled: false,
        driverType: "modbus_tcp",
        driverConfig,
      }),
    );
    expect(result.current.devices[0].enabled).toBe(false);
  });

  it("tracks the updating device ID while a mutation is in flight", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    let resolveUpdate: (value: unknown) => void = () => {};
    mockUpdateInfrastructureDevice.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveUpdate = resolve;
      }),
    );

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    let togglePromise: Promise<unknown> = Promise.resolve();
    act(() => {
      togglePromise = result.current.setDeviceEnabled(result.current.devices[0], false);
    });

    expect(result.current.updatingDeviceIds.has("101")).toBe(true);

    await act(async () => {
      resolveUpdate({ device: apiDevice({ enabled: false }) });
      await togglePromise;
    });

    expect(result.current.updatingDeviceIds.has("101")).toBe(false);
  });

  it("deletes a device and removes it from the list", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    mockDeleteInfrastructureDevice.mockResolvedValueOnce({});

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    await act(async () => {
      await result.current.deleteDevice("101");
    });

    expect(mockDeleteInfrastructureDevice).toHaveBeenCalledWith(expect.objectContaining({ id: 101n }));
    expect(result.current.devices).toHaveLength(0);
  });

  it("tracks the updating device ID while a delete is in flight", async () => {
    mockListInfrastructureDevices.mockResolvedValueOnce({ devices: [apiDevice()] });
    let resolveDelete: (value: unknown) => void = () => {};
    mockDeleteInfrastructureDevice.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveDelete = resolve;
      }),
    );

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await result.current.listDevices();
    });

    let deletePromise: Promise<unknown> = Promise.resolve();
    act(() => {
      deletePromise = result.current.deleteDevice("101");
    });

    expect(result.current.updatingDeviceIds.has("101")).toBe(true);

    await act(async () => {
      resolveDelete({});
      await deletePromise;
    });

    expect(result.current.updatingDeviceIds.has("101")).toBe(false);
  });

  it("rethrows mutation failures with the RPC message", async () => {
    mockCreateInfrastructureDevice.mockRejectedValueOnce(new Error("site not found"));

    const { result } = renderHook(() => useInfrastructureDevices(false));

    await act(async () => {
      await expect(
        result.current.createDevice({
          siteId: "8",
          buildingName: "Building 1",
          name: "Roof exhaust",
          deviceKind: "fan_group",
          fanCount: 12,
          driverType: "modbus_tcp",
          driverConfig,
        }),
      ).rejects.toThrow("site not found");
    });

    expect(mockHandleAuthErrors).toHaveBeenCalled();
  });
});
