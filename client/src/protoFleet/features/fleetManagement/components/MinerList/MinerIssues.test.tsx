import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import MinerIssues from "./MinerIssues";
import {
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const createMiner = (overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot =>
  ({
    deviceIdentifier: "miner-1",
    deviceStatus: 0,
    pairingStatus: PairingStatus.PAIRED,
    ...overrides,
  }) as MinerStateSnapshot;

describe("MinerIssues", () => {
  it("does not show an issue for default-password miners without other issues", () => {
    const { container } = render(
      <MinerIssues miner={createMiner({ pairingStatus: PairingStatus.DEFAULT_PASSWORD })} errors={[]} errorsLoaded />,
    );

    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText(/password/i)).not.toBeInTheDocument();
  });
});
