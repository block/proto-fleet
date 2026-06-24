import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import InfraDeviceList from "./InfraDeviceList";
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

describe("InfraDeviceList", () => {
  test("syncs rows when devices prop changes", async () => {
    const { rerender } = render(<InfraDeviceList devices={[]} />);

    expect(screen.getByText("0 devices")).toBeInTheDocument();

    rerender(<InfraDeviceList devices={[device]} />);

    await waitFor(() => expect(screen.getByText("Roof exhaust")).toBeInTheDocument());
    expect(screen.getByText("Fan group (12 fans)")).toBeInTheDocument();
    expect(screen.getByText("1 device")).toBeInTheDocument();
  });
});
