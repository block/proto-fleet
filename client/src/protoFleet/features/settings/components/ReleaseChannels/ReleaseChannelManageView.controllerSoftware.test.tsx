import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { manageViewProps } from "./__tests__/helpers";
import { defaultBehavior } from "./behaviorUtils";
import type { ControllerSoftwareApi } from "./controllerSoftwareUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import {
  ReleaseChannelContainerSchema,
  type ReleaseChannelControllerSoftware,
  ReleaseChannelControllerSoftwareSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

const rigGroup = create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model: "Rig", minerCount: 4 });
const containerGroup = create(ReleaseChannelModelGroupSchema, { manufacturer: "proto", model: "CT1", minerCount: 3 });
const assignedContainerGroup = create(ReleaseChannelModelGroupSchema, {
  manufacturer: "proto",
  model: "CT1",
  minerCount: 3,
  firmwareFileId: "hashboard-1.39.0",
  firmwareChecksum: "a".repeat(64),
  firmwareVersion: "1.39.0",
  firmwareAvailable: true,
});

const channelWith = (...modelGroups: ChannelView["modelGroups"]): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Production",
    scope: { siteIds: [1n] },
    behavior: defaultBehavior(),
  }),
  modelGroups,
});

const software = (targetVersion: string): ReleaseChannelControllerSoftware =>
  create(ReleaseChannelControllerSoftwareSchema, {
    manufacturer: "proto",
    model: "CT1",
    targetVersion,
    containerCount: 3,
    onTargetCount: targetVersion === "" ? 0 : 1,
  });

const controllerSoftwareApi = (targetVersion: string, canSet = true) => ({
  canSet,
  list: vi.fn<ControllerSoftwareApi["list"]>().mockResolvedValue([software(targetVersion)]),
  listContainers: vi.fn<ControllerSoftwareApi["listContainers"]>().mockResolvedValue([
    create(ReleaseChannelContainerSchema, {
      deviceIdentifier: "ct1:a",
      controllerSoftwareVersion: "1.6.0",
      onTarget: targetVersion !== "",
    }),
    create(ReleaseChannelContainerSchema, { deviceIdentifier: "ct1:b", controllerSoftwareVersion: "1.5.0" }),
    create(ReleaseChannelContainerSchema, { deviceIdentifier: "ct1:c", controllerSoftwareVersion: "" }),
  ]),
  set: vi.fn<ControllerSoftwareApi["set"]>().mockResolvedValue(undefined),
  // The same release uploaded for two architectures is offered once.
  listVersions: vi.fn<ControllerSoftwareApi["listVersions"]>().mockResolvedValue(["1.6.0", "v1.6.0", "1.5.0"]),
});

function renderManage(channel: ChannelView, controllerSoftware?: ControllerSoftwareApi) {
  const props = { ...manageViewProps(), channel, controllerSoftware, minerNames: { "ct1:a": "Container A" } };
  render(
    <MemoryRouter>
      <ReleaseChannelManageView {...props} />
    </MemoryRouter>,
  );
  return props;
}

const controllerRow = () => screen.findByTestId("controller-software-row-CT1");

