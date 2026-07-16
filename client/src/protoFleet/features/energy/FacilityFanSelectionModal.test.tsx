import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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
    const onApply = vi.fn();
    render(
      <FacilityFanSelectionModal
        devices={facilityFanDevices(1025)}
        initialSelectedDeviceIds={[]}
        initialFanOffDelaySec=""
        initialFanRestoreDelaySec=""
        onDismiss={vi.fn()}
        onApply={onApply}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Select all" }));

    expect(screen.getByText("1024 devices selected (maximum)")).toBeInTheDocument();
    expect(within(screen.getByTestId("facility-fan-device-1")).getByRole("checkbox")).toBeChecked();
    const overflowCheckbox = within(screen.getByTestId("facility-fan-device-1025")).getByRole("checkbox");
    expect(overflowCheckbox).toBeDisabled();
    expect(overflowCheckbox).not.toBeChecked();

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledWith(
      expect.objectContaining({
        selectedDeviceIds: expect.arrayContaining(["1", "1024"]),
      }),
    );
    expect(onApply.mock.calls[0]?.[0]?.selectedDeviceIds).toHaveLength(1024);
    expect(onApply.mock.calls[0]?.[0]?.selectedDeviceIds).not.toContain("1025");
  });
});
