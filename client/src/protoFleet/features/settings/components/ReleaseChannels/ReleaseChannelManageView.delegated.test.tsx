import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import type { ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";

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

const delegatedChannel = (order = RolloutOrder.RANDOM): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Externally controlled",
    behavior: {
      method: RolloutMethod.DELEGATED,
      order,
      maxConcurrentOffline: 7,
      controllerTimeoutSeconds: 120,
    },
  }),
  modelGroups: [],
});

function renderManage(channel?: ChannelView) {
  const onSave = vi.fn<(draft: ReleaseChannelDraft) => Promise<void>>().mockResolvedValue(undefined);
  render(
    <ReleaseChannelManageView
      channel={channel}
      rollouts={[]}
      firmwareFiles={[]}
      minerNames={{}}
      previewScope={vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema))}
      listChannelMiners={vi.fn().mockResolvedValue([])}
      listRolloutDevices={vi.fn().mockResolvedValue([])}
      onSave={onSave}
      onApply={vi.fn().mockResolvedValue(undefined)}
    />,
  );
  return onSave;
}

function chooseMethod(label: string) {
  fireEvent.click(screen.getByTestId("rollout-method"));
  fireEvent.click(screen.getByRole("option", { name: new RegExp(`^${label}`) }));
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("existing delegated release channels", () => {
  it("shows and saves single-batch order when the offline budget throttles dispatch", async () => {
    const channel = delegatedChannel();
    channel.behavior = create(RolloutBehaviorSchema, {
      method: RolloutMethod.ALL_AT_ONCE,
      order: RolloutOrder.RANDOM,
      maxConcurrentOffline: 1,
    });
    const onSave = renderManage(channel);
    expect(screen.getByTestId("rollout-order")).toHaveTextContent("Random");
    expect(screen.queryByLabelText("Batch size (miners)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Pilot batch size (miners)")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("rollout-order"));
    fireEvent.click(screen.getByRole("option", { name: "Least efficient first" }));
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        behavior: create(RolloutBehaviorSchema, {
          method: RolloutMethod.ALL_AT_ONCE,
          order: RolloutOrder.LEAST_EFFICIENT_FIRST,
          maxConcurrentOffline: 1,
        }),
      }),
    );
  });

  it.each([
    [RolloutOrder.RANDOM, "Random", RolloutOrder.LEAST_EFFICIENT_FIRST, "Least efficient first"],
    [RolloutOrder.LEAST_EFFICIENT_FIRST, "Least efficient first", RolloutOrder.RANDOM, "Random"],
    [RolloutOrder.UNSPECIFIED, "Least efficient first", RolloutOrder.RANDOM, "Random"],
  ] as const)(
    "shows order %s and saves a changed order without altering external control",
    async (order, label, nextOrder, nextLabel) => {
      const channel = delegatedChannel(order);
      const onSave = renderManage(channel);
      expect(screen.getByTestId("rollout-order")).toHaveTextContent(label);
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      fireEvent.click(screen.getByTestId("rollout-order"));
      expect(screen.getByRole("option", { name: label })).toHaveAttribute("aria-selected", "true");
      fireEvent.click(screen.getByRole("option", { name: nextLabel }));
      expect(screen.getByTestId("rollout-order")).toHaveTextContent(nextLabel);
      expect(screen.getByTestId("save-channel")).toBeEnabled();
      await act(async () => fireEvent.click(screen.getByTestId("save-channel")));
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          behavior: create(RolloutBehaviorSchema, {
            method: RolloutMethod.DELEGATED,
            order: nextOrder,
            controllerTimeoutSeconds: 120,
            maxConcurrentOffline: 7,
          }),
        }),
      );
    },
  );

  it("displays external control and preserves its valid settings when only the name changes", async () => {
    const channel = delegatedChannel();
    const onSave = renderManage(channel);
    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Controlled externally");
    expect(
      screen.getByText("An external controller decides which miners update and when, through the API."),
    ).toBeVisible();
    expect(screen.queryByLabelText("Batch size (miners)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Pilot batch size (miners)")).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed delegated channel" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));

    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ name: "Renamed delegated channel", behavior: channel.behavior }),
    );
  });

  it("lets an existing delegated channel revert a tentative method change without saving inactive settings", async () => {
    const channel = delegatedChannel();
    const onSave = renderManage(channel);
    chooseMethod("Multiple batches");
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "5" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    chooseMethod("Controlled externally");

    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Controlled externally");
    expect(screen.queryByLabelText("Batch size (miners)")).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed after reverting" } });
    await act(async () => fireEvent.click(screen.getByTestId("save-channel")));

    expect(onSave).toHaveBeenCalledOnce();
    expect(rolloutBehaviorForRequest(onSave.mock.calls[0][0].behavior)).toEqual(channel.behavior);
  });

  it.each(["new", "existing nondelegated"] as const)(
    "keeps externally controlled mode out of a %s channel's method choices",
    (kind) => {
      renderManage(
        kind === "new"
          ? undefined
          : {
              ...delegatedChannel(),
              behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.ALL_AT_ONCE }),
            },
      );
      fireEvent.click(screen.getByTestId("rollout-method"));

      expect(screen.getAllByRole("option")).toHaveLength(3);
      expect(screen.getByRole("option", { name: /^Single batch/ })).toBeVisible();
      expect(screen.getByRole("option", { name: /^Multiple batches/ })).toBeVisible();
      expect(screen.getByRole("option", { name: /^Pilot batch, then remaining/ })).toBeVisible();
      expect(screen.queryByRole("option", { name: /^Controlled externally/ })).not.toBeInTheDocument();
    },
  );
});
