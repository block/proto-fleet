import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import FoundMiners from "./FoundMiners";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

describe("FoundMiners", () => {
  it("groups and counts identical model names by their reported class", () => {
    const miners = [
      create(DeviceSchema, {
        deviceIdentifier: "container",
        manufacturer: "Proto",
        model: "CU1",
        deviceClass: DeviceClass.CONTAINER,
      }),
      create(DeviceSchema, {
        deviceIdentifier: "rig",
        manufacturer: "Proto",
        model: "CU1",
        deviceClass: DeviceClass.RIG,
      }),
    ];
    render(<FoundMiners miners={miners} deselectedMiners={["rig"]} />);

    expect(screen.getByText("1 container and 1 miner found on your network")).toBeInTheDocument();
    const rows = screen.getAllByTestId("miner-model-row");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("Proto Container")).toBeInTheDocument();
    expect(within(rows[0]).getByText("1 container")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Proto CU1")).toBeInTheDocument();
    expect(within(rows[1]).getByText("0 miners")).toBeInTheDocument();
  });
});
