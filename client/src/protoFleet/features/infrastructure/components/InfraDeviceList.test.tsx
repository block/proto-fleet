import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import InfraDeviceList from "./InfraDeviceList";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
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

const getDeviceNameOrder = (names: string[]) =>
  screen
    .getAllByRole("button")
    .map((button) => button.textContent ?? "")
    .filter((text) => names.includes(text));

describe("InfraDeviceList", () => {
  test("syncs rows when devices prop changes", async () => {
    const { rerender } = render(<InfraDeviceList devices={[]} />);

    expect(screen.getByText("0 devices")).toBeInTheDocument();

    rerender(<InfraDeviceList devices={[device]} />);

    await waitFor(() => expect(screen.getByText("Roof exhaust")).toBeInTheDocument());
    expect(screen.getByText("Fan group (12 fans)")).toBeInTheDocument();
    expect(screen.getByText("1 device")).toBeInTheDocument();
  });

  test("constrains pagination footer to the page-scroll chrome width", () => {
    const devices = Array.from({ length: 51 }, (_, index) => ({
      ...device,
      id: `device-${index + 1}`,
      name: `Device ${index + 1}`,
    }));

    render(<InfraDeviceList devices={devices} />);

    expect(screen.getByTestId("infra-devices-pagination")).toHaveClass(...PAGE_SCROLL_CHROME_WIDTH.split(" "));
  });

  test("sorts last seen by age instead of display label", () => {
    const devices = [
      { ...device, id: "older", name: "Older exhaust", lastSeen: "1h ago" },
      { ...device, id: "recent", name: "Recent exhaust", lastSeen: "2 min ago" },
      { ...device, id: "never", name: "Never seen exhaust", lastSeen: "Never" },
      { ...device, id: "current", name: "Current exhaust", lastSeen: "Just now" },
    ];
    const deviceNames = devices.map((infraDevice) => infraDevice.name);

    render(<InfraDeviceList devices={devices} />);

    fireEvent.click(screen.getByRole("button", { name: "Last seen" }));
    expect(getDeviceNameOrder(deviceNames)).toEqual([
      "Current exhaust",
      "Recent exhaust",
      "Older exhaust",
      "Never seen exhaust",
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Last seen" }));
    expect(getDeviceNameOrder(deviceNames)).toEqual([
      "Never seen exhaust",
      "Older exhaust",
      "Recent exhaust",
      "Current exhaust",
    ]);
  });
});
