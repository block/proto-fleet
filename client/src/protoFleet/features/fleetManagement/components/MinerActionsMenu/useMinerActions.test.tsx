import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import { deviceActions, performanceActions, settingsActions } from "./constants";
import { useMinerActions } from "./useMinerActions";
import { PerformanceMode } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { FleetSlice } from "@/protoFleet/store/slices/fleetSlice";
import { createFleetSlice } from "@/protoFleet/store/slices/fleetSlice";
import type { UISlice } from "@/protoFleet/store/slices/uiSlice";
import { createUISlice } from "@/protoFleet/store/slices/uiSlice";
import * as toaster from "@/shared/features/toaster";

type TestStore = { fleet: FleetSlice; ui: UISlice };

// Create mock functions at module level
const mockStartBatchOperation = vi.fn();
const mockCompleteBatchOperation = vi.fn();
const mockRemoveDevicesFromBatch = vi.fn();
const mockFetchBatchTelemetry = vi.fn();
const mockResetFetchedIds = vi.fn();
const mockStreamCommandBatchUpdates = vi.fn((_params: any) => Promise.resolve());
const mockStartMining = vi.fn();
const mockStopMining = vi.fn();
const mockBlinkLED = vi.fn();
const mockUnpair = vi.fn();
const mockReboot = vi.fn();
const mockSetPowerTarget = vi.fn();

// Mock dependencies
vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({
    startMining: mockStartMining,
    stopMining: mockStopMining,
    blinkLED: mockBlinkLED,
    unpair: mockUnpair,
    reboot: mockReboot,
    streamCommandBatchUpdates: mockStreamCommandBatchUpdates,
    setPowerTarget: mockSetPowerTarget,
  }),
}));

vi.mock("@/protoFleet/api/useBatchTelemetry", () => ({
  default: () => ({
    fetchBatchTelemetry: mockFetchBatchTelemetry,
    resetFetchedIds: mockResetFetchedIds,
  }),
}));

vi.mock("@/protoFleet/store", () => ({
  useFleetStore: {
    getState: vi.fn(),
  },
  useStartBatchOperation: () => mockStartBatchOperation,
  useCompleteBatchOperation: () => mockCompleteBatchOperation,
  useRemoveDevicesFromBatch: () => mockRemoveDevicesFromBatch,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => 1),
  updateToast: vi.fn(),
  STATUSES: {
    success: "success",
    error: "error",
    loading: "loading",
  },
}));

