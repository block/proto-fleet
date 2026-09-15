import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MinerIssues from "./MinerIssues";
import { ComponentType, type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceOfflineReason,
  DeviceStatus,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

function createMockMiner(overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot {
  return {
    deviceIdentifier: "test-device",
    deviceStatus: DeviceStatus.ONLINE,
    pairingStatus: PairingStatus.PAIRED,
    ...overrides,
  } as MinerStateSnapshot;
}

describe("MinerIssues", () => {
  it("shows only no connection when the assigned Fleet Node is unavailable", () => {
    const staleHardwareError = { componentType: ComponentType.HASH_BOARD } as ErrorMessage;
    const { container } = render(
      <MinerIssues
        miner={createMockMiner({
          deviceStatus: DeviceStatus.OFFLINE,
          offlineReason: DeviceOfflineReason.FLEET_NODE_UNAVAILABLE,
        })}
        errors={[staleHardwareError]}
        errorsLoaded={false}
      />,
    );

    expect(screen.getByText("No connection")).toBeInTheDocument();
    expect(screen.queryByTestId("skeleton-bar")).not.toBeInTheDocument();
    expect(container.querySelector("svg")).toBeNull();
  });
});
