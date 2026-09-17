import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { deferred } from "./__tests__/helpers";
import { activeRigRollout } from "./ReleaseChannels.fixtures";
import RolloutMinersModal from "./RolloutMinersModal";
import {
  type RolloutDevice,
  RolloutDevicePhase,
  RolloutDeviceSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { useFleetStore } from "@/protoFleet/store";

const initialAuth = useFleetStore.getState().auth;
beforeEach(() =>
  useFleetStore.setState({
    auth: { ...initialAuth, username: "operator", sessionGeneration: 1, isAuthenticated: true },
  }),
);
afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
});

const failed = create(RolloutDeviceSchema, {
  deviceIdentifier: "failed-rig",
  phase: RolloutDevicePhase.FAILED,
  lastError: "Install failed",
});
const done = create(RolloutDeviceSchema, { deviceIdentifier: "done-rig", phase: RolloutDevicePhase.DONE });
const propsFor = () => ({
  rollout: activeRigRollout,
  minerNames: { "failed-rig": "Failed rig", "done-rig": "Updated rig" },
  listRolloutDevices: vi.fn<ReleaseChannelsApi["listRolloutDevices"]>().mockResolvedValue([failed, done]),
  onClose: vi.fn(),
});

describe("rollout miner detail loading", () => {
  it("finishes slow scans across summary polls and preserves filtering and names during the trailing refresh", async () => {
    const first = deferred<RolloutDevice[]>();
    const second = deferred<RolloutDevice[]>();
    const props = propsFor();
    props.listRolloutDevices.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { rerender } = render(<RolloutMinersModal {...props} initialFilter="failed" />);
    let polledRollout = props.rollout;
    for (let poll = 0; poll < 4; poll += 1) {
      polledRollout = { ...props.rollout };
      rerender(<RolloutMinersModal {...props} rollout={polledRollout} initialFilter="failed" />);
    }
    expect(props.listRolloutDevices).toHaveBeenCalledExactlyOnceWith(props.rollout.id, expect.any(AbortSignal));
    expect(props.listRolloutDevices.mock.calls[0][1]?.aborted).toBe(false);
    await act(async () => first.resolve([failed, done]));
    expect(screen.getByText("Failed rig")).toBeInTheDocument();
    expect(screen.queryByText("Updated rig")).not.toBeInTheDocument();
    expect(screen.getByText("1 miner failed to update")).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);

    rerender(
      <RolloutMinersModal
        {...props}
        rollout={polledRollout}
        initialFilter="failed"
        minerNames={{ ...props.minerNames, "failed-rig": "Renamed rig" }}
      />,
    );
    expect(screen.getByText("Renamed rig")).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole("button", { name: "All miners" }));
    expect(screen.getByText("Updated rig")).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
    await act(async () => second.resolve([done]));
    expect(screen.queryByText("Renamed rig")).not.toBeInTheDocument();
    expect(screen.getByText("Updated rig")).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
  });

  it("shows an initial failure instead of an empty success, and permits a retry that returns no miners", async () => {
    const retry = deferred<RolloutDevice[]>();
    const props = propsFor();
    props.listRolloutDevices
      .mockRejectedValueOnce(new Error("Device list unavailable"))
      .mockReturnValueOnce(retry.promise);
    render(<RolloutMinersModal {...props} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load miners");
    expect(screen.getByRole("alert")).toHaveTextContent("Device list unavailable");
    expect(screen.queryByText("No miners to show.")).not.toBeInTheDocument();
    expect(screen.queryByText("No miners failed.")).not.toBeInTheDocument();
    expect(screen.queryByText("Loading miners…")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(screen.getByRole("button", { name: "Retrying..." })).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(2);
    await act(async () => retry.resolve([]));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByText("No miners to show.")).toBeInTheDocument();
  });

  it("retains complete rows and the selected filter after a refresh deadline, then recovers through Retry", async () => {
    const props = propsFor();
    const { rerender } = render(<RolloutMinersModal {...props} initialFilter="failed" />);
    await screen.findByText("Failed rig");
    props.listRolloutDevices.mockRejectedValueOnce(new ConnectError("Device read timed out", Code.DeadlineExceeded));
    rerender(<RolloutMinersModal {...props} rollout={{ ...props.rollout }} initialFilter="failed" />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Showing the last loaded data");
    expect(screen.getByRole("alert")).toHaveTextContent("Device read timed out");
    expect(screen.getByText("Failed rig")).toBeInTheDocument();
    expect(screen.queryByText("Updated rig")).not.toBeInTheDocument();
    const retry = deferred<RolloutDevice[]>();
    props.listRolloutDevices.mockReturnValueOnce(retry.promise);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(screen.getByRole("alert")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByText("Failed rig")).toBeInTheDocument();
    await act(async () => retry.resolve([done]));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByText("Failed rig")).not.toBeInTheDocument();
    expect(screen.queryByText("Updated rig")).not.toBeInTheDocument();
    expect(screen.getByText("No miners failed.")).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(3);
  });

  it.each(["Done", "Escape"])("aborts pending and queued scans immediately when dismissed with %s", async (dismiss) => {
    const pending = deferred<RolloutDevice[]>();
    const props = propsFor();
    props.listRolloutDevices.mockReturnValueOnce(pending.promise);
    const { rerender } = render(<RolloutMinersModal {...props} />);
    rerender(<RolloutMinersModal {...props} rollout={{ ...props.rollout }} />);
    const signal = props.listRolloutDevices.mock.calls[0][1]!;
    if (dismiss === "Done") fireEvent.click(screen.getByRole("button", { name: "Done" }));
    else fireEvent.keyDown(document, { key: "Escape" });
    expect(signal.aborted).toBe(true);
    expect(props.onClose).toHaveBeenCalledOnce();
    await act(async () => pending.resolve([failed]));
    expect(props.listRolloutDevices).toHaveBeenCalledOnce();
    expect(screen.queryByText("Failed rig")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("hides the preceding rollout's rows and ignores its late failure when another rollout is selected", async () => {
    const props = propsFor();
    const { rerender } = render(<RolloutMinersModal {...props} />);
    await screen.findByText("Failed rig");
    const previous = deferred<RolloutDevice[]>();
    const current = deferred<RolloutDevice[]>();
    props.listRolloutDevices.mockReturnValueOnce(previous.promise).mockReturnValueOnce(current.promise);
    rerender(<RolloutMinersModal {...props} rollout={{ ...props.rollout }} />);
    const oldSignal = props.listRolloutDevices.mock.calls[1][1]!;
    const replacement = { ...props.rollout, id: 999n, model: "Different model" };
    rerender(<RolloutMinersModal {...props} rollout={replacement} />);
    expect(oldSignal.aborted).toBe(true);
    expect(screen.queryByText("Failed rig")).not.toBeInTheDocument();
    expect(screen.queryByText("Updated rig")).not.toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenLastCalledWith(999n, expect.any(AbortSignal));
    await act(async () => previous.reject(new Error("Old rollout failed")));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await act(async () => current.resolve([done]));
    expect(screen.getByText("Updated rig")).toBeInTheDocument();
    expect(screen.getByText(/Different model/)).toBeInTheDocument();
    expect(props.listRolloutDevices).toHaveBeenCalledTimes(3);
  });
});
