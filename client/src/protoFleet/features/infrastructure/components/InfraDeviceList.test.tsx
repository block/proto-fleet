import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import InfraDeviceList from "./InfraDeviceList";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error" },
}));

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
  driverConfig: JSON.stringify({
    endpoint: "10.12.1.21",
    port: 502,
    unit_id: 17,
    register_address: 2001,
    write_mode: "coil",
  }),
};

describe("InfraDeviceList", () => {
  beforeEach(() => {
    vi.mocked(pushToast).mockReset();
  });

  test("syncs rows when devices prop changes", async () => {
    const { rerender } = render(<InfraDeviceList devices={[]} />);

    expect(screen.getByText("0 devices")).toBeInTheDocument();

    rerender(<InfraDeviceList devices={[device]} />);

    await waitFor(() => expect(screen.getByText("Roof exhaust")).toBeInTheDocument());
    expect(screen.getByText("Fan group (12 fans)")).toBeInTheDocument();
    expect(screen.getByText("1 device")).toBeInTheDocument();
  });

  test("renders the driver connection summary and degrades when redacted", () => {
    const redactedDevice: InfraDeviceItem = {
      ...device,
      id: "102",
      name: "Redacted exhaust",
      driverConfig: "",
    };

    render(<InfraDeviceList devices={[device, redactedDevice]} />);

    expect(screen.getByText("10.12.1.21:502 · unit 17 · coil 2001")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  test("shows the loading state while the list fetch is in flight", () => {
    render(<InfraDeviceList isLoading />);

    expect(screen.getByTestId("infra-devices-loading")).toBeInTheDocument();
    expect(screen.queryByText("0 devices")).not.toBeInTheDocument();
  });

  test("shows the load error with a retry action", async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();

    render(<InfraDeviceList loadError="boom" onRetry={onRetry} />);

    expect(screen.getByTestId("infra-devices-load-error")).toHaveTextContent("boom");

    await user.click(screen.getByRole("button", { name: "Retry" }));

    expect(onRetry).toHaveBeenCalled();
  });

  test("toggles enabled through the callback", async () => {
    const user = userEvent.setup();
    const onSetDeviceEnabled = vi.fn().mockResolvedValue(undefined);

    render(<InfraDeviceList devices={[device]} onSetDeviceEnabled={onSetDeviceEnabled} />);

    await user.click(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" }));

    expect(onSetDeviceEnabled).toHaveBeenCalledWith(device, false);
    expect(pushToast).not.toHaveBeenCalled();
  });

  test("surfaces an enabled-toggle failure as an error toast", async () => {
    const user = userEvent.setup();
    const onSetDeviceEnabled = vi.fn().mockRejectedValue(new Error("update failed"));

    render(<InfraDeviceList devices={[device]} onSetDeviceEnabled={onSetDeviceEnabled} />);

    await user.click(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" }));

    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith(expect.objectContaining({ message: "update failed", status: "error" })),
    );
    // The switch still reflects the device prop, so a failed toggle
    // visually reverts once the rejection lands.
    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeChecked();
  });

  test("disables the enabled switch while its device update is in flight", () => {
    render(<InfraDeviceList devices={[device]} updatingDeviceIds={new Set([device.id])} />);

    expect(screen.getByRole("checkbox", { name: "Enabled for Roof exhaust" })).toBeDisabled();
  });

  test("requires confirmation before a row-menu delete fires the RPC", async () => {
    const user = userEvent.setup();
    const onDeleteDevice = vi.fn().mockResolvedValue(undefined);

    render(<InfraDeviceList devices={[device]} onDeleteDevice={onDeleteDevice} />);

    await user.click(screen.getByRole("button", { name: "Actions for Roof exhaust" }));
    await user.click(await screen.findByText("Delete"));

    // The dialog names exactly what is being removed; nothing has been
    // deleted yet.
    expect(onDeleteDevice).not.toHaveBeenCalled();
    expect(screen.getByTestId("infra-device-delete-dialog")).toHaveTextContent("Roof exhaust");
    expect(screen.getByTestId("infra-device-delete-dialog")).toHaveTextContent("Building 1");
    expect(screen.getByTestId("infra-device-delete-dialog")).toHaveTextContent("Austin");

    await user.click(screen.getByRole("button", { name: "Delete device" }));

    expect(onDeleteDevice).toHaveBeenCalledWith("101");
    await waitFor(() => expect(screen.queryByTestId("infra-device-delete-dialog")).not.toBeInTheDocument());
  });

  test("cancelling the delete confirmation leaves the device untouched", async () => {
    const user = userEvent.setup();
    const onDeleteDevice = vi.fn();

    render(<InfraDeviceList devices={[device]} onDeleteDevice={onDeleteDevice} />);

    await user.click(screen.getByRole("button", { name: "Actions for Roof exhaust" }));
    await user.click(await screen.findByText("Delete"));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onDeleteDevice).not.toHaveBeenCalled();
    expect(screen.queryByTestId("infra-device-delete-dialog")).not.toBeInTheDocument();
  });

  test("disables the row Edit and Delete actions while the device update is in flight", async () => {
    const user = userEvent.setup();
    render(<InfraDeviceList devices={[device]} updatingDeviceIds={new Set([device.id])} />);

    await user.click(screen.getByRole("button", { name: "Actions for Roof exhaust" }));

    expect((await screen.findByText("Edit")).closest("button")).toBeDisabled();
    expect(screen.getByText("Delete").closest("button")).toBeDisabled();
  });

  test("constrains pagination footer to the page-scroll chrome width", () => {
    const devices = Array.from({ length: 51 }, (_, index) => ({
      ...device,
      id: `${index + 1}`,
      name: `Device ${index + 1}`,
    }));

    render(<InfraDeviceList devices={devices} />);

    expect(screen.getByTestId("infra-devices-pagination")).toHaveClass(...PAGE_SCROLL_CHROME_WIDTH.split(" "));
  });
});
