import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { selectAllFacilityFanDeviceIds } from "@/protoFleet/features/energy/facilityFanSelection";
import FacilityFanSelectionModal, {
  type FacilityFanDeviceOption,
} from "@/protoFleet/features/energy/FacilityFanSelectionModal";

function facilityFanDevices(count: number): FacilityFanDeviceOption[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `${index + 1}`,
    siteId: "101",
    siteName: "Austin, TX",
    buildingName: "Building 1",
    rackName: "Rack A1",
    name: `Fan ${index + 1}`,
    deviceKind: "single_fan",
    enabled: true,
  }));
}

describe("FacilityFanSelectionModal", () => {
  it("caps Select all at the response profile fan limit", () => {
    const selectedDeviceIds = selectAllFacilityFanDeviceIds(
      ["1"],
      facilityFanDevices(9).map(({ id }) => id),
    );

    expect([...selectedDeviceIds]).toHaveLength(8);
    expect(selectedDeviceIds).toContain("1");
    expect(selectedDeviceIds).toContain("8");
    expect(selectedDeviceIds).not.toContain("9");
  });

  it("selects all available devices and applies them", () => {
    const onApply = vi.fn();
    render(
      <FacilityFanSelectionModal
        devices={facilityFanDevices(2)}
        initialSelectedDeviceIds={[]}
        initialFanOffDelaySec=""
        initialFanRestoreDelaySec=""
        onDismiss={vi.fn()}
        onApply={onApply}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Select all" }));

    expect(screen.getByText("2 devices selected")).toBeInTheDocument();
    expect(within(screen.getByTestId("facility-fan-device-1")).getByRole("checkbox")).toBeChecked();
    expect(within(screen.getByTestId("facility-fan-device-2")).getByRole("checkbox")).toBeChecked();
    expect(screen.getAllByText("Rack A1 · Building 1 · Austin, TX")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ selectedDeviceIds: ["1", "2"] }));
  });

  it("preserves an oversized legacy selection until the operator reduces it", () => {
    const onApply = vi.fn();
    const devices = facilityFanDevices(9);
    render(
      <FacilityFanSelectionModal
        devices={devices}
        initialSelectedDeviceIds={devices.map(({ id }) => id)}
        initialFanOffDelaySec=""
        initialFanRestoreDelaySec=""
        onDismiss={vi.fn()}
        onApply={onApply}
      />,
    );

    expect(screen.getByText("9 devices selected (maximum)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Select all" }));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ selectedDeviceIds: devices.map(({ id }) => id) }));
  });

  it("rejects fan delays above the server limit", () => {
    const onApply = vi.fn();
    render(
      <FacilityFanSelectionModal
        devices={facilityFanDevices(1)}
        initialSelectedDeviceIds={["1"]}
        initialFanOffDelaySec="3601"
        initialFanRestoreDelaySec="3601"
        onDismiss={vi.fn()}
        onApply={onApply}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(screen.getByText("Enter fan-off delay of 3,600 or less.")).toBeInTheDocument();
    expect(screen.getByText("Enter fan restore delay of 3,600 or less.")).toBeInTheDocument();
    expect(onApply).not.toHaveBeenCalled();
  });
});
