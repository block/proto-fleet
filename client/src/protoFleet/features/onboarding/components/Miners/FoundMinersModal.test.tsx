import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import FoundMinersModal from "./FoundMinersModal";
import { createScaleDiscoveryMiners, scaleFleetNodes } from "./scaleDiscoveryFixture";
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
      miners={devices}
      deselectedMinerIds={new Set(deselected)}
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

function LargeScan() {
  const [devices] = useState(createScaleDiscoveryMiners);
  const [deselected, setDeselected] = useState<string[]>([]);
  const deselectedIds = new Set(deselected);
  return (
    <FoundMinersModal
      open
      miners={devices}
      deselectedMinerIds={deselectedIds}
      fleetNodes={scaleFleetNodes}
      setDeselectedMiners={setDeselected}
      onDismiss={() => {}}
    />
  );
}

describe("large discovery selection", () => {
  it("bounds 50,000 devices to 100 rows and preserves selection between pages and filtered bulk changes", async () => {
    const user = userEvent.setup();
    render(<LargeScan />);
    const minerRegion = () => screen.getByRole("region", { name: "Miners" });
    const checks = () => within(minerRegion()).getAllByRole("checkbox");
    expect(checks()).toHaveLength(101);
    expect(screen.getByText("Showing 1–100 of 50000 devices")).toBeInTheDocument();
    expect(screen.getByText("50000 devices selected")).toBeInTheDocument();
    expect(screen.getAllByText("Mining hall 1")).toHaveLength(100);
    await user.click(checks()[1]);
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.getByText("Showing 101–200 of 50000 devices")).toBeInTheDocument();
    await user.click(checks()[1]);
    expect(screen.getByText("49998 devices selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Previous page" }));
    expect(checks()[1]).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: /^Select none$/ }));
    expect(screen.getByText("0 devices selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^Select all$/ }));
    expect(screen.getByText("50000 devices selected")).toBeInTheDocument();
    await user.click(screen.getByTestId("filter-dropdown-Model"));
    await user.click(screen.getByTestId("filter-option-model:S21 Pro"));
    await user.click(screen.getByRole("button", { name: /^Select none$/ }));
    expect(screen.getByText("25000 devices selected")).toBeInTheDocument();
    expect(checks()).toHaveLength(101);
    expect(screen.getByText("Showing 1–100 of 25000 devices")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^Select all$/ }));
    expect(screen.getByText("50000 devices selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(
      checks()
        .slice(1)
        .every((checkbox) => (checkbox as HTMLInputElement).checked),
    ).toBe(true);
    await user.click(screen.getByRole("button", { name: /^Select none$/ }));
    expect(screen.getByText("25000 devices selected")).toBeInTheDocument();
  });
});
