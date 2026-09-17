import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
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
import { useFleetStore } from "@/protoFleet/store";

const initialAuth = useFleetStore.getState().auth;
afterEach(() => useFleetStore.setState({ auth: initialAuth }));

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
  it("finishes slow detail scans across repeated summary polls and coalesces one trailing refresh", async () => {
    const props = propsFor();
    const members = deferred<ReleaseChannelMiner[]>();
    const progress = deferred<RolloutDevice[]>();
    const nextMembers = deferred<ReleaseChannelMiner[]>();
    const nextProgress = deferred<RolloutDevice[]>();
    props.listChannelMiners.mockReturnValueOnce(members.promise).mockReturnValueOnce(nextMembers.promise);
    props.listRolloutDevices.mockReturnValueOnce(progress.promise).mockReturnValueOnce(nextProgress.promise);
    const { rerender } = render(<ModelMinersModal {...props} />);

    for (let poll = 0; poll < 4; poll++) {
      rerender(<ModelMinersModal {...props} group={{ ...props.group }} activeRollout={{ ...activeRigRollout }} />);
    }
    expect(props.listChannelMiners).toHaveBeenCalledOnce();
    expect(props.listRolloutDevices).toHaveBeenCalledOnce();
    await act(async () => members.resolve([miner]));
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    await act(async () => progress.resolve([device]));

    expect(screen.getByTestId("channel-miner-rig-1")).toHaveTextContent("Updated");
    expect(props.listChannelMiners).toHaveBeenCalledTimes(2);
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("table")).toHaveAttribute("aria-busy", "true");
    await act(async () => {
      nextMembers.resolve([create(ReleaseChannelMinerSchema, { deviceIdentifier: "rig-2" })]);
      nextProgress.resolve([
        create(RolloutDeviceSchema, { deviceIdentifier: "rig-2", phase: RolloutDevicePhase.FAILED }),
      ]);
    });
    expect(screen.getByTestId("channel-miner-rig-2")).toHaveTextContent("Failed");
    expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
    expect(screen.getByRole("table")).toHaveAttribute("aria-busy", "false");
    expect(props.listChannelMiners).toHaveBeenCalledTimes(2);
  });

  it("waits for both reads to settle before retrying a scan whose other read failed", async () => {
    const props = propsFor();
    const members = deferred<ReleaseChannelMiner[]>();
    const progress = deferred<RolloutDevice[]>();
    props.listChannelMiners.mockReturnValueOnce(members.promise);
    props.listRolloutDevices.mockReturnValueOnce(progress.promise);
    const { rerender } = render(<ModelMinersModal {...props} />);
    await act(async () => progress.reject(new Error("Device page failed")));
    rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);
    rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);

    expect(props.listChannelMiners).toHaveBeenCalledOnce();
    expect(props.listRolloutDevices).toHaveBeenCalledOnce();
    expect(screen.getByTestId("channel-miners-loading")).toBeInTheDocument();
    await act(async () => members.resolve([miner]));
    expect(screen.getByTestId("channel-miner-rig-1")).toHaveTextContent("Updated");
    expect(props.listChannelMiners).toHaveBeenCalledTimes(2);
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("discards queued work when unmounted during a slow scan", async () => {
    const props = propsFor();
    const members = deferred<ReleaseChannelMiner[]>();
    props.listChannelMiners.mockReturnValueOnce(members.promise);
    const { rerender, unmount } = render(<ModelMinersModal {...props} />);
    rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);
    unmount();
    await act(async () => members.resolve([miner]));
    expect(props.listChannelMiners).toHaveBeenCalledOnce();
    expect(props.listRolloutDevices).toHaveBeenCalledOnce();
  });

  it.each(["resolve", "reject"] as const)(
    "invalidates prior-session data and a pending scan's late %s",
    async (result) => {
      const props = propsFor();
      const { rerender } = render(<ModelMinersModal {...props} />);
      await screen.findByTestId("channel-miner-rig-1");
      const obsolete = deferred<ReleaseChannelMiner[]>();
      const current = deferred<ReleaseChannelMiner[]>();
      props.listChannelMiners.mockReturnValueOnce(obsolete.promise).mockReturnValueOnce(current.promise);
      rerender(<ModelMinersModal {...props} group={{ ...props.group }} />);
      act(() => {
        useFleetStore.setState({
          auth: { ...initialAuth, username: "new-operator", sessionGeneration: initialAuth.sessionGeneration + 1 },
        });
      });
      expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
      expect(screen.getByTestId("channel-miners-loading")).toBeInTheDocument();
      await act(async () => {
        if (result === "resolve") obsolete.resolve([miner]);
        else obsolete.reject(new Error("Old session failed"));
        current.resolve([create(ReleaseChannelMinerSchema, { deviceIdentifier: "new-session" })]);
      });
      expect(screen.getByTestId("channel-miner-new-session")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-rig-1")).not.toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(props.listChannelMiners).toHaveBeenCalledTimes(3);
    },
  );

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

describe("ModelMinersModal assigned firmware status", () => {
  const assignedChecksum = "a".repeat(64);

  it.each([
    {
      name: "matching version and provenance",
      version: "2.0.0",
      deployed: assignedChecksum,
      checksum: assignedChecksum,
      expected: "On assigned version",
    },
    {
      name: "matching version without provenance",
      version: "2.0.0",
      deployed: "",
      checksum: assignedChecksum,
      expected: "Not on assigned version",
    },
    {
      name: "the same version from different firmware bytes",
      version: "2.0.0",
      deployed: "b".repeat(64),
      checksum: assignedChecksum,
      expected: "Not on assigned version",
    },
    {
      name: "matching provenance with a different reported version",
      version: "1.0.0",
      deployed: assignedChecksum,
      checksum: assignedChecksum,
      expected: "Not on assigned version",
    },
    {
      name: "matching provenance without a reported version",
      version: "",
      deployed: assignedChecksum,
      checksum: assignedChecksum,
      expected: "Not on assigned version",
    },
    {
      name: "an empty assignment checksum and empty provenance",
      version: "2.0.0",
      deployed: "",
      checksum: "",
      expected: "Not on assigned version",
    },
  ])("handles $name", async ({ version, deployed, checksum, expected }) => {
    const props = propsFor();
    props.listChannelMiners.mockResolvedValue([
      create(ReleaseChannelMinerSchema, {
        ...miner,
        firmwareVersion: version,
        lastDeployedFirmwareChecksum: deployed,
      }),
    ]);
    render(
      <ModelMinersModal
        {...props}
        activeRollout={undefined}
        group={{ ...props.group, firmwareVersion: "2.0.0", firmwareChecksum: checksum }}
      />,
    );
    const row = await screen.findByTestId("channel-miner-rig-1");
    expect(within(row).getAllByRole("cell")[2]).toHaveTextContent(expected);
    expect(props.listRolloutDevices).not.toHaveBeenCalled();
  });

  it("leaves unassigned miners neutral even when they retain firmware provenance", async () => {
    const props = propsFor();
    props.listChannelMiners.mockResolvedValue([
      create(ReleaseChannelMinerSchema, { ...miner, lastDeployedFirmwareChecksum: assignedChecksum }),
    ]);
    render(
      <ModelMinersModal
        {...props}
        activeRollout={undefined}
        group={{ ...props.group, firmwareVersion: "", firmwareChecksum: "" }}
      />,
    );
    const row = await screen.findByTestId("channel-miner-rig-1");
    expect(within(row).getAllByRole("cell")[2]).toHaveTextContent(/^—$/);
  });

  it.each([
    { phase: RolloutDevicePhase.IN_PROGRESS, label: "Updating", deployed: assignedChecksum },
    { phase: RolloutDevicePhase.FAILED, label: "Failed", deployed: assignedChecksum },
    { phase: RolloutDevicePhase.DONE, label: "Updated", deployed: "" },
    { phase: RolloutDevicePhase.UNSPECIFIED, label: "On assigned version", deployed: assignedChecksum },
  ])("preserves the server phase $phase before applying the identity fallback", async ({ phase, label, deployed }) => {
    const props = propsFor();
    props.listChannelMiners.mockResolvedValue([
      create(ReleaseChannelMinerSchema, { ...miner, firmwareVersion: "2.0.0", lastDeployedFirmwareChecksum: deployed }),
    ]);
    props.listRolloutDevices.mockResolvedValue([create(RolloutDeviceSchema, { ...device, phase })]);
    render(
      <ModelMinersModal
        {...props}
        group={{ ...props.group, firmwareVersion: "2.0.0", firmwareChecksum: assignedChecksum }}
      />,
    );
    const row = await screen.findByTestId("channel-miner-rig-1");
    expect(within(row).getAllByRole("cell")[2]).toHaveTextContent(label);
  });
});

describe("ModelMinersModal unknown observed identities", () => {
  it.each([
    { manufacturer: "", model: "Rig", otherManufacturer: "Proto", otherModel: "Rig" },
    { manufacturer: "Proto", model: "", otherManufacturer: "Proto", otherModel: "Rig" },
    { manufacturer: "", model: "", otherManufacturer: "Proto", otherModel: "Rig" },
  ])(
    "limits wildcard results to the exact raw group [$manufacturer, $model]",
    async ({ manufacturer, model, otherManufacturer, otherModel }) => {
      const props = propsFor();
      props.listChannelMiners.mockResolvedValue([
        create(ReleaseChannelMinerSchema, {
          deviceIdentifier: "other-pair",
          manufacturer: otherManufacturer,
          model: otherModel,
        }),
        create(ReleaseChannelMinerSchema, { deviceIdentifier: "unknown-pair", manufacturer, model }),
      ]);
      render(<ModelMinersModal {...props} activeRollout={undefined} group={{ ...props.group, manufacturer, model }} />);

      expect(await screen.findByTestId("channel-miner-unknown-pair")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-other-pair")).not.toBeInTheDocument();
      expect(props.listChannelMiners).toHaveBeenCalledExactlyOnceWith(props.channelId, manufacturer, model);
    },
  );

  it("shows an empty unknown group when a wildcard read only contains other raw pairs", async () => {
    const props = propsFor();
    props.listChannelMiners.mockResolvedValue([
      create(ReleaseChannelMinerSchema, { deviceIdentifier: "known-pair", manufacturer: "Proto", model: "Rig" }),
    ]);
    render(
      <ModelMinersModal
        {...props}
        activeRollout={undefined}
        group={{ ...props.group, manufacturer: "", model: "Rig" }}
      />,
    );
    expect(await screen.findByText("No miners in this model group.")).toBeInTheDocument();
    expect(screen.queryByTestId("channel-miner-known-pair")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