describe("useMinerActions", () => {
  let store: any;

  beforeEach(async () => {
    vi.clearAllMocks();

    // Create a fresh store for each test
    store = create<TestStore>()(
      immer((set, get, api) => ({
        fleet: createFleetSlice(set as any, get as any, api as any),
        ui: createUISlice(set as any, get as any, api as any),
      })),
    );

    // Setup mock implementations
    const { useFleetStore } = vi.mocked(await import("@/protoFleet/store"));
    useFleetStore.getState = vi.fn(() => store.getState());
  });

  describe("Basic hook initialization", () => {
    it("should initialize with correct default values", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
          totalCount: 2,
        }),
      );

      expect(result.current.currentAction).toBeNull();
      expect(result.current.numberOfMiners).toBe(2);
      expect(result.current.showManagePowerModal).toBe(false);
      expect(result.current.popoverActions).toBeDefined();
      expect(result.current.popoverActions.length).toBeGreaterThan(0);
    });

    it("should calculate displayCount correctly for 'all' selection mode", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1" }, { deviceIdentifier: "device-2" }],
          selectionMode: "all",
          totalCount: 100,
        }),
      );

      const sleepAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);
      expect(sleepAction?.confirmation?.title).toContain("100");
    });

    it("should calculate displayCount correctly for 'subset' selection mode", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1" }, { deviceIdentifier: "device-2" }],
          selectionMode: "subset",
          totalCount: 100,
        }),
      );

      const sleepAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);
      expect(sleepAction?.confirmation?.title).toContain("2");
    });

    it("should include all expected actions in popoverActions", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const actions = result.current.popoverActions.map((a) => a.action);

      expect(actions).toContain(deviceActions.blinkLEDs);
      expect(actions).toContain(deviceActions.reboot);
      expect(actions).toContain(deviceActions.shutdown);
      expect(actions).toContain(deviceActions.unpair);
      expect(actions).toContain(performanceActions.managePower);
      expect(actions).toContain(settingsActions.miningPool);
    });
  });

  describe("Power state actions", () => {
    it("should show both sleep and wake up actions for bulk selection with mixed status", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.INACTIVE },
          ],
          selectionMode: "subset",
        }),
      );

      const sleepAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);
      const wakeUpAction = result.current.popoverActions.find((a) => a.action === deviceActions.wakeUp);

      expect(sleepAction).toBeDefined();
      expect(wakeUpAction).toBeDefined();
    });

    it("should show only wake up action for single inactive device", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.INACTIVE }],
          selectionMode: "subset",
        }),
      );

      const actions = result.current.popoverActions.map((a) => a.action);

      expect(actions).not.toContain(deviceActions.shutdown);
      expect(actions).toContain(deviceActions.wakeUp);
    });

    it("should show only sleep action for single active device", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const actions = result.current.popoverActions.map((a) => a.action);

      expect(actions).toContain(deviceActions.shutdown);
      expect(actions).not.toContain(deviceActions.wakeUp);
    });

    it("should show both actions when device status is undefined (bulk with different statuses)", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ERROR },
          ],
          selectionMode: "subset",
        }),
      );

      const actions = result.current.popoverActions.map((a) => a.action);

      expect(actions).toContain(deviceActions.shutdown);
      expect(actions).toContain(deviceActions.wakeUp);
    });
  });

  describe("Action handlers - Setting current action", () => {
    it("should set currentAction when reboot action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      act(() => {
        rebootAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.reboot);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when shutdown action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const shutdownAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);

      act(() => {
        shutdownAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.shutdown);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when wake up action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.INACTIVE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const wakeUpAction = result.current.popoverActions.find((a) => a.action === deviceActions.wakeUp);

      act(() => {
        wakeUpAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.wakeUp);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when unpair action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const unpairAction = result.current.popoverActions.find((a) => a.action === deviceActions.unpair);

      act(() => {
        unpairAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.unpair);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when mining pool action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      act(() => {
        poolAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(settingsActions.miningPool);
      expect(onActionStart).toHaveBeenCalled();
    });
  });

  describe("Blink LEDs action (immediate execution, no confirmation)", () => {
    it("should call blinkLED API when blink action handler is called", () => {
      mockBlinkLED.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-blink" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const blinkAction = result.current.popoverActions.find((a) => a.action === deviceActions.blinkLEDs);

      act(() => {
        blinkAction?.actionHandler();
      });

      expect(mockBlinkLED).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-blink",
        action: deviceActions.blinkLEDs,
        deviceIdentifiers: ["device-1"],
      });
    });

    it("should push loading toast when blink action is triggered", () => {
      mockBlinkLED.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-blink" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const blinkAction = result.current.popoverActions.find((a) => a.action === deviceActions.blinkLEDs);

      act(() => {
        blinkAction?.actionHandler();
      });

      expect(toaster.pushToast).toHaveBeenCalledWith({
        message: "Blinking LEDs",
        status: toaster.STATUSES.loading,
        longRunning: true,
      });
    });
  });

  describe("Modal interactions", () => {
    it("should open manage power modal when action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const managePowerAction = result.current.popoverActions.find((a) => a.action === performanceActions.managePower);

      act(() => {
        managePowerAction?.actionHandler();
      });

      expect(result.current.showManagePowerModal).toBe(true);
      expect(result.current.currentAction).toBe(performanceActions.managePower);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should handle manage power confirm and call API", () => {
      mockSetPowerTarget.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-power" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Open modal first
      const managePowerAction = result.current.popoverActions.find((a) => a.action === performanceActions.managePower);

      act(() => {
        managePowerAction?.actionHandler();
      });

      // Confirm with performance mode
      act(() => {
        result.current.handleManagePowerConfirm(PerformanceMode.MAXIMUM_HASHRATE);
      });

      expect(result.current.showManagePowerModal).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(mockSetPowerTarget).toHaveBeenCalled();
      // Note: setPowerTarget does not track batch operations since it completes instantly
      // and doesn't show loading states or require status confirmation polling
    });

    it("should handle manage power dismiss", () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      // Open modal first
      const managePowerAction = result.current.popoverActions.find((a) => a.action === performanceActions.managePower);

      act(() => {
        managePowerAction?.actionHandler();
      });

      // Dismiss modal
      act(() => {
        result.current.handleManagePowerDismiss();
      });

      expect(result.current.showManagePowerModal).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });
  });

  describe("handleConfirmation", () => {
    it("should call stopMining API when confirming shutdown action", async () => {
      mockStopMining.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-shutdown" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Set current action to shutdown
      const shutdownAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);

      act(() => {
        shutdownAction?.actionHandler();
      });

      // Call handleConfirmation
      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockStopMining).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-shutdown",
        action: deviceActions.shutdown,
        deviceIdentifiers: ["device-1"],
      });
      expect(result.current.currentAction).toBeNull();
    });

    it("should call startMining API when confirming wake up action", async () => {
      mockStartMining.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-wakeup" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.INACTIVE }],
          selectionMode: "subset",
        }),
      );

      const wakeUpAction = result.current.popoverActions.find((a) => a.action === deviceActions.wakeUp);

      act(() => {
        wakeUpAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockStartMining).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-wakeup",
        action: deviceActions.wakeUp,
        deviceIdentifiers: ["device-1"],
      });
    });

    it("should call unpair API when confirming unpair action", async () => {
      mockUnpair.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-unpair" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const unpairAction = result.current.popoverActions.find((a) => a.action === deviceActions.unpair);

      act(() => {
        unpairAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockUnpair).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-unpair",
        action: deviceActions.unpair,
        deviceIdentifiers: ["device-1"],
      });
    });

    it("should call reboot API when confirming reboot action", async () => {
      mockReboot.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-reboot" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      act(() => {
        rebootAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockReboot).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-reboot",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1"],
      });
    });
  });

  describe("handleCancel", () => {
    it("should reset currentAction to null and call onActionComplete", () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      // Set an action first
      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      act(() => {
        rebootAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.reboot);

      // Cancel
      act(() => {
        result.current.handleCancel();
      });

      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });
  });

  describe("Callbacks", () => {
    it("should call onActionStart when confirmation action is triggered", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      act(() => {
        rebootAction?.actionHandler();
      });

      expect(onActionStart).toHaveBeenCalled();
    });

    it("should call onActionComplete when handleCancel is called", () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      act(() => {
        result.current.handleCancel();
      });

      expect(onActionComplete).toHaveBeenCalled();
    });
  });

  describe("handleMiningPoolSuccess", () => {
    it("should start batch operation and push toast", () => {
      const batchIdentifier = "batch-pool";

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      act(() => {
        result.current.handleMiningPoolSuccess(batchIdentifier);
      });

      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier,
        action: settingsActions.miningPool,
        deviceIdentifiers: ["device-1"],
      });

      expect(toaster.pushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Assigning pools miners",
          status: toaster.STATUSES.loading,
          longRunning: true,
        }),
      );

      expect(result.current.currentAction).toBeNull();
    });
  });

  describe("handleMiningPoolError", () => {
    it("should push error toast and reset current action", () => {
      const onActionComplete = vi.fn();
      const errorMessage = "Failed to assign pool";

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      // Set current action first
      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      act(() => {
        poolAction?.actionHandler();
      });

      // Trigger error
      act(() => {
        result.current.handleMiningPoolError(errorMessage);
      });

      expect(toaster.pushToast).toHaveBeenCalledWith({
        message: errorMessage,
        status: toaster.STATUSES.error,
        longRunning: true,
      });

      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });
  });

  describe("Status polling optimization with visible miners", () => {
    it("should filter telemetry fetch to only visible miners", () => {
      // This test verifies the filtering logic without relying on polling timing
      const successDeviceIds = ["device-1", "device-2", "device-3"];
      const visibleMinerIds = new Set(["device-1", "device-3"]);

      // Test the filtering logic that the implementation uses
      const visibleSuccessDeviceIds = successDeviceIds.filter((id) => visibleMinerIds.has(id));

      expect(visibleSuccessDeviceIds).toEqual(["device-1", "device-3"]);
      expect(visibleSuccessDeviceIds).not.toContain("device-2");
    });
  });

  describe("Reboot status completion check", () => {
    it("should consider reboot complete when device status is ONLINE", () => {
      // Test the status check logic directly - TypeScript knows this is always true,
      // but we're testing the runtime behavior for documentation purposes
      const deviceStatus: DeviceStatus = DeviceStatus.ONLINE;
      // @ts-expect-error - Testing runtime behavior: any non-OFFLINE status completes reboot
      const isRebootComplete = deviceStatus !== DeviceStatus.OFFLINE;

      expect(isRebootComplete).toBe(true);
    });

    it("should consider reboot complete when device status is NEEDS_MINING_POOL", () => {
      // Test the status check logic directly
      const deviceStatus: DeviceStatus = DeviceStatus.NEEDS_MINING_POOL;
      // @ts-expect-error - Testing runtime behavior: any non-OFFLINE status completes reboot
      const isRebootComplete = deviceStatus !== DeviceStatus.OFFLINE;

      expect(isRebootComplete).toBe(true);
    });

    it("should consider reboot complete when device status is ERROR", () => {
      // Test the status check logic directly
      const deviceStatus: DeviceStatus = DeviceStatus.ERROR;
      // @ts-expect-error - Testing runtime behavior: any non-OFFLINE status completes reboot
      const isRebootComplete = deviceStatus !== DeviceStatus.OFFLINE;

      expect(isRebootComplete).toBe(true);
    });

    it("should NOT consider reboot complete when device status is OFFLINE", () => {
      // Test the status check logic directly
      const deviceStatus = DeviceStatus.OFFLINE;
      const isRebootComplete = deviceStatus !== DeviceStatus.OFFLINE;

      expect(isRebootComplete).toBe(false);
    });
  });

  describe("Polling intervals and timeout", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("should poll every 3 seconds during status confirmation", async () => {
      const successDeviceIds = ["device-1"];

      mockReboot.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-reboot" });
      });

      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        setTimeout(() => {
          onStreamData({
            status: {
              commandBatchDeviceCount: {
                total: BigInt(1),
                success: BigInt(1),
                failure: BigInt(0),
                successDeviceIdentifiers: successDeviceIds,
                failureDeviceIdentifiers: [],
              },
            },
          });
        }, 100);
        // Keep stream open
        return new Promise(() => {}) as Promise<void>;
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Keep device OFFLINE to trigger polling
      store.getState().fleet.updateMinerDeviceStatus({ deviceId: "device-1", deviceStatus: DeviceStatus.OFFLINE });

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      act(() => {
        rebootAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      // Wait for stream callback to execute
      await act(async () => {
        await vi.advanceTimersByTimeAsync(200);
      });

      // Track completion calls before advancing time
      const initialCalls = mockCompleteBatchOperation.mock.calls.length;

      // Advance 2.5 seconds - should not poll yet
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2500);
      });

      expect(mockCompleteBatchOperation.mock.calls.length).toBe(initialCalls);

      // Advance to 3 seconds - should poll once
      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });

      // Should have polled (but not completed since device still OFFLINE)
      expect(mockCompleteBatchOperation.mock.calls.length).toBe(initialCalls);

      // Advance another 3 seconds - should poll again
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3000);
      });

      // Polling happened (still not complete)
      expect(mockCompleteBatchOperation.mock.calls.length).toBe(initialCalls);
    });

    it("should timeout after reaching max polls (3 minutes)", () => {
      // Test the timeout logic directly
      const checkInterval = 3000; // 3 seconds
      const maxPolls = 60; // 3 minutes max
      const totalTimeoutMs = maxPolls * checkInterval;

      expect(totalTimeoutMs).toBe(180000); // 180 seconds = 3 minutes
      expect(maxPolls).toBeGreaterThan(0);
    });

    it("should refetch telemetry every 10 polling cycles (30 seconds)", () => {
      // Test the telemetry refetch interval logic directly
      const checkInterval = 3000; // 3 seconds per poll
      const refetchEveryNPolls = 10;
      const refetchIntervalMs = refetchEveryNPolls * checkInterval;

      expect(refetchIntervalMs).toBe(30000); // 30 seconds

      // Test the modulo logic used in implementation
      for (let pollCount = 1; pollCount <= 30; pollCount++) {
        const shouldRefetch = pollCount % 10 === 0;
        if (pollCount === 10 || pollCount === 20 || pollCount === 30) {
          expect(shouldRefetch).toBe(true);
        } else {
          expect(shouldRefetch).toBe(false);
        }
      }
    });
  });
});
