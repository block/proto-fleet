import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { type BulkAction } from "../BulkActions/types";
import { ACTION_PERMISSIONS, usePermittedActions } from "./actionPermissions";
import { deviceActions, performanceActions, settingsActions, type SupportedAction } from "./constants";

vi.mock("@/protoFleet/store", () => ({
  usePermissions: vi.fn(),
}));

import { usePermissions } from "@/protoFleet/store";

const action = (a: SupportedAction): BulkAction<SupportedAction> => ({
  action: a,
  title: a,
  icon: null,
  actionHandler: () => {},
  requiresConfirmation: false,
});

describe("usePermittedActions", () => {
  it("filters out actions whose required catalog key is missing", () => {
    vi.mocked(usePermissions).mockReturnValue(["miner:reboot"]);

    const { result } = renderHook(() =>
      usePermittedActions([action(deviceActions.reboot), action(deviceActions.unpair)]),
    );

    expect(result.current.map((a) => a.action)).toEqual([deviceActions.reboot]);
  });

  it("keeps actions whose required key is granted", () => {
    vi.mocked(usePermissions).mockReturnValue(["miner:reboot", "miner:delete"]);

    const { result } = renderHook(() =>
      usePermittedActions([action(deviceActions.reboot), action(deviceActions.unpair)]),
    );

    expect(result.current.map((a) => a.action)).toEqual([deviceActions.reboot, deviceActions.unpair]);
  });

  it("requires every key when the mapping is an AND-list", () => {
    // miningPool needs both miner:update_pools and pool:read; holding
    // only one is insufficient because the pool-selection flow calls
    // ListPools before the miner-side write.
    vi.mocked(usePermissions).mockReturnValue(["miner:update_pools"]);
    const onlyMinerKey = renderHook(() => usePermittedActions([action(settingsActions.miningPool)]));
    expect(onlyMinerKey.result.current).toEqual([]);

    vi.mocked(usePermissions).mockReturnValue(["miner:update_pools", "pool:read"]);
    const both = renderHook(() => usePermittedActions([action(settingsActions.miningPool)]));
    expect(both.result.current.map((a) => a.action)).toEqual([settingsActions.miningPool]);

    // Firmware update needs miner:reboot too because successful installs
    // automatically reboot the miner after activation.
    vi.mocked(usePermissions).mockReturnValue(["miner:firmware_update"]);
    const firmwareWithoutReboot = renderHook(() => usePermittedActions([action(deviceActions.firmwareUpdate)]));
    expect(firmwareWithoutReboot.result.current).toEqual([]);

    vi.mocked(usePermissions).mockReturnValue(["miner:firmware_update", "miner:reboot"]);
    const firmwareWithReboot = renderHook(() => usePermittedActions([action(deviceActions.firmwareUpdate)]));
    expect(firmwareWithReboot.result.current.map((a) => a.action)).toEqual([deviceActions.firmwareUpdate]);
  });

  it("passes through actions without a mapped permission (e.g. viewMiner)", () => {
    vi.mocked(usePermissions).mockReturnValue([]);

    const viewMiner: BulkAction<"viewMiner"> = {
      action: "viewMiner",
      title: "View miner",
      icon: null,
      actionHandler: () => {},
      requiresConfirmation: false,
    };

    const { result } = renderHook(() => usePermittedActions([viewMiner]));

    expect(result.current.map((a) => a.action)).toEqual(["viewMiner"]);
  });

  it("hides every action when permissions are empty", () => {
    // Pre-U10a sessions and FIELD_TECH-without-miner-permissions both
    // hit this path; the menu collapses to nothing rather than showing
    // controls that 403.
    vi.mocked(usePermissions).mockReturnValue([]);

    const { result } = renderHook(() =>
      usePermittedActions([action(deviceActions.reboot), action(deviceActions.blinkLEDs)]),
    );

    expect(result.current).toEqual([]);
  });
});

describe("ACTION_PERMISSIONS", () => {
  it("anchors well-known actions to their server-side catalog keys", () => {
    // ACTION_PERMISSIONS is typed `Record<SupportedAction, ...>`, so
    // exhaustiveness is enforced at compile time — adding a new
    // SupportedAction without an entry fails tsc. This spot-check
    // anchors a few high-traffic mappings against rpc_permissions.go
    // so a wire-misalignment lands in code review instead of an
    // operator's lap.
    expect(ACTION_PERMISSIONS[deviceActions.reboot]).toBe("miner:reboot");
    expect(ACTION_PERMISSIONS[deviceActions.blinkLEDs]).toBe("miner:blink_led");
    // Unpair routes through FleetManagementService.DeleteMiners
    // (miner:delete) on the server, not MinerCommandService.Unpair.
    expect(ACTION_PERMISSIONS[deviceActions.unpair]).toBe("miner:delete");
    expect(ACTION_PERMISSIONS[deviceActions.firmwareUpdate]).toEqual(["miner:firmware_update", "miner:reboot"]);
    expect(ACTION_PERMISSIONS[deviceActions.downloadLogs]).toBe("miner:download_logs");
    expect(ACTION_PERMISSIONS[performanceActions.curtail]).toBe("curtailment:manage");
    expect(ACTION_PERMISSIONS[settingsActions.miningPool]).toEqual(["miner:update_pools", "pool:read"]);
  });
});
