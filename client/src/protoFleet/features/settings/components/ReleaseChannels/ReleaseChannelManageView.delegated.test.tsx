import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { applyChannelSettings, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  ReleaseChannelSchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
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
  const props = manageViewProps();
  render(<ReleaseChannelManageView {...props} channel={channel} />);
  if (channel) openChannelSettings();
  return props.onSave;
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
    await applyChannelSettings();
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
    "shows order %s and requires a supported method before saving the changed order",
    async (order, label, nextOrder, nextLabel) => {
      const channel = delegatedChannel(order);
      const onSave = renderManage(channel);
      expect(screen.getByTestId("rollout-order")).toHaveTextContent(label);
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      fireEvent.click(screen.getByTestId("rollout-order"));
      expect(screen.getByRole("option", { name: label })).toHaveAttribute("aria-selected", "true");
      fireEvent.click(screen.getByRole("option", { name: nextLabel }));
      expect(screen.getByTestId("rollout-order")).toHaveTextContent(nextLabel);
      expect(screen.getByTestId("save-channel")).toBeDisabled();
      await applyChannelSettings();
      expect(onSave).not.toHaveBeenCalled();
      expect(screen.getByTestId("delegated-save-unavailable")).toHaveTextContent("Choose another update method");
      chooseMethod("Single batch");
      expect(screen.queryByTestId("delegated-save-unavailable")).not.toBeInTheDocument();
      expect(screen.getByTestId("rollout-order")).toHaveTextContent(nextLabel);
      await applyChannelSettings();
      expect(onSave).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          behavior: create(RolloutBehaviorSchema, {
            method: RolloutMethod.ALL_AT_ONCE,
            order: nextOrder,
            maxConcurrentOffline: 7,
          }),
        }),
      );
    },
  );

  it("keeps the saved method visible and blocks unrelated edits until the operator changes it", async () => {
    const channel = delegatedChannel();
    const onSave = renderManage(channel);
    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Controlled externally");
    await waitFor(() =>
      expect(
        screen.getByText("An external controller decides which miners update and when, through the API."),
      ).toBeVisible(),
    );
    expect(screen.queryByLabelText("Batch size (miners)")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Pilot batch size (miners)")).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed delegated channel" } });
    await applyChannelSettings();

    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Controlled externally");
    chooseMethod("Multiple batches");
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "5" } });
    await applyChannelSettings();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        name: "Renamed delegated channel",
        behavior: create(RolloutBehaviorSchema, {
          method: RolloutMethod.BATCHED,
          order: RolloutOrder.RANDOM,
          batchSize: 5,
          maxConcurrentOffline: 7,
        }),
      }),
    );
  });

  it("keeps Save blocked after reverting an unsaved switch to external control", async () => {
    const channel = delegatedChannel();
    const onSave = renderManage(channel);
    await waitFor(() => expect(screen.getByText("120 seconds")).toBeVisible());
    chooseMethod("Multiple batches");
    expect(screen.queryByText("Controller timeout")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Batch size (miners)"), { target: { value: "5" } });
    expect(screen.getByTestId("save-channel")).toBeEnabled();
    chooseMethod("Controlled externally");

    await waitFor(() => expect(screen.getByText("120 seconds")).toBeVisible());
    expect(screen.getByTestId("rollout-method")).toHaveTextContent("Controlled externally");
    expect(screen.queryByLabelText("Batch size (miners)")).not.toBeInTheDocument();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed after reverting" } });
    await applyChannelSettings();

    expect(screen.getByTestId("save-channel")).toBeDisabled();
    await waitFor(() => expect(screen.getByTestId("delegated-save-unavailable")).toBeVisible());
    expect(onSave).not.toHaveBeenCalled();
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
