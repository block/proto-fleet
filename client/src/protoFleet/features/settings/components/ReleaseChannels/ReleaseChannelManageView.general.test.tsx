import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { closeChannelSettings, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { canaryChannel } from "./ReleaseChannels.fixtures";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";

vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: () => null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

function renderManage(channel: ChannelView = canaryChannel) {
  const props = { ...manageViewProps(), channel };
  const { rerender } = render(<ReleaseChannelManageView {...props} />);
  openChannelSettings();
  return {
    onSave: props.onSave,
    onApply: props.onApply,
    updateChannel: (next: ChannelView) => rerender(<ReleaseChannelManageView {...props} channel={next} />),
  };
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("release channel name and description validation", () => {
  it.each([
    { label: "Name", field: "name", valid: "n".repeat(100), limit: 100 },
    { label: "Name", field: "name", valid: "😀".repeat(100), limit: 100 },
    { label: "Name", field: "name", valid: "e\u0301".repeat(50), limit: 100 },
    { label: "Description", field: "description", valid: "😀".repeat(1000), limit: 1000 },
  ])("validates trimmed $label by Unicode code points: $limit", async ({ label, field, valid, limit }) => {
    const { onSave } = renderManage();
    const input = screen.getByLabelText(label);
    const invalid = ` \u0085${valid}x\u0085 `;
    fireEvent.change(input, { target: { value: invalid } });

    expect(input).toHaveValue(invalid);
    expect(input).toHaveAttribute("aria-invalid", "true");
    await waitFor(() => expect(screen.getByText(`${label} must be ${limit} characters or fewer.`)).toBeVisible());
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: ` \u0085${valid}\u0085 ` } });
    expect(input).not.toHaveAttribute("aria-invalid");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));

    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ [field]: valid }));
  });

  it.each([
    { label: "Name", field: "name", value: "\uFEFF", limit: 100 },
    { label: "Description", field: "description", value: "\uFEFF", limit: 1000 },
  ])("preserves BOM in $label and includes it in length validation", async ({ label, field, value, limit }) => {
    const { onSave } = renderManage();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value: value + "x".repeat(limit) } });
    await waitFor(() => expect(screen.getByText(`${label} must be ${limit} characters or fewer.`)).toBeVisible());
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(input, { target: { value } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ [field]: value }));
    expect(input).toHaveValue(value);
    expect(screen.getByTestId("save-channel")).toBeDisabled();
  });

  it("uses Go trimming for dirty state and rebases untouched name and description fields", async () => {
    const channel = { ...canaryChannel, name: "\uFEFFCanary", description: "\uFEFFDescription" };
    const { onSave, updateChannel } = renderManage(channel);
    const name = screen.getByLabelText("Name");
    const description = screen.getByLabelText("Description");
    const save = screen.getByTestId("save-channel");
    expect(save).toBeDisabled();
    fireEvent.change(name, { target: { value: `\u0085${channel.name}\u0085` } });
    fireEvent.change(description, { target: { value: `\u0085${channel.description}\u0085` } });
    expect(save).toBeDisabled();
    updateChannel({ ...channel, name: "Remote", description: "Remote description" });
    expect(name).toHaveValue("Remote");
    expect(description).toHaveValue("Remote description");
    expect(save).toBeDisabled();
    fireEvent.change(name, { target: { value: "\u0085Local\u0085" } });
    fireEvent.change(description, { target: { value: "\u0085\u0085" } });
    await act(async () => fireEvent.click(save));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Local", description: "" }));
    expect(save).toBeDisabled();
    updateChannel({ ...channel, name: "Local", description: "" });
    expect(save).toBeDisabled();
    updateChannel({ ...channel, name: "Next", description: "Next description" });
    expect(name).toHaveValue("Next");
    expect(description).toHaveValue("Next description");
    expect(save).toBeDisabled();
  });

  it.each(["Name", "Description"])("rejects null characters in %s without discarding the text", async (label) => {
    const { onSave } = renderManage();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value: "Before\u0000after" } });
    expect(input).toHaveValue("Before\u0000after");
    expect(input).toHaveAttribute("aria-invalid", "true");
    await waitFor(() => expect(screen.getByText(`${label} cannot contain null characters.`)).toBeVisible());
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it.each([
    { label: "Name", value: "   " },
    { label: "Name", value: "\u0085" },
    { label: "Name", value: "n".repeat(101) },
    { label: "Description", value: "d".repeat(1001) },
  ])("blocks invalid $label but lets firmware Apply use saved settings", async ({ label, value }) => {
    const { onSave, onApply } = renderManage();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value } });
    expect(input).toHaveValue(value);
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    closeChannelSettings();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Clear assignments" })));

    expect(onApply).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
    openChannelSettings();
    expect(screen.getByLabelText(label)).toHaveValue(value);
  });
});
