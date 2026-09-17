import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ModelMinersModal from "./ModelMinersModal";
import { activeRigRollout, canaryChannel } from "./ReleaseChannels.fixtures";
import {
  type ReleaseChannelMiner,
  ReleaseChannelMinerSchema,
  type RolloutDevice,
  RolloutDevicePhase,
  RolloutDeviceSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const miner = create(ReleaseChannelMinerSchema, { deviceIdentifier: "rig-1", firmwareVersion: "1.0.0" });
const device = create(RolloutDeviceSchema, {
  deviceIdentifier: miner.deviceIdentifier,
  phase: RolloutDevicePhase.DONE,
});

const propsFor = () => ({
  channelId: canaryChannel.id,
  channelName: canaryChannel.name,
  group: canaryChannel.modelGroups[0],
  activeRollout: activeRigRollout,
  minerNames: {},
  listChannelMiners: vi.fn<() => Promise<ReleaseChannelMiner[]>>().mockResolvedValue([miner]),
  listRolloutDevices: vi.fn<() => Promise<RolloutDevice[]>>().mockResolvedValue([device]),
  onClose: vi.fn(),
});

describe("ModelMinersModal detail loading", () => {
  it.each(["listChannelMiners", "listRolloutDevices"] as const)(
    "shows a failed initial %s and retries both complete detail lists",
    async (failedRead) => {
      const props = propsFor();
      props[failedRead].mockRejectedValueOnce(new Error("A later page could not be loaded"));
      render(<ModelMinersModal {...props} />);

      expect(screen.getByTestId("channel-miners-loading")).toBeInTheDocument();
      expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load miners");
      expect(screen.getByRole("alert")).toHaveTextContent("A later page could not be loaded");
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
      expect(screen.queryByText("No miners in this model group.")).not.toBeInTheDocument();
      expect(screen.queryByTestId("channel-miners-loading")).not.toBeInTheDocument();

      const members = deferred<ReleaseChannelMiner[]>();
      const progress = deferred<RolloutDevice[]>();
      props.listChannelMiners.mockReturnValueOnce(members.promise);
      props.listRolloutDevices.mockReturnValueOnce(progress.promise);
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "true");
      fireEvent.click(screen.getByRole("button", { name: "Retrying..." }));
      expect(props.listChannelMiners).toHaveBeenCalledTimes(2);
      expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
      await act(async () => members.resolve([miner]));
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
      await act(async () => progress.resolve([device]));

      expect(await screen.findByTestId("channel-miner-rig-1")).toHaveTextContent("Updated");
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(props.listChannelMiners).toHaveBeenLastCalledWith(
        canaryChannel.id,
        props.group.manufacturer,
        props.group.model,
      );
      expect(props.listRolloutDevices).toHaveBeenLastCalledWith(activeRigRollout.id);
    },
  );

  it("shows a true empty state only after a successful retry and skips device loading without a rollout", async () => {
    const props = propsFor();
    props.listChannelMiners.mockRejectedValueOnce(new Error(""));
    render(<ModelMinersModal {...props} activeRollout={undefined} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("The request failed. Try again.");
    expect(screen.queryByText("No miners in this model group.")).not.toBeInTheDocument();
    props.listChannelMiners.mockResolvedValueOnce([]);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("No miners in this model group.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(props.listRolloutDevices).not.toHaveBeenCalled();
  });

  it.each(["listChannelMiners", "listRolloutDevices"] as const)(
    "retains both previous lists when a polled %s fails, then replaces them together on retry",
    async (failedRead) => {
      const props = propsFor();
      const { rerender } = render(<ModelMinersModal {...props} />);
      expect(await screen.findByTestId("channel-miner-rig-1")).toHaveTextContent("Updated");
      props.listChannelMiners.mockResolvedValue([create(ReleaseChannelMinerSchema, { deviceIdentifier: "rig-2" })]);
      props.listRolloutDevices.mockResolvedValue([
        create(RolloutDeviceSchema, { deviceIdentifier: "rig-2", phase: RolloutDevicePhase.FAILED }),
      ]);
      props[failedRead].mockRejectedValueOnce(new Error("Refresh failed"));
      rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);

      expect(await screen.findByRole("alert")).toHaveTextContent("Showing the last loaded data");
      const oldRow = screen.getByTestId("channel-miner-rig-1");
      expect(oldRow).toHaveTextContent("1.0.0");
      expect(within(oldRow).getByText("Updated")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-rig-2")).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));

      expect(await screen.findByTestId("channel-miner-rig-2")).toHaveTextContent("Failed");
      expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    },
  );

  it.each(["channel", "model", "rollout"] as const)(
    "clears the preceding snapshot when the %s changes and ignores obsolete results",
    async (changedContext) => {
      const props = propsFor();
      const { rerender } = render(<ModelMinersModal {...props} />);
      await screen.findByTestId("channel-miner-rig-1");
      const obsolete = deferred<ReleaseChannelMiner[]>();
      props.listChannelMiners.mockReturnValueOnce(obsolete.promise);
      rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);
      await waitFor(() => expect(props.listChannelMiners).toHaveBeenCalledTimes(2));

      const next = deferred<ReleaseChannelMiner[]>();
      props.listChannelMiners.mockReturnValueOnce(next.promise);
      const nextProps = {
        ...props,
        channelId: changedContext === "channel" ? props.channelId + 1n : props.channelId,
        group: changedContext === "model" ? { ...props.group, model: "Other model" } : props.group,
        activeRollout:
          changedContext === "rollout" ? { ...activeRigRollout, id: activeRigRollout.id + 1n } : activeRigRollout,
      };
      rerender(<ModelMinersModal {...nextProps} />);
      expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
      expect(screen.getByTestId("channel-miners-loading")).toBeInTheDocument();
      await act(async () => next.resolve([create(ReleaseChannelMinerSchema, { deviceIdentifier: "new-context" })]));
      await screen.findByTestId("channel-miner-new-context");
      await act(async () => {
        if (changedContext === "model") obsolete.reject(new Error("Obsolete request failed"));
        else obsolete.resolve([miner]);
      });
      expect(screen.getByTestId("channel-miner-new-context")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    },
  );
});
