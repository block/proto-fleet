import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import InfraDeviceDetailModal from "./InfraDeviceDetailModal";
import type { InfraBuildingOption, InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";

vi.mock("@/shared/components/Select", () => ({
  default: ({
    id,
    label,
    options,
    value,
    onChange,
    disabled,
  }: {
    id: string;
    label: string;
    options: { value: string; label: string }[];
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <label htmlFor={id}>
      {label}
      <select id={id} aria-label={label} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        <option value="" disabled hidden>
          {label}
        </option>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  ),
}));

const device: InfraDeviceItem = {
  id: "aus-b1-roof-exhaust",
  unitId: 17,
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

const buildingOptions: InfraBuildingOption[] = [
  { siteName: "Austin", buildingName: "Building 1" },
  { siteName: "Austin", buildingName: "Building 10" },
  { siteName: "Denver", buildingName: "Denver Plant" },
];

const renderModal = (onSave = vi.fn()) =>
  render(
    <InfraDeviceDetailModal
      device={device}
      siteOptions={["Austin", "Denver"]}
      buildingOptions={buildingOptions}
      onSave={onSave}
      onDelete={vi.fn()}
      onDismiss={vi.fn()}
    />,
  );

const getSelectOptionLabels = (label: string) =>
  Array.from(screen.getByRole("combobox", { name: label }).querySelectorAll("option")).map(
    (option) => option.textContent,
  );

describe("InfraDeviceDetailModal", () => {
  test("filters building choices to the selected site", async () => {
    renderModal();

    expect(getSelectOptionLabels("Building")).toContain("Building 1");
    expect(getSelectOptionLabels("Building")).toContain("Building 10");
    expect(getSelectOptionLabels("Building")).not.toContain("Denver Plant");
  });

  test("resets the selected building when the site changes", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    renderModal(onSave);

    await user.selectOptions(screen.getByRole("combobox", { name: "Site" }), "Denver");

    expect(getSelectOptionLabels("Building")).toContain("Denver Plant");
    expect(getSelectOptionLabels("Building")).not.toContain("Building 1");
    expect(screen.getByRole<HTMLSelectElement>("combobox", { name: "Building" }).value).toBe("Denver Plant");

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        siteName: "Denver",
        buildingName: "Denver Plant",
      }),
    );
  });
});
