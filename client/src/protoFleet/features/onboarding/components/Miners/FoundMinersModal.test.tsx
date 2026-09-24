import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import FoundMinersModal from "./FoundMinersModal";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

function MixedScan() {
  const [deselected, setDeselected] = useState<string[]>([]);
  const devices = [
    create(DeviceSchema, {
      deviceIdentifier: "ct1",
      model: "CT1",
      deviceClass: DeviceClass.CONTAINER,
      ipAddress: "192.0.2.1",
    }),
    create(DeviceSchema, {
      deviceIdentifier: "rig",
      model: "R2",
      deviceClass: DeviceClass.RIG,
      ipAddress: "192.0.2.2",
    }),
  ];
  return (
    <FoundMinersModal
      open
      miners={devices.map((device) => ({ ...device, selected: !deselected.includes(device.deviceIdentifier) }))}
      setDeselectedMiners={setDeselected}
      onDismiss={() => {}}
    />
  );
}

describe("mixed discovery selection", () => {
  it("keeps selection independent between container and miner sections", async () => {
    const user = userEvent.setup();
    render(<MixedScan />);
    expect(screen.getByText("1 container and 1 miner found on your network")).toBeInTheDocument();
    expect(screen.getByText("2 devices selected")).toBeInTheDocument();
    const containers = screen.getByRole("region", { name: "Containers" });
    const miners = screen.getByRole("region", { name: "Miners" });
    const containerChecks = within(containers).getAllByRole("checkbox");
    await user.click(containerChecks[containerChecks.length - 1]);
    expect(screen.getByText("1 device selected")).toBeInTheDocument();
    const minerChecks = within(miners).getAllByRole("checkbox");
    expect(minerChecks[minerChecks.length - 1]).toBeChecked();
  });
});
