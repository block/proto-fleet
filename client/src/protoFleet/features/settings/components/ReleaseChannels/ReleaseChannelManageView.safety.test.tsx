import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { closeChannelSettings, deferred, manageViewProps, openChannelSettings } from "./__tests__/helpers";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { canaryChannel, firmwareFiles } from "./ReleaseChannels.fixtures";
import { type ReleaseChannelScope, ReleaseChannelScopeSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { pushToast } from "@/shared/features/toaster";

vi.mock("./ScopeEditor", () => ({
  default: ({ scope, onChange }: { scope: ReleaseChannelScope; onChange: (scope: ReleaseChannelScope) => void }) => (
    <>
      <button onClick={() => onChange(create(ReleaseChannelScopeSchema, { ...scope, siteIds: [1n, 2n] }))}>
        Add another site
      </button>
      <button onClick={() => onChange(create(ReleaseChannelScopeSchema, { ...scope, deviceIdentifiers: ["new"] }))}>
        Replace miners
      </button>
    </>
  ),
}));
vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn(), STATUSES: { success: "success", error: "error" } }));

const channel = {
  ...canaryChannel,
  scope: create(ReleaseChannelScopeSchema, { siteIds: [1n] }),
};
const unassigned = {
  ...channel,
  modelGroups: channel.modelGroups.map((group) => ({
    ...group,
    firmwareFileId: "",
    firmwareChecksum: "",
    firmwareVersion: "",
  })),
};

function chooseFirmware(clear = false) {
  fireEvent.click(screen.getByTestId("channel-firmware-select-Rig"));
  fireEvent.click(screen.getByRole("option", { name: clear ? "No firmware" : /1\.4\.3/ }));
}

function expandScope() {
  openChannelSettings();
  fireEvent.click(screen.getByRole("button", { name: "Add another site" }));
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
});
afterEach(() => vi.restoreAllMocks());

