import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import { pushToast } from "@/shared/features/toaster";

// Keep the management view and debounced scope preview real; the shared
// selection modals' fleet-data loading is outside this save-flow regression.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: ({ open, onSave }: { open: boolean; onSave: (selection: { siteIds: string[] }) => void }) =>
    open ? <button onClick={() => onSave({ siteIds: ["2"] })}>Choose site 2</button> : null,
  BuildingSelectionModal: () => null,
  RackSelectionModal: () => null,
  GroupSelectionModal: () => null,
  MinerSelectionModal: () => null,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const existingChannel = (): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Production",
    scope: { siteIds: [1n] },
    behavior: defaultBehavior(),
  }),
  modelGroups: [],
});

function renderManage(
  channel: ChannelView | undefined,
  onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockResolvedValue(undefined),
) {
  const previewScope = vi.fn().mockResolvedValue(
    create(PreviewReleaseChannelScopeResponseSchema, {
      minerCount: 1,
      conflicts: [{ channelId: 2n, channelName: "Canary", minerCount: 1 }],
      conflictCount: 1,
    }),
  );
  render(
    <ReleaseChannelManageView
      channel={channel}
      rollouts={[]}
      firmwareFiles={[]}
      minerNames={{}}
      previewScope={previewScope}
      listChannelMiners={vi.fn().mockResolvedValue([])}
      listRolloutDevices={vi.fn().mockResolvedValue([])}
      onSave={onSave}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );
  return { onSave, previewScope };
}

describe("release channel saves during scope overlaps", () => {
  test("allows existing channel edits while preserving empty-name, unchanged and busy guards", async () => {
    const channel = existingChannel();
    let finishSaving!: () => void;
    const completion = new Promise<void>((resolve) => {
      finishSaving = resolve;
    });
    const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockReturnValue(completion);
    renderManage(channel, onSave);
    await screen.findByTestId("scope-conflicts");
    const save = screen.getByTestId("save-channel");
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "   " } });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    expect(save).toBeEnabled();
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent("Existing overlaps can remain");
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ name: "Renamed", scope: channel.scope }));
    expect(save).toBeDisabled();
    await act(async () => finishSaving());
    await waitFor(() => expect(save).toBeEnabled());
  });

  test("lets the server reject newly introduced overlaps and displays its error", async () => {
    const onSave = vi
      .fn<(draft: ReleaseChannelDraft) => Promise<void>>()
      .mockRejectedValue(new Error("Scope adds an overlap with Canary"));
    renderManage(existingChannel(), onSave);
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await screen.findByTestId("scope-conflicts");
    const save = screen.getByTestId("save-channel");
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() =>
      expect(pushToast).toHaveBeenCalledWith({ message: "Scope adds an overlap with Canary", status: "error" }),
    );
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ scope: expect.objectContaining({ siteIds: [2n] }) }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Production");
    expect(save).toBeEnabled();
  });

  test("continues to block creating a channel whose scope overlaps", async () => {
    const { onSave } = renderManage(undefined);
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } });
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Choose site 2" }));
    await screen.findByTestId("scope-conflicts");
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent(
      "Remove those miners from one of the channels before saving",
    );
    const save = screen.getByTestId("save-channel");
    expect(save).toBeDisabled();
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
  });
});
