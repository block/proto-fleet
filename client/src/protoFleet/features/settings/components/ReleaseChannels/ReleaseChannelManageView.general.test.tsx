import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { canaryChannel } from "./ReleaseChannels.fixtures";
import { PreviewReleaseChannelScopeResponseSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";

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

function renderManage() {
  const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockResolvedValue(undefined);
  const onApply = vi.fn().mockResolvedValue(undefined);
  render(
    <ReleaseChannelManageView
      channel={canaryChannel}
      rollouts={[]}
      firmwareFiles={[]}
      minerNames={{}}
      previewScope={vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema))}
      listChannelMiners={vi.fn().mockResolvedValue([])}
      listRolloutDevices={vi.fn().mockResolvedValue([])}
      onSave={onSave}
      onApply={onApply}
    />,
  );
  return { onSave, onApply };
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
    const invalid = `  ${valid}x  `;
    fireEvent.change(input, { target: { value: invalid } });

    expect(input).toHaveValue(invalid);
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(`${label} must be ${limit} characters or fewer.`)).toBeVisible();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: `  ${valid}  ` } });
    expect(input).not.toHaveAttribute("aria-invalid");
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));

    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ [field]: valid }));
  });

  it.each(["Name", "Description"])("rejects null characters in %s without discarding the text", (label) => {
    const { onSave } = renderManage();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value: "Before\u0000after" } });
    expect(input).toHaveValue("Before\u0000after");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(`${label} cannot contain null characters.`)).toBeVisible();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it.each([
    { label: "Name", value: "   " },
    { label: "Name", value: "n".repeat(101) },
    { label: "Description", value: "d".repeat(1001) },
  ])("blocks invalid $label but lets firmware Apply use saved settings", async ({ label, value }) => {
    const { onSave, onApply } = renderManage();
    const input = screen.getByLabelText(label);
    fireEvent.change(input, { target: { value } });
    expect(input).toHaveValue(value);
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
    fireEvent.click(screen.getByRole("option", { name: "No firmware" }));
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Start update" })));

    expect(onApply).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
    expect(input).toHaveValue(value);
  });
});
