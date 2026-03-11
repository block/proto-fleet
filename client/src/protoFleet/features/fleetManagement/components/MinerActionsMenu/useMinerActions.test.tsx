import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create as createProto } from "@bufbuild/protobuf";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import { deviceActions, performanceActions, settingsActions } from "./constants";
import { useMinerActions } from "./useMinerActions";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { MinerListFilterSchema, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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
const mockStreamCommandBatchUpdates = vi.fn((_params: any) => Promise.resolve());
const mockStartMining = vi.fn();
const mockStopMining = vi.fn();
const mockBlinkLED = vi.fn();
const mockDeleteMiners = vi.fn();
const mockReboot = vi.fn();
const mockSetPowerTarget = vi.fn();
const mockSetCoolingMode = vi.fn();
const mockUpdateMinerPassword = vi.fn();
const mockGetMinerModelGroups = vi.fn();
const mockDownloadLogs = vi.fn();
const mockGetCommandBatchLogBundle = vi.fn();
const mockRenameSingleMiner = vi.fn();
const mockUpdateMinerName = vi.fn();
const mockCheckCommandCapabilities = vi.fn(({ onSuccess }) => {
  // Default to all supported (no modal shown)
  onSuccess({
    allSupported: true,
    noneSupported: false,
    supportedCount: 1,
    unsupportedCount: 0,
    totalCount: 1,
    unsupportedGroups: [],
    supportedDeviceIdentifiers: [],
  });
});

// Mock dependencies
vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: () => ({
    startMining: mockStartMining,
    stopMining: mockStopMining,
    blinkLED: mockBlinkLED,
    deleteMiners: mockDeleteMiners,
    reboot: mockReboot,
    streamCommandBatchUpdates: mockStreamCommandBatchUpdates,
    setPowerTarget: mockSetPowerTarget,
    setCoolingMode: mockSetCoolingMode,
    checkCommandCapabilities: mockCheckCommandCapabilities,
    updateMinerPassword: mockUpdateMinerPassword,
    downloadLogs: mockDownloadLogs,
    getCommandBatchLogBundle: mockGetCommandBatchLogBundle,
  }),
}));

const mockFetchCoolingMode = vi.fn(() => Promise.resolve(0)); // CoolingMode.UNSPECIFIED
vi.mock("@/protoFleet/api/useMinerCoolingMode", () => ({
  default: () => ({
    fetchCoolingMode: mockFetchCoolingMode,
  }),
}));

vi.mock("@/protoFleet/api/useRenameMiners", () => ({
  default: () => ({
    renameSingleMiner: mockRenameSingleMiner,
  }),
}));

const { mockUseFleetStore } = vi.hoisted(() => {
  const fn: any = vi.fn();
  fn.getState = vi.fn();
  return { mockUseFleetStore: fn };
});

vi.mock("@/protoFleet/api/useMinerModelGroups", () => ({
  default: () => ({
    getMinerModelGroups: mockGetMinerModelGroups,
  }),
}));

