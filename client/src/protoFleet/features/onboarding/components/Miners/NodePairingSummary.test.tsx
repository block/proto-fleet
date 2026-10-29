import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import NodePairingSummary from "./NodePairingSummary";
import { scaleFleetNodes } from "./scaleDiscoveryFixture";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

const miners = [
  create(DeviceSchema, { deviceIdentifier: "one", fleetNodeId: 1n }),
  create(DeviceSchema, { deviceIdentifier: "two", fleetNodeId: 1n }),
];

describe("pairing capacity guidance", () => {
  it.each([
    [3998, null],
    [3999, "Above the 4,000-miner target"],
    [4998, "Above the 4,000-miner target"],
    [4999, "Exceeds 5,000 miners per Fleet Node"],
  ])("combines %i paired miners with the two selected miners", (pairedDeviceCount, warning) => {
    render(
      <NodePairingSummary
        miners={miners}
        fleetNodes={[{ ...scaleFleetNodes[0], pairedDeviceCount }]}
        showPairedCounts
      />,
    );
    expect(
      screen.getByText(
        `${pairedDeviceCount.toLocaleString()} paired + 2 selected = ${(pairedDeviceCount + 2).toLocaleString()} total`,
      ),
    ).toBeInTheDocument();
    if (warning) expect(screen.getByText(warning)).toBeInTheDocument();
    else expect(screen.queryByText(/Above|Exceeds/)).not.toBeInTheDocument();
  });

  it("summarizes only sources with selected devices", () => {
    const { rerender } = render(<NodePairingSummary miners={miners} fleetNodes={scaleFleetNodes} showPairedCounts />);
    expect(screen.getByText("Mining hall 1")).toBeInTheDocument();
    expect(screen.queryByText("Mining hall 2")).not.toBeInTheDocument();
    rerender(<NodePairingSummary miners={[]} fleetNodes={scaleFleetNodes} showPairedCounts />);
    expect(screen.queryByRole("region", { name: "Pairing by source" })).not.toBeInTheDocument();
  });

  it("keeps redacted paired totals and node names unavailable", () => {
    render(<NodePairingSummary miners={miners} fleetNodes={scaleFleetNodes} showPairedCounts={false} />);
    expect(screen.getByText("Paired count unavailable + 2 selected")).toBeInTheDocument();
    expect(screen.queryByText("Mining hall 1")).not.toBeInTheDocument();
    expect(screen.queryByText(/0 paired/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Above|Exceeds/)).not.toBeInTheDocument();
  });
});
