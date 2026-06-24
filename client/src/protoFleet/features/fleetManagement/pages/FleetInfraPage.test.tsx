import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import FleetInfraPage from "./FleetInfraPage";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

const device: InfraDeviceItem = {
  id: "aus-b1-roof-exhaust",
  name: "Roof exhaust",
  buildingName: "Building 1",
  siteName: "Austin",
  connectionType: "modbus_tcp",
  endpoint: "10.12.1.21",
  port: 502,
  status: "offline",
  enabled: "auto",
  lastSeen: "Never",
  endpointKind: "fan_group",
  fanCount: 12,
};

describe("FleetInfraPage", () => {
  test("does not expose local-only management controls by default", () => {
    render(<FleetInfraPage devices={[device]} />);

    expect(screen.queryByRole("button", { name: "Add device" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for Roof exhaust" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeDisabled();
  });

  test("allows stories or future persistence-backed callers to opt into management controls", () => {
    render(<FleetInfraPage devices={[device]} canManage />);

    expect(screen.getByRole("button", { name: "Add device" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeEnabled();
  });
});
