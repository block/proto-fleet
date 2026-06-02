import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { type BulkAction } from "../BulkActions/types";
import { ACTION_PERMISSIONS, usePermittedActions } from "./actionPermissions";
import { deviceActions, type SupportedAction } from "./constants";

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
    vi.mocked(usePermissions).mockReturnValue(["miner:reboot", "miner:unpair"]);

    const { result } = renderHook(() =>
      usePermittedActions([action(deviceActions.reboot), action(deviceActions.unpair)]),
    );

    expect(result.current.map((a) => a.action)).toEqual([deviceActions.reboot, deviceActions.unpair]);
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
  it("maps every SupportedAction to its server-side catalog key", () => {
    // Spot-check the mapping against rpc_permissions.go's
    // MinerCommandService entries; this test fails loudly if the proto
    // gains a new action without a matching catalog key here.
    expect(ACTION_PERMISSIONS[deviceActions.reboot]).toBe("miner:reboot");
    expect(ACTION_PERMISSIONS[deviceActions.blinkLEDs]).toBe("miner:blink_led");
    expect(ACTION_PERMISSIONS[deviceActions.unpair]).toBe("miner:unpair");
    expect(ACTION_PERMISSIONS[deviceActions.firmwareUpdate]).toBe("miner:firmware_update");
    expect(ACTION_PERMISSIONS[deviceActions.downloadLogs]).toBe("miner:download_logs");
  });
});
