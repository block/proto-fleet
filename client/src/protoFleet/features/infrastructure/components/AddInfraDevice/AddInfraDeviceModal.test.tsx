import { useEffect } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import AddInfraDeviceModal from "./AddInfraDeviceModal";
import type { ManualAddStepState } from "./ManualAddStep";
import type { InfraDeviceDraft } from "@/protoFleet/features/infrastructure/types";

const draft: InfraDeviceDraft = {
  name: "Roof exhaust",
  siteName: "Austin",
  buildingName: "Building 1",
  rackName: "Rack A1",
  deviceKind: "fan_group",
  fanCount: 12,
  driverType: "modbus_tcp",
  driverConfig: JSON.stringify({
    endpoint: "10.12.1.21",
    port: 502,
    unit_id: 17,
    register_address: 2001,
    write_mode: "coil",
  }),
};

vi.mock("./ManualAddStep", () => {
  const MockManualAddStep = ({
    onSuccess,
    onStateChange,
  }: {
    onSuccess: (device: InfraDeviceDraft) => void;
    onStateChange: (state: ManualAddStepState) => void;
  }) => {
    useEffect(() => {
      onStateChange({ canAdd: true, addHandler: () => onSuccess(draft) });
    }, [onStateChange, onSuccess]);
    return <div data-testid="manual-add-step" />;
  };
  return { default: MockManualAddStep };
});

describe("AddInfraDeviceModal", () => {
  test("submits the draft and leaves closing to the caller on success", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);

    render(<AddInfraDeviceModal onDismiss={vi.fn()} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));

    expect(onSubmit).toHaveBeenCalledWith(draft);
    await waitFor(() => expect(screen.queryByTestId("infra-device-action-error")).not.toBeInTheDocument());
  });

  test("keeps the modal open and shows the RPC failure inline", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockRejectedValue(new Error("site not found"));

    render(<AddInfraDeviceModal onDismiss={vi.fn()} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));

    expect(await screen.findByTestId("infra-device-action-error")).toHaveTextContent("site not found");
    expect(screen.getByTestId("manual-add-step")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add device" })).toBeEnabled();
  });

  test("blocks dismissal while the submit is in flight", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    let resolveSubmit: () => void = () => {};
    const onSubmit = vi.fn().mockReturnValue(
      new Promise<void>((resolve) => {
        resolveSubmit = resolve;
      }),
    );

    render(<AddInfraDeviceModal onDismiss={onDismiss} onSubmit={onSubmit} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));

    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    await user.keyboard("{Escape}");
    expect(onDismiss).not.toHaveBeenCalled();

    resolveSubmit();
    await waitFor(() => expect(screen.getByRole("button", { name: "Add device" })).toBeEnabled());

    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
