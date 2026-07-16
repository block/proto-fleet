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
    name: `Fan ${index + 1}`,
    deviceKind: "single_fan",
    enabled: true,
  }));
}

describe("FacilityFanSelectionModal", () => {
  it("caps Select all at the response profile fan limit", () => {
    const selectedDeviceIds = selectAllFacilityFanDeviceIds(
      ["1"],
      facilityFanDevices(1025).map(({ id }) => id),
    );

    expect([...selectedDeviceIds]).toHaveLength(1024);
    expect(selectedDeviceIds).toContain("1");
    expect(selectedDeviceIds).toContain("1024");
    expect(selectedDeviceIds).not.toContain("1025");
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

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledWith(expect.objectContaining({ selectedDeviceIds: ["1", "2"] }));
  });
});