describe("controller software in a release channel", () => {
  // The version picker positions its options from the trigger's layout box.
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.mocked(pushToast).mockClear();
  });

  it("makes no request for a channel without containers", () => {
    const api = controllerSoftwareApi("1.6.0");
    renderManage(channelWith(rigGroup), api);

    expect(screen.queryByText("Controller software")).not.toBeInTheDocument();
    expect(api.list).not.toHaveBeenCalled();
    expect(api.listVersions).not.toHaveBeenCalled();
  });

  it("lists controller software above hashboard firmware with the containers on it", async () => {
    const api = controllerSoftwareApi("1.6.0");
    renderManage(channelWith(rigGroup, assignedContainerGroup), api);

    const row = await controllerRow();
    await within(row).findByRole("button", { name: "Controller software for proto CT1" });
    const rows = within(screen.getByRole("table", { name: "Assigned Proto Containers by model" })).getAllByRole("row");
    // Header, controller software, then the hashboard firmware it installs.
    expect(rows[1]).toBe(row);
    expect(rows[2]).toBe(screen.getByTestId("model-group-CT1"));

    const [model, count] = within(row).getAllByRole("cell");
    expect(model).toHaveTextContent("proto CT1");
    expect(model).toHaveTextContent("Controller software");
    expect(count).toHaveTextContent("3");
    expect(within(row).getByRole("button", { name: "Controller software for proto CT1" })).toHaveTextContent("1.6.0");
    expect(within(row).getByTestId("controller-software-status-CT1")).toHaveTextContent("1 of 3 on 1.6.0");
    expect(api.list).toHaveBeenCalledWith(1n, expect.any(AbortSignal));

    // The hashboard firmware row says why its update is waiting.
    expect(screen.getByTestId("controller-software-hold-CT1")).toHaveTextContent(
      "2 containers are held until their controller software is 1.6.0.",
    );
  });

  it("warns of no hold when no version is set or no firmware is assigned", async () => {
    const unset = controllerSoftwareApi("");
    const { unmount } = render(
      <MemoryRouter>
        <ReleaseChannelManageView
          {...manageViewProps()}
          channel={channelWith(assignedContainerGroup)}
          controllerSoftware={unset}
        />
      </MemoryRouter>,
    );
    const row = await controllerRow();
    expect(await within(row).findByRole("button", { name: "Controller software for proto CT1" })).toHaveTextContent(
      "Not set",
    );
    expect(screen.queryByTestId("controller-software-status-CT1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("controller-software-hold-CT1")).not.toBeInTheDocument();
    unmount();

    renderManage(channelWith(containerGroup), controllerSoftwareApi("1.6.0"));
    expect(await screen.findByTestId("controller-software-status-CT1")).toHaveTextContent("1 of 3 on 1.6.0");
    expect(screen.queryByTestId("controller-software-hold-CT1")).not.toBeInTheDocument();
  });

  it("shows the version read-only to a user who cannot set it", async () => {
    const api = controllerSoftwareApi("1.6.0", false);
    renderManage(channelWith(containerGroup), api);

    const row = await controllerRow();
    expect(await within(row).findByText("1.6.0")).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "Controller software for proto CT1" })).not.toBeInTheDocument();
    // Listing uploaded packages needs the controller software permission.
    expect(api.listVersions).not.toHaveBeenCalled();
  });

  it("sets the version after confirming, apart from the channel's pending changes", async () => {
    const api = controllerSoftwareApi("1.6.0");
    renderManage(channelWith(containerGroup), api);
    const row = await controllerRow();

    fireEvent.click(await within(row).findByRole("button", { name: "Controller software for proto CT1" }));
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual(["Not set", "1.6.0", "1.5.0"]);
    fireEvent.click(screen.getByRole("option", { name: "1.5.0" }));

    // Nothing is saved until confirmed, and nothing joins "Apply changes".
    expect(api.set).not.toHaveBeenCalled();
    expect(screen.queryByTestId("apply-firmware-changes")).not.toBeInTheDocument();
    const dialog = screen.getByTestId("controller-software-dialog");
    expect(dialog).toHaveTextContent("Require controller software 1.5.0?");
    expect(dialog).toHaveTextContent("This doesn't update any container.");

    const readsBefore = api.list.mock.calls.length;
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Set version" })));

    expect(api.set).toHaveBeenCalledExactlyOnceWith(1n, "proto", "CT1", "1.5.0");
    expect(screen.queryByTestId("controller-software-dialog")).not.toBeInTheDocument();
    expect(pushToast).toHaveBeenCalledWith({
      message: "Controller software version set to 1.5.0",
      status: "success",
    });
    await waitFor(() => expect(api.list.mock.calls.length).toBeGreaterThan(readsBefore));
  });

  it("does nothing when the current version is chosen again", async () => {
    const api = controllerSoftwareApi("v1.6.0");
    renderManage(channelWith(containerGroup), api);
    const row = await controllerRow();

    fireEvent.click(await within(row).findByRole("button", { name: "Controller software for proto CT1" }));
    fireEvent.click(await screen.findByRole("option", { name: "1.6.0" }));

    expect(screen.queryByTestId("controller-software-dialog")).not.toBeInTheDocument();
    expect(api.set).not.toHaveBeenCalled();
  });

  it("clears the version and keeps the dialog open when saving fails", async () => {
    const api = controllerSoftwareApi("1.6.0");
    api.set.mockRejectedValueOnce(new Error("permission denied"));
    renderManage(channelWith(containerGroup), api);
    const row = await controllerRow();

    fireEvent.click(await within(row).findByRole("button", { name: "Controller software for proto CT1" }));
    fireEvent.click(await screen.findByRole("option", { name: "Not set" }));
    const dialog = screen.getByTestId("controller-software-dialog");
    expect(dialog).toHaveTextContent("Stop requiring a controller software version?");
    expect(dialog).toHaveTextContent(
      "Firmware updates for proto CT1 in Production will no longer wait for controller software 1.6.0.",
    );

    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Clear version" })));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("permission denied");
    expect(pushToast).not.toHaveBeenCalled();

    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Clear version" })));
    expect(api.set).toHaveBeenLastCalledWith(1n, "proto", "CT1", "");
    expect(screen.queryByTestId("controller-software-dialog")).not.toBeInTheDocument();
    expect(pushToast).toHaveBeenCalledWith({ message: "Controller software version cleared", status: "success" });
  });

  it("lists each container with its version and a link to where it is updated", async () => {
    renderManage(channelWith(containerGroup), controllerSoftwareApi("1.6.0"));
    const row = await controllerRow();

    fireEvent.click(
      await within(row).findByRole("button", { name: "View proto CT1 controller software by container" }),
    );
    const modal = screen.getByTestId("controller-software-containers-modal");
    await within(modal).findByTestId("controller-software-container-ct1:a");
    expect(modal).toHaveTextContent("Update controller software from each container's page.");

    const onTarget = within(modal).getByTestId("controller-software-container-ct1:a");
    expect(within(onTarget).getByRole("link", { name: "Container A" })).toHaveAttribute("href", "/containers/ct1%3Aa");
    expect(onTarget).toHaveTextContent("On 1.6.0");
    const behind = within(modal).getByTestId("controller-software-container-ct1:b");
    expect(behind).toHaveTextContent("1.5.0");
    expect(behind).toHaveTextContent("Needs update");
    const unreported = within(modal).getByTestId("controller-software-container-ct1:c");
    expect(unreported).toHaveTextContent("Not reported");
    expect(unreported).toHaveTextContent("Needs update");
  });

  it("offers a retry when the versions cannot be read", async () => {
    const api = controllerSoftwareApi("1.6.0");
    api.list.mockRejectedValueOnce(new Error("network error"));
    renderManage(channelWith(containerGroup), api);
    const row = await controllerRow();

    expect(await within(row).findByRole("alert")).toHaveTextContent("network error");
    // The hashboard firmware row is unaffected.
    expect(screen.getByRole("button", { name: "Hashboard firmware for proto CT1" })).toBeInTheDocument();
    fireEvent.click(within(row).getByRole("button", { name: "Retry" }));
    expect(await within(row).findByRole("button", { name: "Controller software for proto CT1" })).toHaveTextContent(
      "1.6.0",
    );
  });
  it("keeps a saved version and warns when its summary refresh fails", async () => {
    const api = controllerSoftwareApi("1.6.0");
    renderManage(channelWith(assignedContainerGroup), api);
    const row = await controllerRow();
    fireEvent.click(await within(row).findByRole("button", { name: "Controller software for proto CT1" }));
    fireEvent.click(await screen.findByRole("option", { name: "1.5.0" }));
    api.list.mockRejectedValue(new Error("refresh failed"));
    await act(async () =>
      fireEvent.click(
        within(screen.getByTestId("controller-software-dialog")).getByRole("button", { name: "Set version" }),
      ),
    );
    expect(await within(row).findByRole("alert")).toHaveTextContent("refresh failed");
    expect(within(row).getByRole("button", { name: "Controller software for proto CT1" })).toHaveTextContent("1.5.0");
    expect(row).toHaveTextContent("Refreshing status");
    expect(within(row).queryByTestId("controller-software-status-CT1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("controller-software-hold-CT1")).not.toBeInTheDocument();
    // Restoring the previous version remains a real change.
    fireEvent.click(within(row).getByRole("button", { name: "Controller software for proto CT1" }));
    fireEvent.click(await screen.findByRole("option", { name: "1.6.0" }));
    expect(screen.getByTestId("controller-software-dialog")).toHaveTextContent("Require controller software 1.6.0?");
    fireEvent.click(within(screen.getByTestId("controller-software-dialog")).getByRole("button", { name: "Cancel" }));
    api.list.mockResolvedValue([software("1.5.0")]);
    fireEvent.click(within(row).getByRole("button", { name: "Retry" }));
    expect(await within(row).findByTestId("controller-software-status-CT1")).toHaveTextContent("1 of 3 on 1.5.0");
    expect(within(row).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps a saved version when an older in-flight read finishes", async () => {
    const api = controllerSoftwareApi("1.6.0");
    const props = { ...manageViewProps(), channel: channelWith(containerGroup), controllerSoftware: api };
    const { rerender } = render(
      <MemoryRouter>
        <ReleaseChannelManageView {...props} />
      </MemoryRouter>,
    );
    const row = await controllerRow();
    await within(row).findByRole("button", { name: "Controller software for proto CT1" });
    let finishOld!: (value: ReleaseChannelControllerSoftware[]) => void;
    api.list
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishOld = resolve;
          }),
      )
      .mockRejectedValue(new Error("new read failed"));
    rerender(
      <MemoryRouter>
        <ReleaseChannelManageView {...props} channel={{ ...props.channel }} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2));
    fireEvent.click(within(row).getByRole("button", { name: "Controller software for proto CT1" }));
    fireEvent.click(await screen.findByRole("option", { name: "1.5.0" }));
    await act(async () =>
      fireEvent.click(
        within(screen.getByTestId("controller-software-dialog")).getByRole("button", { name: "Set version" }),
      ),
    );
    await act(async () => finishOld([software("1.6.0")]));
    expect(await within(row).findByRole("alert")).toHaveTextContent("new read failed");
    expect(within(row).getByRole("button", { name: "Controller software for proto CT1" })).toHaveTextContent("1.5.0");
    expect(row).toHaveTextContent("Refreshing status");
  });

  it("loads details only on demand and retries a failed detail read", async () => {
    const api = controllerSoftwareApi("1.6.0");
    api.listContainers.mockRejectedValueOnce(new Error("details failed"));
    renderManage(channelWith(containerGroup), api);
    const row = await controllerRow();
    const button = await within(row).findByRole("button", { name: "View proto CT1 controller software by container" });
    expect(api.listContainers).not.toHaveBeenCalled();
    fireEvent.click(button);
    const modal = screen.getByTestId("controller-software-containers-modal");
    expect(await within(modal).findByRole("alert")).toHaveTextContent("details failed");
    fireEvent.click(within(modal).getByRole("button", { name: "Retry" }));
    expect(await within(modal).findByTestId("controller-software-container-ct1:b")).toHaveTextContent("Needs update");
    expect(api.listContainers).toHaveBeenLastCalledWith(1n, "proto", "CT1", expect.any(AbortSignal));
    fireEvent.click(within(modal).getByRole("button", { name: "Done" }));
    expect(screen.queryByTestId("controller-software-containers-modal")).not.toBeInTheDocument();
  });

  it("cancels a pending detail read when the modal closes", async () => {
    const api = controllerSoftwareApi("1.6.0");
    let finish!: (containers: Awaited<ReturnType<ControllerSoftwareApi["listContainers"]>>) => void;
    api.listContainers.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    renderManage(channelWith(containerGroup), api);
    fireEvent.click(
      await within(await controllerRow()).findByRole("button", {
        name: "View proto CT1 controller software by container",
      }),
    );
    await waitFor(() => expect(api.listContainers).toHaveBeenCalledTimes(1));
    const modal = screen.getByTestId("controller-software-containers-modal");
    expect(modal).toHaveTextContent("Loading containers");
    fireEvent.click(within(modal).getByRole("button", { name: "Done" }));
    expect(api.listContainers.mock.calls[0][3]?.aborted).toBe(true);
    await act(async () => finish([]));
    expect(screen.queryByTestId("controller-software-containers-modal")).not.toBeInTheDocument();
  });
});
