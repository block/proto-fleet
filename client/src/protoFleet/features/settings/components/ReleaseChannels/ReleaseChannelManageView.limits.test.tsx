import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { closeChannelSettings, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { ReleaseChannelModelGroupSchema, ReleaseChannelSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";

// Exercise real management state and writes at the API's 100-pair boundary;
// the picker portal and scope-preview behavior have their own component tests.
vi.mock("./ScopeEditor", () => ({ default: () => null }));
vi.mock("./FirmwarePickerButton", () => ({
  default: ({
    options,
    value,
    onChange,
    testId,
  }: {
    options: { value: string; label: string }[];
    value: string | null;
    onChange: (value: string) => void;
    testId: string;
  }) => (
    <select data-testid={testId} value={value ?? ""} onChange={(event) => onChange(event.target.value)}>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  ),
}));
vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(), STATUSES: { success: "success", error: "error" } }));

describe("firmware assignment request limit", () => {
  test("blocks 101 canonical changes atomically, preserves edits, and allows a reverted set of 100", async () => {
    const groups = Array.from({ length: 101 }, (_, index) =>
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: "Proto",
        model: `Model-${index}`,
        minerCount: 1,
        firmwareFileId: `old-${index}`,
        firmwareAvailable: true,
        firmwareChecksum: "a".repeat(64),
        firmwareVersion: "1.0",
        firmwareTargetManufacturer: "Proto",
        firmwareTargetModel: `Model-${index}`,
        assignmentGeneration: 1n,
      }),
    );
    const channel: ChannelView = {
      ...create(ReleaseChannelSchema, { id: 1n, name: "Large channel", behavior: defaultBehavior() }),
      modelGroups: [
        ...groups,
        create(ReleaseChannelModelGroupSchema, { ...groups[0], manufacturer: " PROTO ", model: " model-0 " }),
      ],
    };
    const firmwareFiles = groups.flatMap((group, index) =>
      ["old", "new"].map((version) => ({
        id: `${version}-${index}`,
        filename: `${version}-${index}.swu`,
        size: 1,
        uploaded_at: "2026-09-17T00:00:00Z",
        target_manufacturer: "Proto",
        target_model: group.model,
        firmware_version: version,
      })),
    );
    const props = manageViewProps();
    const { onApply, onSave } = props;
    render(<ReleaseChannelManageView {...props} channel={channel} firmwareFiles={firmwareFiles} />);
    openChannelSettings();
    for (let index = 0; index < 100; index += 1) {
      fireEvent.change(screen.getByTestId(`channel-firmware-select-Model-${index}`), {
        target: { value: `new-${index}` },
      });
    }
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("100");
    const aliasPicker = screen.getByTestId("channel-firmware-select- model-0 ", { normalizer: (value) => value });
    expect(aliasPicker).toHaveValue("new-0");
    fireEvent.change(aliasPicker, { target: { value: "old-0" } });
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("99");
    fireEvent.change(aliasPicker, { target: { value: "" } });
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("100");
    const apply = screen.getByTestId("apply-firmware-changes");
    expect(apply).toBeEnabled();
    fireEvent.click(apply);
    const start = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" });
    expect(start).toBeEnabled();

    fireEvent.change(screen.getByTestId("channel-firmware-select-Model-100"), { target: { value: "new-100" } });
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("101");
    expect(apply).toBeDisabled();
    expect(start).toBeDisabled();
    expect(screen.getAllByText(/Apply up to 100 model changes at a time/)).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
    fireEvent.click(start);
    expect(onApply).not.toHaveBeenCalled();
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Cancel" }));
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed channel" } });
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    closeChannelSettings();
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("102");

    fireEvent.change(screen.getByTestId("channel-firmware-select-Model-100"), { target: { value: "old-100" } });
    expect(screen.getByTestId("pending-change-count")).toHaveTextContent("101");
    await waitFor(() => expect(screen.queryByText(/Apply up to 100 model changes at a time/)).not.toBeInTheDocument());
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const confirm = within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" });
    expect(confirm).toBeEnabled();
    await act(async () => fireEvent.click(confirm));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Renamed channel" }));
    expect(onApply).toHaveBeenCalledExactlyOnceWith(
      1n,
      groups.slice(0, 100).map((group, index) => ({
        manufacturer: "Proto",
        model: group.model,
        firmwareFileId: index === 0 ? "" : `new-${index}`,
      })),
    );
  }, 15_000);
});
