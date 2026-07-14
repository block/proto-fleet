import { render, screen, waitFor } from "@testing-library/react";
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

const driverConfig = JSON.stringify({
  endpoint: "10.12.1.21",
  port: 502,
  unit_id: 17,
  register_address: 2001,
  write_mode: "coil",
});

const device: InfraDeviceItem = {
  id: "101",
  siteId: "8",
  siteName: "Austin",
  buildingName: "Building 1",
  name: "Roof exhaust",
  deviceKind: "fan_group",
  fanCount: 12,
  enabled: true,
  driverType: "modbus_tcp",
  driverConfig,
};

const buildingOptions: InfraBuildingOption[] = [
  { siteName: "Austin", buildingName: "Building 1" },
  { siteName: "Austin", buildingName: "Building 10" },
  { siteName: "Denver", buildingName: "Denver Plant" },
];

const renderModal = ({
  onSave = vi.fn().mockResolvedValue(undefined),
  onDelete = vi.fn().mockResolvedValue(undefined),
  onDismiss = vi.fn(),
  targetDevice = device,
  canManage = true,
} = {}) => {
  render(
    <InfraDeviceDetailModal
      device={targetDevice}
      siteOptions={["Austin", "Denver"]}
      buildingOptions={buildingOptions}
      canManage={canManage}
      onSave={onSave}
      onDelete={onDelete}
      onDismiss={onDismiss}
    />,
  );

  return { onSave, onDelete, onDismiss };
};

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
    const { onSave, onDismiss } = renderModal();

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
    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
  });

  test("edits Modbus connection fields through the driver form module", async () => {
    const user = userEvent.setup();
    const { onSave } = renderModal();

    expect(screen.getByLabelText("Connection type")).toHaveValue("Modbus TCP");
    expect(screen.getByLabelText("Endpoint")).toHaveValue("10.12.1.21");

    await user.clear(screen.getByLabelText("Endpoint"));
    await user.type(screen.getByLabelText("Endpoint"), "10.12.9.9");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledTimes(1);
    const update = onSave.mock.calls[0][0];
    expect(update).toMatchObject({ id: "101", driverType: "modbus_tcp" });
    expect(update.enabled).toBeUndefined();
    expect(JSON.parse(update.driverConfig)).toEqual({
      endpoint: "10.12.9.9",
      port: 502,
      unit_id: 17,
      register_address: 2001,
      write_mode: "coil",
    });
  });

  test("preserves an unknown driver's raw config on save", async () => {
    const user = userEvent.setup();
    const unknownDriverDevice: InfraDeviceItem = {
      ...device,
      driverType: "mqtt_bridge",
      driverConfig: '{"topic":"fans/roof"}',
    };
    const { onSave } = renderModal({ targetDevice: unknownDriverDevice });

    expect(screen.getByLabelText("Connection type")).toHaveValue("mqtt_bridge");
    expect(screen.queryByLabelText("Endpoint")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        driverType: "mqtt_bridge",
        driverConfig: '{"topic":"fans/roof"}',
      }),
    );
  });

  test("keeps the modal open and shows the RPC failure inline on save", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockRejectedValue(new Error("driver_config field port must be a valid int"));
    const { onDismiss } = renderModal({ onSave });

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByTestId("infra-device-action-error")).toHaveTextContent(
      "driver_config field port must be a valid int",
    );
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  test("degrades to a connection summary row when the config is redacted", () => {
    const redactedDevice: InfraDeviceItem = { ...device, driverConfig: "" };
    renderModal({ targetDevice: redactedDevice, canManage: false });

    expect(screen.queryByLabelText("Endpoint")).not.toBeInTheDocument();
    expect(screen.getByText("Connection")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  test("deletes through the callback and dismisses on success", async () => {
    const user = userEvent.setup();
    const { onDelete, onDismiss } = renderModal();

    await user.click(screen.getByRole("button", { name: "Delete" }));

    expect(onDelete).toHaveBeenCalledWith("101");
    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
  });

  test("a name-only save omits enabled so the server preserves it", async () => {
    const user = userEvent.setup();
    const { onSave } = renderModal();

    await user.clear(screen.getByLabelText("Name"));
    await user.type(screen.getByLabelText("Name"), "Roof exhaust renamed");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: "Roof exhaust renamed", enabled: undefined }));
  });

  test("flipping the switch then saving includes the new enabled value", async () => {
    const user = userEvent.setup();
    const { onSave } = renderModal();

    await user.click(screen.getByRole("checkbox", { name: "Enabled" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
  });
});