describe("safe combined channel changes", () => {
  it.each([false, true])("blocks scope expansion with an existing assignment change (clear=%s)", (clear) => {
    const props = { ...manageViewProps(), channel, firmwareFiles };
    render(<ReleaseChannelManageView {...props} />);
    chooseFirmware(clear);
    expandScope();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(within(screen.getByTestId("channel-settings-modal")).getByRole("alert")).toHaveTextContent(
      /previous firmware/,
    );
    closeChannelSettings();
    expect(screen.getByTestId("apply-firmware-changes")).toBeDisabled();
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Keep current scope" }));
    expect(screen.getByTestId("apply-firmware-changes")).toBeEnabled();
    expect(screen.getByTestId("channel-firmware-select-Rig")).toHaveTextContent(clear ? "No firmware" : "1.4.3");
  });

  it.each([false, true])("blocks scope changes when an assigned upload is unavailable (clear=%s)", (clear) => {
    const props = {
      ...manageViewProps(),
      channel: {
        ...channel,
        modelGroups: channel.modelGroups.map((group) => ({ ...group, firmwareFileId: "", firmwareAvailable: false })),
      },
      firmwareFiles,
    };
    render(<ReleaseChannelManageView {...props} />);
    chooseFirmware(clear);
    expandScope();
    expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(within(screen.getByTestId("channel-settings-modal")).getByRole("alert")).toHaveTextContent(
      /previous firmware/,
    );
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).not.toHaveBeenCalled();
  });

  it("allows scope expansion when the changed model has no assignment", async () => {
    const props = { ...manageViewProps(), channel: unassigned, firmwareFiles };
    render(<ReleaseChannelManageView {...props} />);
    chooseFirmware();
    expandScope();
    fireEvent.click(screen.getByTestId("save-channel"));
    await act(async () =>
      fireEvent.click(
        within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }),
      ),
    );
    expect(props.onSave).toHaveBeenCalledOnce();
    expect(props.onApply).toHaveBeenCalledOnce();
    expect(props.onSave.mock.invocationCallOrder[0]).toBeLessThan(props.onApply.mock.invocationCallOrder[0]);
  });

  it("keeps the reviewed settings and firmware visible while the second write is pending", async () => {
    const props = { ...manageViewProps(), channel, firmwareFiles };
    const save = deferred<void>();
    const apply = deferred<void>();
    props.onSave.mockReturnValueOnce(save.promise);
    props.onApply.mockReturnValueOnce(apply.promise);
    render(<ReleaseChannelManageView {...props} />);
    chooseFirmware();
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed channel" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" }));
    await act(async () => save.resolve());
    expect(props.onApply).toHaveBeenCalledOnce();
    expect(dialog).toHaveTextContent("Apply channel changes?");
    expect(within(dialog).getByRole("table", { name: "Channel settings changes" })).toHaveTextContent(
      "Renamed channel",
    );
    expect(within(dialog).getByRole("table", { name: "Firmware changes" })).toHaveTextContent("1.4.3");
    expect(dialog).toHaveTextContent("2 changes pending");
    await act(async () => apply.resolve());
    await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
  });

  it("preserves submitted miner lists across refreshes and clears nested details when Apply completes", async () => {
    const props = {
      ...manageViewProps(),
      channel: { ...unassigned, scope: create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["old"] }) },
      firmwareFiles,
      minerNames: { old: "Original rig", new: "Target rig" },
    };
    const apply = deferred<void>();
    props.onApply.mockReturnValueOnce(apply.promise);
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    chooseFirmware();
    openChannelSettings();
    fireEvent.click(screen.getByRole("button", { name: "Replace miners" }));
    fireEvent.click(screen.getByTestId("save-channel"));
    const preview = screen.getByTestId("apply-firmware-dialog");
    await act(async () => fireEvent.click(within(preview).getByRole("button", { name: "Apply changes" })));
    rerender(<ReleaseChannelManageView {...props} minerNames={{ old: "Renamed original", new: "Renamed target" }} />);
    fireEvent.click(within(preview).getByRole("button", { name: "View miners" }));
    const table = screen.getByRole("table", { name: "Miner changes" });
    expect(table).toHaveTextContent("Original rig");
    expect(table).toHaveTextContent("Target rig");
    expect(table).not.toHaveTextContent("Renamed");
    await act(async () => apply.resolve());
    expect(screen.queryByTestId("miner-scope-changes-modal")).not.toBeInTheDocument();
    expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument();
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Next name" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    expect(screen.getByTestId("apply-firmware-dialog")).not.toHaveAttribute("inert");
    expect(screen.queryByTestId("miner-scope-changes-modal")).not.toBeInTheDocument();
  });

  it.each([false, true])("uses acknowledged assignments when refresh is unavailable (cleared=%s)", async (cleared) => {
    const props = {
      ...manageViewProps(),
      channel: cleared ? channel : unassigned,
      firmwareFiles,
      hasRefreshError: true,
    };
    render(<ReleaseChannelManageView {...props} />);
    chooseFirmware(cleared);
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    await act(async () =>
      fireEvent.click(
        within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", {
          name: cleared ? "Clear assignments" : "Start update",
        }),
      ),
    );
    await waitFor(() => expect(screen.queryByTestId("apply-firmware-dialog")).not.toBeInTheDocument());
    chooseFirmware(!cleared);
    expandScope();
    if (cleared) expect(screen.getByTestId("save-channel")).toBeEnabled();
    else expect(screen.getByTestId("save-channel")).toBeDisabled();
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).toHaveBeenCalledOnce();
  });

  it("blocks an open scope-and-firmware preview if a poll adds an existing assignment", () => {
    const props = { ...manageViewProps(), firmwareFiles };
    const { rerender } = render(<ReleaseChannelManageView {...props} channel={unassigned} />);
    chooseFirmware();
    expandScope();
    fireEvent.click(screen.getByTestId("save-channel"));
    rerender(<ReleaseChannelManageView {...props} channel={channel} />);
    const dialog = within(screen.getByTestId("apply-firmware-dialog"));
    expect(dialog.getByRole("button", { name: "Apply changes" })).toBeDisabled();
    expect(dialog.getByRole("alert")).toHaveTextContent(/previous firmware/);
    expect(props.onSave).not.toHaveBeenCalled();
    expect(props.onApply).not.toHaveBeenCalled();
  });

  it("reports the partial result when the editor closes between the two writes", async () => {
    const props = { ...manageViewProps(), channel, firmwareFiles };
    const save = deferred<void>();
    props.onSave.mockReturnValueOnce(save.promise);
    const { unmount } = render(<ReleaseChannelManageView {...props} />);
    chooseFirmware();
    openChannelSettings();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed channel" } });
    fireEvent.click(screen.getByTestId("save-channel"));
    fireEvent.click(within(screen.getByTestId("apply-firmware-dialog")).getByRole("button", { name: "Apply changes" }));
    unmount();
    await act(async () => save.resolve());
    expect(props.onApply).not.toHaveBeenCalled();
    expect(pushToast).toHaveBeenCalledWith({
      message:
        "Channel settings were saved, but firmware changes were not applied. Open the channel to review its firmware assignments.",
      status: "error",
    });
  });
});