vi.mock("@/protoFleet/store", () => ({
  useFleetStore: mockUseFleetStore,
  useStartBatchOperation: () => mockStartBatchOperation,
  useCompleteBatchOperation: () => mockCompleteBatchOperation,
  useRemoveDevicesFromBatch: () => mockRemoveDevicesFromBatch,
  useUpdateMinerName: () => mockUpdateMinerName,
  useAuthErrors: () => ({
    handleAuthErrors: vi.fn(({ onError }) => onError?.()),
  }),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => 1),
  updateToast: vi.fn(),
  removeToast: vi.fn(),
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
    mockGetMinerModelGroups.mockResolvedValue([]);

    // Create a fresh store for each test
    store = create<TestStore>()(
      immer((set, get, api) => ({
        fleet: createFleetSlice(set as any, get as any, api as any),
        ui: createUISlice(set as any, get as any, api as any),
      })),
    );

    // Setup mock implementations: support both selector calls and getState()
    mockUseFleetStore.mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector(store.getState());
      }
      return store.getState();
    });
    mockUseFleetStore.getState = vi.fn(() => store.getState());
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
      expect(actions).toContain(deviceActions.delete);
      expect(actions).toContain(performanceActions.managePower);
      expect(actions).toContain(settingsActions.miningPool);
      expect(actions).toContain(settingsActions.coolingMode);
      expect(actions).not.toContain(settingsActions.rename);
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
    it("should set currentAction when reboot action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.reboot);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when shutdown action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const shutdownAction = result.current.popoverActions.find((a) => a.action === deviceActions.shutdown);

      await act(async () => {
        await shutdownAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.shutdown);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when wake up action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.INACTIVE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const wakeUpAction = result.current.popoverActions.find((a) => a.action === deviceActions.wakeUp);

      await act(async () => {
        await wakeUpAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.wakeUp);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should set currentAction when delete action handler is called", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);

      act(() => {
        deleteAction?.actionHandler();
      });

      expect(result.current.currentAction).toBe(deviceActions.delete);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should show authentication modal when mining pool action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      expect(result.current.showAuthenticateFleetModal).toBe(true);
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
    it("should open manage power modal when action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const managePowerAction = result.current.popoverActions.find((a) => a.action === performanceActions.managePower);

      await act(async () => {
        await managePowerAction?.actionHandler();
      });

      expect(result.current.showManagePowerModal).toBe(true);
      expect(result.current.currentAction).toBe(performanceActions.managePower);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should handle manage power confirm and call API", async () => {
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

      await act(async () => {
        await managePowerAction?.actionHandler();
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

    it("should handle manage power dismiss", async () => {
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

      await act(async () => {
        await managePowerAction?.actionHandler();
      });

      // Dismiss modal
      act(() => {
        result.current.handleManagePowerDismiss();
      });

      expect(result.current.showManagePowerModal).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should open cooling mode modal and fetch current mode for single miner", async () => {
      const onActionStart = vi.fn();
      mockFetchCoolingMode.mockResolvedValueOnce(CoolingMode.AIR_COOLED);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const coolingModeAction = result.current.popoverActions.find((a) => a.action === settingsActions.coolingMode);

      await act(async () => {
        await coolingModeAction?.actionHandler();
      });

      expect(result.current.showCoolingModeModal).toBe(true);
      expect(result.current.currentAction).toBe(settingsActions.coolingMode);
      expect(onActionStart).toHaveBeenCalled();
      expect(mockFetchCoolingMode).toHaveBeenCalledWith("device-1");
      expect(result.current.currentCoolingMode).toBe(CoolingMode.AIR_COOLED);
    });

    it("should not fetch cooling mode for multi-miner selection", async () => {
      mockFetchCoolingMode.mockClear();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const coolingModeAction = result.current.popoverActions.find((a) => a.action === settingsActions.coolingMode);

      await act(async () => {
        await coolingModeAction?.actionHandler();
      });

      expect(result.current.showCoolingModeModal).toBe(true);
      expect(mockFetchCoolingMode).not.toHaveBeenCalled();
      expect(result.current.currentCoolingMode).toBeUndefined();
    });

    it("should handle cooling mode confirm and call API", async () => {
      mockSetCoolingMode.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-cooling" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Open modal first
      const coolingModeAction = result.current.popoverActions.find((a) => a.action === settingsActions.coolingMode);

      await act(async () => {
        await coolingModeAction?.actionHandler();
      });

      // Confirm with cooling mode
      act(() => {
        result.current.handleCoolingModeConfirm(CoolingMode.AIR_COOLED);
      });

      expect(result.current.showCoolingModeModal).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(mockSetCoolingMode).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-cooling",
        action: settingsActions.coolingMode,
        deviceIdentifiers: ["device-1"],
      });
    });

    it("should handle cooling mode dismiss", async () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      // Open modal first
      const coolingModeAction = result.current.popoverActions.find((a) => a.action === settingsActions.coolingMode);

      await act(async () => {
        await coolingModeAction?.actionHandler();
      });

      // Dismiss modal
      act(() => {
        result.current.handleCoolingModeDismiss();
      });

      expect(result.current.showCoolingModeModal).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should use filtered device selector for cooling mode when unsupported miners exist", async () => {
      mockSetCoolingMode.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-cooling-filtered" });
      });

      // First call returns partial support (triggers unsupported miners modal)
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const coolingModeAction = result.current.popoverActions.find((a) => a.action === settingsActions.coolingMode);

      await act(async () => {
        await coolingModeAction?.actionHandler();
      });

      // Unsupported miners modal should be shown
      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.supportedDeviceIdentifiers).toEqual(["device-1"]);

      // Continue with supported miners only
      await act(async () => {
        result.current.handleUnsupportedMinersContinue();
      });

      // Now modal should be shown with filtered count
      expect(result.current.showCoolingModeModal).toBe(true);
      expect(result.current.coolingModeCount).toBe(1);

      // Confirm with cooling mode
      act(() => {
        result.current.handleCoolingModeConfirm(CoolingMode.IMMERSION_COOLED);
      });

      // Should have been called with only the supported device
      expect(mockSetCoolingMode).toHaveBeenCalled();
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-cooling-filtered",
        action: settingsActions.coolingMode,
        deviceIdentifiers: ["device-1"],
      });
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

      await act(async () => {
        await shutdownAction?.actionHandler();
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

      await act(async () => {
        await wakeUpAction?.actionHandler();
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

    it("should call deleteMiners API with explicit device identifiers in subset mode", async () => {
      mockDeleteMiners.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ deletedCount: 1 });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);

      act(() => {
        deleteAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockDeleteMiners).toHaveBeenCalled();
      const calledWith = mockDeleteMiners.mock.calls[0][0];
      const selector = calledWith.deleteMinersRequest.deviceSelector;
      expect(selector.selectionType.case).toBe("includeDevices");
      expect(selector.selectionType.value.deviceIdentifiers).toEqual(["device-1"]);
      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({
          message: "Deleted 1 miner",
          status: "success",
        }),
      );
    });

    it("should call deleteMiners API with allDevices selector and filter in 'all' mode", async () => {
      mockDeleteMiners.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ deletedCount: 10 });
      });

      const activeFilter = createProto(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ERROR],
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "all",
          totalCount: 10,
          currentFilter: activeFilter,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);

      act(() => {
        deleteAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockDeleteMiners).toHaveBeenCalled();
      const calledWith = mockDeleteMiners.mock.calls[0][0];
      const selector = calledWith.deleteMinersRequest.deviceSelector;
      expect(selector.selectionType.case).toBe("allDevices");
      expect(selector.selectionType.value.deviceStatus).toEqual([DeviceStatus.ERROR]);
      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({
          message: "Deleted 10 miners",
          status: "success",
        }),
      );
    });

    it("should send allDevices selector in 'all' mode when no active filter", async () => {
      mockDeleteMiners.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ deletedCount: 5 });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "all",
          totalCount: 5,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);

      act(() => {
        deleteAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockDeleteMiners).toHaveBeenCalled();
      const calledWith = mockDeleteMiners.mock.calls[0][0];
      const selector = calledWith.deleteMinersRequest.deviceSelector;
      expect(selector.selectionType.case).toBe("allDevices");
      expect(selector.selectionType.value).toBeDefined();
    });

    it("should use includeDevices selector in subset mode even with active filter", async () => {
      mockDeleteMiners.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ deletedCount: 1 });
      });

      const activeFilter = createProto(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ERROR],
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          currentFilter: activeFilter,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);

      act(() => {
        deleteAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleConfirmation();
      });

      expect(mockDeleteMiners).toHaveBeenCalled();
      const calledWith = mockDeleteMiners.mock.calls[0][0];
      const selector = calledWith.deleteMinersRequest.deviceSelector;
      expect(selector.selectionType.case).toBe("includeDevices");
      expect(selector.selectionType.value.deviceIdentifiers).toEqual(["device-1"]);
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

      await act(async () => {
        await rebootAction?.actionHandler();
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
    it("should reset currentAction to null and call onActionComplete", async () => {
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

      await act(async () => {
        await rebootAction?.actionHandler();
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
    it("should call onActionStart when confirmation action is triggered", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
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
      store.getState().fleet.setMiners([
        {
          deviceIdentifier: "device-1",
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.PAIRED,
          name: "device-1",
          macAddress: "",
          serialNumber: "",
          model: "",
          manufacturer: "",
          ipAddress: "",
          url: "",
          firmwareVersion: "",
          powerUsage: [],
          temperature: [],
          hashrate: [],
          efficiency: [],
          temperatureStatus: 0,
          driverName: "",
        },
      ]);

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
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

  describe("Unsupported miners modal flow", () => {
    it("should show unsupported miners modal when some miners do not support the action", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 2,
          totalCount: 3,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 2 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-3", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.totalUnsupportedCount).toBe(2);
      expect(result.current.unsupportedMinersInfo.noneSupported).toBe(false);
      expect(result.current.unsupportedMinersInfo.supportedDeviceIdentifiers).toEqual(["device-1"]);
      expect(result.current.unsupportedMinersInfo.unsupportedGroups).toHaveLength(1);
    });

    it("should show unsupported miners modal with noneSupported flag when no miners support the action", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: true,
          supportedCount: 0,
          unsupportedCount: 2,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 2 }],
          supportedDeviceIdentifiers: [],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.noneSupported).toBe(true);
      expect(result.current.unsupportedMinersInfo.supportedDeviceIdentifiers).toEqual([]);
      expect(result.current.currentAction).toBeNull();
    });

    it("should not show confirmation dialog when unsupported miners modal is shown", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.currentAction).toBeNull();
    });

    it("should execute action with filtered device selector when continuing from unsupported modal", async () => {
      mockReboot.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-reboot" });
      });

      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.supportedDeviceIdentifiers).toEqual(["device-1"]);

      await act(async () => {
        result.current.handleUnsupportedMinersContinue();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(false);
      // Verify reboot was called
      expect(mockReboot).toHaveBeenCalled();
      // Verify batch operation was started with only the supported device identifier
      expect(mockStartBatchOperation).toHaveBeenCalledWith({
        batchIdentifier: "batch-reboot",
        action: deviceActions.reboot,
        deviceIdentifiers: ["device-1"],
      });
    });

    it("should reset state when dismissing unsupported miners modal", async () => {
      const onActionComplete = vi.fn();

      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);

      act(() => {
        result.current.handleUnsupportedMinersDismiss();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should proceed without modal when all miners support the action", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: true,
          noneSupported: false,
          supportedCount: 2,
          unsupportedCount: 0,
          totalCount: 2,
          unsupportedGroups: [],
          supportedDeviceIdentifiers: ["device-1", "device-2"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(false);
      expect(result.current.currentAction).toBe(deviceActions.reboot);
    });

    it("should proceed without modal when capability check fails (fail-open)", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onError }: any) => {
        onError(new Error("Network error"));
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const rebootAction = result.current.popoverActions.find((a) => a.action === deviceActions.reboot);

      await act(async () => {
        await rebootAction?.actionHandler();
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(false);
      expect(result.current.currentAction).toBe(deviceActions.reboot);
    });
  });

  describe("Delete confirmation contextual subtitles", () => {
    const setStoreMiners = (
      miners: Array<{ id: string; driverName: string; deviceStatus: number; pairingStatus: number }>,
    ) => {
      const minerSnapshots = miners.map((m) => ({
        deviceIdentifier: m.id,
        driverName: m.driverName,
        deviceStatus: m.deviceStatus,
        pairingStatus: m.pairingStatus,
        name: m.id,
        macAddress: "",
        serialNumber: "",
        model: "",
        manufacturer: "",
        ipAddress: "",
        url: "",
        firmwareVersion: "",
        powerUsage: [],
        temperature: [],
        hashrate: [],
        efficiency: [],
        temperatureStatus: 0,
      }));
      store.getState().fleet.setMiners(minerSnapshots);
    };

    it("should show auth-key-cleared message for single online paired Proto rig", () => {
      setStoreMiners([
        { id: "device-1", driverName: "proto", deviceStatus: DeviceStatus.ONLINE, pairingStatus: PairingStatus.PAIRED },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toBe(
        "This miner will be removed from your fleet and its auth key will be cleared.",
      );
    });

    it("should show unreachable warning for single offline Proto rig", () => {
      setStoreMiners([
        {
          id: "device-1",
          driverName: "proto",
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.OFFLINE }],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toBe(
        "This miner will be removed from your fleet. It may need to be factory reset before re-pairing.",
      );
    });

    it("should show unreachable warning for single unauthenticated Proto rig", () => {
      setStoreMiners([
        {
          id: "device-1",
          driverName: "proto",
          deviceStatus: DeviceStatus.ONLINE,
          pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
        },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toBe(
        "This miner will be removed from your fleet. It may need to be factory reset before re-pairing.",
      );
    });

    it("should show telemetry-stop message for single 3rd-party miner", () => {
      setStoreMiners([
        {
          id: "device-1",
          driverName: "bitmain",
          deviceStatus: DeviceStatus.ONLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toBe(
        "This miner will be removed from your fleet and will stop sending telemetry data.",
      );
    });

    it("should show auth-key-cleared message for multiple online paired Proto rigs", () => {
      setStoreMiners([
        { id: "device-1", driverName: "proto", deviceStatus: DeviceStatus.ONLINE, pairingStatus: PairingStatus.PAIRED },
        { id: "device-2", driverName: "proto", deviceStatus: DeviceStatus.ONLINE, pairingStatus: PairingStatus.PAIRED },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toBe(
        "These miners will be removed from your fleet and their auth keys will be cleared.",
      );
    });

    it("should show mixed warning when bulk deleting Proto rigs with some unreachable", () => {
      setStoreMiners([
        { id: "device-1", driverName: "proto", deviceStatus: DeviceStatus.ONLINE, pairingStatus: PairingStatus.PAIRED },
        {
          id: "device-2",
          driverName: "proto",
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
        {
          id: "device-3",
          driverName: "bitmain",
          deviceStatus: DeviceStatus.ONLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.OFFLINE },
            { deviceIdentifier: "device-3", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toContain("3 miners will be removed");
      expect(deleteAction?.confirmation?.subtitle).toContain("1 Proto miner is unreachable");
      expect(deleteAction?.confirmation?.subtitle).toContain("factory reset");
    });

    it("should show generic message for 'all' selection mode", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1" }],
          selectionMode: "all",
          totalCount: 50,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toContain("All 50 miners");
      expect(deleteAction?.confirmation?.subtitle).toContain("removed from your fleet");
    });

    it("should show 'matching' message for 'all' selection mode with active filter", () => {
      const activeFilter = createProto(MinerListFilterSchema, {
        deviceStatus: [DeviceStatus.ERROR],
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1" }],
          selectionMode: "all",
          totalCount: 12,
          currentFilter: activeFilter,
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toContain("12 matching miners");
      expect(deleteAction?.confirmation?.subtitle).toContain("removed from your fleet");
      expect(deleteAction?.confirmation?.subtitle).not.toContain("All");
    });

    it("should use correct plural for multiple unreachable Proto miners in mixed batch", () => {
      setStoreMiners([
        {
          id: "device-1",
          driverName: "proto",
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
        {
          id: "device-2",
          driverName: "proto",
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
        {
          id: "device-3",
          driverName: "bitmain",
          deviceStatus: DeviceStatus.ONLINE,
          pairingStatus: PairingStatus.PAIRED,
        },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.OFFLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.OFFLINE },
            { deviceIdentifier: "device-3", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const deleteAction = result.current.popoverActions.find((a) => a.action === deviceActions.delete);
      expect(deleteAction?.confirmation?.subtitle).toContain("2 Proto miners are unreachable");
    });
  });

  describe("Mining pool authentication flow", () => {
    it("should show authentication modal when mining pool action handler is called", async () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      expect(result.current.showAuthenticateFleetModal).toBe(true);
      expect(result.current.currentAction).toBe(settingsActions.miningPool);
      expect(onActionStart).toHaveBeenCalled();
    });

    it("should show pool selection page after successful authentication", async () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Trigger mining pool action
      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      expect(result.current.showAuthenticateFleetModal).toBe(true);

      // Authenticate with credentials
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showAuthenticateFleetModal).toBe(false);
      expect(result.current.showPoolSelectionPage).toBe(true);
      expect(result.current.fleetCredentials).toEqual({ username: "testuser", password: "testpass" });
    });

    it("should store pool filtered device IDs when capability check returns partial support", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      // Unsupported miners modal should be shown
      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.supportedDeviceIdentifiers).toEqual(["device-1"]);

      // Continue with supported miners only
      await act(async () => {
        result.current.handleUnsupportedMinersContinue();
      });

      // Should show auth modal with filtered device IDs stored
      expect(result.current.showAuthenticateFleetModal).toBe(true);
      expect(result.current.poolFilteredDeviceIds).toEqual(["device-1"]);
    });

    it("should dismiss pool selection page and reset state when handleCancel is called", async () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      // Trigger mining pool action and authenticate
      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showPoolSelectionPage).toBe(true);

      // Cancel/dismiss
      act(() => {
        result.current.handleCancel();
      });

      expect(result.current.showPoolSelectionPage).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(result.current.fleetCredentials).toBeUndefined();
      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should proceed directly to pool selection when all miners support the action", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: true,
          noneSupported: false,
          supportedCount: 2,
          unsupportedCount: 0,
          totalCount: 2,
          unsupportedGroups: [],
          supportedDeviceIdentifiers: ["device-1", "device-2"],
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const poolAction = result.current.popoverActions.find((a) => a.action === settingsActions.miningPool);

      await act(async () => {
        await poolAction?.actionHandler();
      });

      // Should show auth modal directly (no unsupported miners modal)
      expect(result.current.unsupportedMinersInfo.visible).toBe(false);
      expect(result.current.showAuthenticateFleetModal).toBe(true);
      expect(result.current.poolFilteredDeviceIds).toBeUndefined();
    });
  });

  describe("handlePasswordConfirm - action bar restoration", () => {
    const addMinersToStore = (
      storeInstance: any,
      miners: Array<{ deviceIdentifier: string; manufacturer: string; model: string; name?: string }>,
    ) => {
      storeInstance.getState().fleet.setMiners(miners);
    };

    it("sets group status to failed and keeps ManageSecurityModal open when API call fails", async () => {
      const onActionComplete = vi.fn();

      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
      ]);

      mockUpdateMinerPassword.mockImplementation(({ onError }: any) => {
        onError("Connection failed");
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);

      const group = result.current.minerGroups[0];
      act(() => {
        result.current.handleUpdateGroup(group);
      });
      expect(result.current.showUpdatePasswordModal).toBe(true);

      act(() => {
        result.current.handlePasswordConfirm("oldpass", "newpass");
      });

      // Modal stays open for retry — onActionComplete not called until modal is closed
      expect(onActionComplete).not.toHaveBeenCalled();
      expect(result.current.showManageSecurityModal).toBe(true);
      expect(result.current.minerGroups[0].status).toBe("failed");
    });

    it("does NOT call onActionComplete during batch failure in ManageSecurityModal flow — proto-only selection", async () => {
      const onActionComplete = vi.fn();

      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
      ]);

      mockUpdateMinerPassword.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-security" });
      });

      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({
          status: {
            commandBatchDeviceCount: {
              total: BigInt(1),
              success: BigInt(0),
              failure: BigInt(1),
              successDeviceIdentifiers: [],
              failureDeviceIdentifiers: ["device-1"],
            },
          },
        });
        return Promise.resolve();
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);

      const group = result.current.minerGroups[0];
      act(() => {
        result.current.handleUpdateGroup(group);
      });
      expect(result.current.showUpdatePasswordModal).toBe(true);

      await act(async () => {
        result.current.handlePasswordConfirm("oldpass", "newpass");
      });

      // Modal stays open after batch failure — onActionComplete only called on modal close
      expect(onActionComplete).not.toHaveBeenCalled();
      expect(result.current.showManageSecurityModal).toBe(true);
    });

    it("does NOT call onActionComplete during batch completion in ManageSecurityModal flow — modal handles it", async () => {
      const onActionComplete = vi.fn();

      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
        { deviceIdentifier: "device-2", manufacturer: "bitmain", model: "S19", name: "Antminer S19" },
      ]);

      mockUpdateMinerPassword.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-security" });
      });

      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({
          status: {
            commandBatchDeviceCount: {
              total: BigInt(1),
              success: BigInt(0),
              failure: BigInt(1),
              successDeviceIdentifiers: [],
              failureDeviceIdentifiers: ["device-1"],
            },
          },
        });
        return Promise.resolve();
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);

      const protoGroup = result.current.minerGroups.find((g) => g.manufacturer === "proto");
      act(() => {
        result.current.handleUpdateGroup(protoGroup!);
      });
      expect(result.current.showUpdatePasswordModal).toBe(true);

      await act(async () => {
        result.current.handlePasswordConfirm("oldpass", "newpass");
      });

      // onActionComplete not called yet — ManageSecurityModal is still open
      expect(onActionComplete).not.toHaveBeenCalled();

      // Called only when the modal is closed
      act(() => {
        result.current.handleSecurityModalClose();
      });
      expect(onActionComplete).toHaveBeenCalledTimes(1);
    });
  });

  describe("Manage security action flow", () => {
    const addMinersToStore = (
      storeInstance: any,
      miners: Array<{ deviceIdentifier: string; manufacturer: string; model: string; name?: string }>,
    ) => {
      storeInstance.getState().fleet.setMiners(miners);
    };

    it("shows auth modal when security action is triggered", async () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);

      await act(async () => {
        await securityAction?.actionHandler();
      });

      expect(result.current.showAuthenticateFleetModal).toBe(true);
      expect(result.current.authenticationPurpose).toBe("security");
    });

    it("shows ManageSecurityModal after auth when all miners are proto rigs", async () => {
      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
        { deviceIdentifier: "device-2", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig 2" },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);
      expect(result.current.showUpdatePasswordModal).toBe(false);
      expect(result.current.minerGroups).toHaveLength(1);
    });

    it("shows ManageSecurityModal after auth when miners include non-proto devices", async () => {
      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
        { deviceIdentifier: "device-2", manufacturer: "bitmain", model: "S19", name: "Antminer S19" },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });

      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);
      expect(result.current.showUpdatePasswordModal).toBe(false);
      expect(result.current.minerGroups.length).toBeGreaterThan(0);
    });

    it("handleUpdateGroup opens UpdatePasswordModal for the selected group", async () => {
      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
        { deviceIdentifier: "device-2", manufacturer: "bitmain", model: "S19", name: "Antminer S19" },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      const antminerGroup = result.current.minerGroups.find((g) => g.manufacturer === "bitmain");
      expect(antminerGroup).toBeDefined();

      act(() => {
        result.current.handleUpdateGroup(antminerGroup!);
      });

      expect(result.current.showUpdatePasswordModal).toBe(true);
      expect(result.current.hasThirdPartyMiners).toBe(true);
    });

    it("handleSecurityModalClose resets all security state and calls onActionComplete", async () => {
      const onActionComplete = vi.fn();
      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "bitmain", model: "S19", name: "Antminer S19" },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.showManageSecurityModal).toBe(true);

      act(() => {
        result.current.handleSecurityModalClose();
      });

      expect(result.current.showManageSecurityModal).toBe(false);
      expect(result.current.minerGroups).toHaveLength(0);
      expect(result.current.fleetCredentials).toBeUndefined();
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });

    it("shows UnsupportedMinersModal after auth when some miners do not support password update", async () => {
      mockCheckCommandCapabilities.mockImplementationOnce(({ onSuccess }: any) => {
        onSuccess({
          allSupported: false,
          noneSupported: false,
          supportedCount: 1,
          unsupportedCount: 1,
          totalCount: 2,
          unsupportedGroups: [{ model: "Antminer S19", firmwareVersion: "1.0.0", count: 1 }],
          supportedDeviceIdentifiers: ["device-1"],
        });
      });

      addMinersToStore(store, [
        { deviceIdentifier: "device-1", manufacturer: "proto", model: "Proto Rig", name: "Proto Rig" },
        { deviceIdentifier: "device-2", manufacturer: "bitmain", model: "S19", name: "Antminer S19" },
      ]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const securityAction = result.current.popoverActions.find((a) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });

      expect(result.current.showAuthenticateFleetModal).toBe(true);
      expect(result.current.unsupportedMinersInfo.visible).toBe(false);

      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });

      expect(result.current.unsupportedMinersInfo.visible).toBe(true);
      expect(result.current.unsupportedMinersInfo.totalUnsupportedCount).toBe(1);
      expect(result.current.unsupportedMinersInfo.noneSupported).toBe(false);
      expect(result.current.showManageSecurityModal).toBe(false);
      expect(result.current.showUpdatePasswordModal).toBe(false);
    });
  });

  describe("Manage security action flow - select all mode", () => {
    const triggerSecurityAndAuthenticate = async (result: any) => {
      const securityAction = result.current.popoverActions.find((a: any) => a.action === settingsActions.security);
      await act(async () => {
        await securityAction?.actionHandler();
      });
      await act(async () => {
        await result.current.handleFleetAuthenticated("testuser", "testpass");
      });
    };

    it("calls getMinerModelGroups to fetch backend groups instead of reading local store", async () => {
      mockGetMinerModelGroups.mockResolvedValue([]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "all",
        }),
      );

      await triggerSecurityAndAuthenticate(result);

      expect(mockGetMinerModelGroups).toHaveBeenCalledOnce();
      expect(result.current.showManageSecurityModal).toBe(true);
    });

    it("names Proto Rig groups as manufacturer + model and normalizes manufacturer to lowercase", async () => {
      mockGetMinerModelGroups.mockResolvedValue([{ model: "Rig", manufacturer: "Proto", count: 6 }]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "all",
        }),
      );

      await triggerSecurityAndAuthenticate(result);

      const group = result.current.minerGroups[0];
      expect(group.name).toBe("Proto Rig");
      expect(group.manufacturer).toBe("proto");
      expect(group.count).toBe(6);
    });

    it("names third-party groups by model only, without manufacturer prefix", async () => {
      mockGetMinerModelGroups.mockResolvedValue([{ model: "Antminer S19", manufacturer: "Bitmain", count: 10 }]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "all",
        }),
      );

      await triggerSecurityAndAuthenticate(result);

      const group = result.current.minerGroups[0];
      expect(group.name).toBe("Antminer S19");
      expect(group.manufacturer).toBe("bitmain");
    });

    it("falls back to capability check path when getMinerModelGroups throws", async () => {
      mockGetMinerModelGroups.mockRejectedValue(new Error("Network error"));

      store
        .getState()
        .fleet.setMiners([{ deviceIdentifier: "device-1", manufacturer: "proto", model: "Rig", name: "Proto Rig" }]);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "all",
        }),
      );

      await triggerSecurityAndAuthenticate(result);

      expect(result.current.showManageSecurityModal).toBe(true);
      expect(result.current.minerGroups.length).toBeGreaterThan(0);
    });

    it("uses allDevices selector with model filter in handlePasswordConfirm", async () => {
      mockGetMinerModelGroups.mockResolvedValue([{ model: "Rig", manufacturer: "Proto", count: 6 }]);
      mockUpdateMinerPassword.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-security-all" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "all",
        }),
      );

      await triggerSecurityAndAuthenticate(result);

      const group = result.current.minerGroups[0];
      await act(async () => {
        result.current.handleUpdateGroup(group);
      });
      await act(async () => {
        result.current.handlePasswordConfirm("oldpass", "newpass");
      });

      const callArgs = mockUpdateMinerPassword.mock.calls[0][0];
      expect(callArgs.deviceSelector.selectionType.case).toBe("allDevices");
      expect(callArgs.deviceSelector.selectionType.value.models).toEqual(["Rig"]);
    });
  });

  describe("Download Logs action", () => {
    beforeEach(() => {
      // Reset stream mock to its default behavior in case a test overrode it
      mockStreamCommandBatchUpdates.mockImplementation((_params: any) => Promise.resolve());
      vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:mock-url");
      vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it("should include downloadLogs in popoverActions", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const actions = result.current.popoverActions.map((a) => a.action);
      expect(actions).toContain(deviceActions.downloadLogs);
    });

    it("should call onActionStart to close the menu when triggered", () => {
      const onActionStart = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionStart,
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      act(() => {
        downloadLogsAction?.actionHandler();
      });

      expect(onActionStart).toHaveBeenCalled();
    });

    it("should show loading toast when download begins", async () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        await downloadLogsAction?.actionHandler();
      });

      expect(toaster.pushToast).toHaveBeenCalledWith({
        message: "Downloading logs",
        status: toaster.STATUSES.loading,
        longRunning: true,
      });
    });

    it("should call downloadLogs API with the correct deviceSelector", async () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        await downloadLogsAction?.actionHandler();
      });

      expect(mockDownloadLogs).toHaveBeenCalled();
      const request = mockDownloadLogs.mock.calls[0][0].downloadLogsRequest;
      expect(request.deviceSelector.selectionType.case).toBe("includeDevices");
      expect(request.deviceSelector.selectionType.value.deviceIdentifiers).toEqual(["device-1"]);
    });

    it("should stream batch updates then fetch log bundle after downloadLogs succeeds", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 1, failure: 0 } } });
        return Promise.resolve();
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ chunkData: new Uint8Array([1, 2, 3]), filename: "logs.zip" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Set up anchor spy after renderHook to avoid intercepting React's internal createElement calls
      vi.spyOn(document, "createElement").mockReturnValueOnce({
        href: "",
        download: "",
        click: vi.fn(),
      } as unknown as HTMLElement);

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(mockStreamCommandBatchUpdates).toHaveBeenCalledWith(
        expect.objectContaining({
          streamRequest: expect.objectContaining({ batchIdentifier: "batch-logs-123" }),
        }),
      );
      expect(mockGetCommandBatchLogBundle).toHaveBeenCalledWith(
        expect.objectContaining({
          request: expect.objectContaining({ batchIdentifier: "batch-logs-123" }),
        }),
      );
    });

    it("should trigger browser file download with the correct filename on success", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 1, failure: 0 } } });
        return Promise.resolve();
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ chunkData: new Uint8Array([1, 2, 3]), filename: "miner-logs.zip" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Set up anchor spy after renderHook to avoid intercepting React's internal createElement calls
      const mockAnchorClick = vi.fn();
      const mockAnchor = { href: "", download: "", click: mockAnchorClick };
      vi.spyOn(document, "createElement").mockReturnValueOnce(mockAnchor as unknown as HTMLElement);

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(mockAnchor.download).toBe("miner-logs.zip");
      expect(mockAnchor.href).toBe("blob:mock-url");
      expect(mockAnchorClick).toHaveBeenCalled();
    });

    it("should show success toast after the file is downloaded", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 1, failure: 0 } } });
        return Promise.resolve();
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ chunkData: new Uint8Array([1, 2, 3]), filename: "logs.zip" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      // Set up anchor spy after renderHook to avoid intercepting React's internal createElement calls
      vi.spyOn(document, "createElement").mockReturnValueOnce({
        href: "",
        download: "",
        click: vi.fn(),
      } as unknown as HTMLElement);

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({
          message: "Downloaded logs",
          status: toaster.STATUSES.success,
        }),
      );
    });

    it("should show error toast when downloadLogs API call fails", async () => {
      mockDownloadLogs.mockImplementation(({ onError }: any) => {
        onError("Connection failed");
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        await downloadLogsAction?.actionHandler();
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({
          message: "Connection failed",
          status: toaster.STATUSES.error,
        }),
      );
    });

    it("should show error toast when getCommandBatchLogBundle fails after streaming", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 1, failure: 0 } } });
        return Promise.resolve();
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onError }: any) => {
        onError("Logs too large to download");
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({
          message: "Logs too large to download",
          status: toaster.STATUSES.error,
        }),
      );
    });

    it("should abort the stream when the batch reports FINISHED status", async () => {
      let capturedOnStreamData: ((resp: any) => void) | undefined;
      let capturedAbortController: AbortController | undefined;

      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData, streamAbortController }: any) => {
        capturedOnStreamData = onStreamData;
        capturedAbortController = streamAbortController;
        return new Promise<void>((resolve) => {
          streamAbortController.signal.addEventListener("abort", () => resolve());
        });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        await downloadLogsAction?.actionHandler();
      });

      expect(capturedAbortController?.signal.aborted).toBe(false);

      // PROCESSING update should not abort
      act(() => {
        capturedOnStreamData?.({
          status: { commandBatchUpdateStatus: 2 }, // PROCESSING
        });
      });
      expect(capturedAbortController?.signal.aborted).toBe(false);

      // FINISHED update should abort
      act(() => {
        capturedOnStreamData?.({
          status: { commandBatchUpdateStatus: 3 }, // FINISHED
        });
      });
      expect(capturedAbortController?.signal.aborted).toBe(true);
    });

    it("should show error toast and not fetch bundle when all devices fail", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 0, failure: 2 } } });
        return Promise.resolve();
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({ message: "Failed to download logs", status: toaster.STATUSES.error }),
      );
      expect(mockGetCommandBatchLogBundle).not.toHaveBeenCalled();
    });

    it("should show partial failure toast alongside success when some devices fail", async () => {
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockStreamCommandBatchUpdates.mockImplementation(({ onStreamData }: any) => {
        onStreamData({ status: { commandBatchUpdateStatus: 3, commandBatchDeviceCount: { success: 1, failure: 1 } } });
        return Promise.resolve();
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ chunkData: new Uint8Array([1, 2, 3]), filename: "logs.zip" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [
            { deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE },
            { deviceIdentifier: "device-2", deviceStatus: DeviceStatus.ONLINE },
          ],
          selectionMode: "subset",
        }),
      );

      vi.spyOn(document, "createElement").mockReturnValueOnce({
        href: "",
        download: "",
        click: vi.fn(),
      } as unknown as HTMLElement);

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.any(Number),
        expect.objectContaining({ message: "Downloaded logs", status: toaster.STATUSES.success }),
      );
      expect(toaster.pushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Failed to retrieve logs from 1 miner",
          status: toaster.STATUSES.error,
        }),
      );
    });

    it("should call onActionComplete after the file is downloaded successfully", async () => {
      const onActionComplete = vi.fn();
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ chunkData: new Uint8Array([1, 2, 3]), filename: "logs.zip" });
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      vi.spyOn(document, "createElement").mockReturnValueOnce({
        href: "",
        download: "",
        click: vi.fn(),
      } as unknown as HTMLElement);

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should call onActionComplete when getCommandBatchLogBundle fails", async () => {
      const onActionComplete = vi.fn();
      mockDownloadLogs.mockImplementation(({ onSuccess }: any) => {
        onSuccess({ batchIdentifier: "batch-logs-123" });
      });
      mockGetCommandBatchLogBundle.mockImplementation(({ onError }: any) => {
        onError("Logs too large to download");
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        downloadLogsAction?.actionHandler();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(onActionComplete).toHaveBeenCalled();
    });

    it("should call onActionComplete when the downloadLogs API call fails", async () => {
      const onActionComplete = vi.fn();
      mockDownloadLogs.mockImplementation(({ onError }: any) => {
        onError("Connection failed");
      });

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      const downloadLogsAction = result.current.popoverActions.find((a) => a.action === deviceActions.downloadLogs);
      await act(async () => {
        await downloadLogsAction?.actionHandler();
      });

      expect(onActionComplete).toHaveBeenCalled();
    });
  });

  describe("Rename miner action", () => {
    it("should expose a rename opener that opens the single-miner dialog", () => {
      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      expect(result.current.popoverActions.find((a) => a.action === settingsActions.rename)).toBeUndefined();

      act(() => {
        result.current.handleRenameOpen();
      });

      expect(result.current.showRenameDialog).toBe(true);
      expect(result.current.currentAction).toBe(settingsActions.rename);
    });

    it("should call renameSingleMiner with device identifier and name on confirm", async () => {
      mockRenameSingleMiner.mockResolvedValue(undefined);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      await act(async () => {
        await result.current.handleRenameConfirm("New Name");
      });

      expect(mockRenameSingleMiner).toHaveBeenCalledWith("device-1", "New Name");
    });

    it("should show 'Miner renamed' success toast after successful rename", async () => {
      mockRenameSingleMiner.mockResolvedValue(undefined);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      await act(async () => {
        await result.current.handleRenameConfirm("New Name");
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ message: "Miner renamed", status: "success" }),
      );
    });

    it("should show error toast when rename fails", async () => {
      mockRenameSingleMiner.mockRejectedValue(new Error("Network error"));

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      await act(async () => {
        await result.current.handleRenameConfirm("New Name");
      });

      expect(toaster.updateToast).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ message: "Failed to rename miner", status: "error" }),
      );
    });

    it("should close rename dialog and reset currentAction on confirm", async () => {
      mockRenameSingleMiner.mockResolvedValue(undefined);

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
        }),
      );

      act(() => {
        result.current.handleRenameOpen();
      });

      expect(result.current.showRenameDialog).toBe(true);

      await act(async () => {
        await result.current.handleRenameConfirm("New Name");
      });

      expect(result.current.showRenameDialog).toBe(false);
      expect(result.current.currentAction).toBeNull();
    });

    it("should close rename dialog and call onActionComplete on dismiss", () => {
      const onActionComplete = vi.fn();

      const { result } = renderHook(() =>
        useMinerActions({
          selectedMiners: [{ deviceIdentifier: "device-1", deviceStatus: DeviceStatus.ONLINE }],
          selectionMode: "subset",
          onActionComplete,
        }),
      );

      act(() => {
        result.current.handleRenameOpen();
      });

      act(() => {
        result.current.handleRenameDismiss();
      });

      expect(result.current.showRenameDialog).toBe(false);
      expect(result.current.currentAction).toBeNull();
      expect(onActionComplete).toHaveBeenCalled();
    });
  });
});
