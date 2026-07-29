import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import EditFirmwareMetadataDialog from "./EditFirmwareMetadataDialog";

const mockGetMinerModelGroups = vi.fn();

vi.mock("@/protoFleet/api/useMinerModelGroups", () => ({
  default: () => ({ getMinerModelGroups: mockGetMinerModelGroups }),
}));

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: ({ open, title, children, buttons, testId }: any) =>
    open ? (
      <div data-testid={testId}>
        <h1>{title}</h1>
        {children}
        {buttons?.map((button: any) => (
          <button key={button.text} disabled={button.disabled} onClick={button.onClick}>
            {button.text}
          </button>
        ))}
      </div>
    ) : null,
}));

vi.mock("@/shared/components/Select", () => ({
  default: ({ id, value, options, onChange, disabled }: any) => (
    <button
      type="button"
      data-testid={id}
      data-value={value}
      data-options={options.map((option: { value: string }) => option.value).join(",")}
      disabled={disabled}
      onClick={() => onChange(id.includes("manufacturer") ? "Bitmain" : "Rig")}
    >
      {value}
    </button>
  ),
}));

vi.mock("@/shared/components/Input", () => ({
  default: ({ id, initValue, onChange, disabled }: any) => (
    <input data-testid={id} value={initValue} disabled={disabled} onChange={(event) => onChange(event.target.value)} />
  ),
}));

const firmwareFile = {
  id: "firmware-1",
  filename: "proto-rig-2.0.0.swu",
  targetManufacturer: "Proto",
  targetModel: "Rig",
  firmwareVersion: "2.0.0",
};

describe("EditFirmwareMetadataDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetMinerModelGroups.mockResolvedValue([{ manufacturer: "Proto", model: "Rig", count: 1 }]);
  });

  it("loads the stored metadata and saves it", async () => {
    const onConfirm = vi.fn();
    render(
      <EditFirmwareMetadataDialog
        open
        file={firmwareFile}
        isSubmitting={false}
        onConfirm={onConfirm}
        onDismiss={vi.fn()}
      />,
    );

    expect(await screen.findByTestId("edit-firmware-target-manufacturer")).toHaveAttribute("data-value", "Proto");
    expect(screen.getByTestId("edit-firmware-target-model")).toHaveAttribute("data-value", "Rig");
    expect(screen.getByTestId("edit-firmware-version")).toHaveValue("2.0.0");

    fireEvent.click(screen.getByText("Save changes"));

    expect(onConfirm).toHaveBeenCalledWith({
      targetManufacturer: "Proto",
      targetModel: "Rig",
      firmwareVersion: "2.0.0",
    });
  });

  it("keeps save disabled for legacy firmware until metadata is complete", async () => {
    render(
      <EditFirmwareMetadataDialog
        open
        file={{ ...firmwareFile, targetManufacturer: "", targetModel: "", firmwareVersion: "" }}
        isSubmitting={false}
        onConfirm={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );

    await screen.findByTestId("edit-firmware-target-manufacturer");
    expect(screen.getByText("Save changes")).toBeDisabled();
  });

  it("clears the seeded model after changing manufacturer", async () => {
    const onConfirm = vi.fn();
    mockGetMinerModelGroups.mockResolvedValue([
      { manufacturer: "Proto", model: "Rig", count: 1 },
      { manufacturer: "Bitmain", model: "S19", count: 1 },
    ]);
    render(
      <EditFirmwareMetadataDialog
        open
        file={firmwareFile}
        isSubmitting={false}
        onConfirm={onConfirm}
        onDismiss={vi.fn()}
      />,
    );

    const manufacturerSelect = await screen.findByTestId("edit-firmware-target-manufacturer");
    fireEvent.click(manufacturerSelect);

    const modelSelect = screen.getByTestId("edit-firmware-target-model");
    expect(modelSelect).toHaveAttribute("data-value", "");
    expect(modelSelect).toHaveAttribute("data-options", ",S19");

    fireEvent.click(modelSelect);

    expect(modelSelect).toHaveAttribute("data-value", "");
    expect(screen.getByText("Save changes")).toBeDisabled();
    fireEvent.click(screen.getByText("Save changes"));
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
