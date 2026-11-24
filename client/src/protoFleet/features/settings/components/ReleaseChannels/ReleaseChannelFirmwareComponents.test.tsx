import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { manageViewProps } from "./__tests__/helpers";
import { defaultBehavior } from "./behaviorUtils";
import type { ControllerSoftwareApi } from "./controllerSoftwareUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import { modelCount, pairKey, pairLabel } from "./rolloutStatus";
import {
  ReleaseChannelControllerSoftwareSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  RolloutCancelReason,
  RolloutSchema,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  acknowledgeRollout,
  isRollbackAcknowledged,
  isRolloutSuperseded,
} from "@/protoFleet/api/rollbackAcknowledgements";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";

vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useHasPermission: vi.fn(() => true),
}));

vi.mock("@/protoFleet/api/useContainers", () => {
  const catalog = { containers: [], error: undefined, loading: false, refetch: () => {} };
  return { useContainers: () => catalog };
});

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error" },
}));

// A CT1 container has two firmware components: its hashboard firmware, which
// is the model's main firmware, and its PSU firmware.
const archive: FirmwareFileInfo = {
  id: "archive",
  filename: "hashboard-firmware-1.39.0-prod.zip",
  size: 100,
  uploaded_at: "2026-09-17T00:00:00Z",
  target_manufacturer: "Proto",
  target_model: "CT1",
  firmware_version: "1.39.0",
  hashboard_build: "prod",
};
const capsule: FirmwareFileInfo = {
  id: "capsule",
  filename: "boco-ha1200000-0123456789ab.psucap",
  size: 100,
  uploaded_at: "2026-09-17T00:00:00Z",
  target_manufacturer: "Proto",
  target_model: "CT1",
  firmware_version: "app=1410;bl=1110",
  component: "psu",
};

const hashboards = create(ReleaseChannelModelGroupSchema, {
  manufacturer: "Proto",
  model: "CT1",
  minerCount: 12,
  firmwareFileId: archive.id,
  firmwareChecksum: "a".repeat(64),
  firmwareVersion: "1.39.0",
  firmwareTargetManufacturer: "Proto",
  firmwareTargetModel: "CT1",
  firmwareAvailable: true,
  assignmentGeneration: 5n,
  onTargetCount: 12,
  reportedVersions: ["1.39.0"],
  reportedVersionCount: 1,
});
const psu = create(ReleaseChannelModelGroupSchema, {
  manufacturer: "Proto",
  model: "CT1",
  component: "psu",
  minerCount: 12,
  firmwareFileId: capsule.id,
  firmwareChecksum: "b".repeat(64),
  firmwareVersion: "app=1410;bl=1110",
  firmwareTargetManufacturer: "Proto",
  firmwareTargetModel: "CT1",
  firmwareAvailable: true,
  assignmentGeneration: 1n,
  onTargetCount: 3,
  reportedVersions: ["app=1310;bl=1110", "app=1410;bl=1110"],
  reportedVersionCount: 2,
});

const channel = (...modelGroups: ChannelView["modelGroups"]): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Production",
    scope: { siteIds: [1n] },
    behavior: defaultBehavior(),
    minerCount: 12,
  }),
  modelGroups,
});

function renderManage(view: ChannelView, firmwareFiles: FirmwareFileInfo[]) {
  const props = { ...manageViewProps(), channel: view, firmwareFiles };
  render(<ReleaseChannelManageView {...props} />);
  return props;
}

const options = () => screen.getAllByRole("option").map((option) => option.textContent);

// Review the staged changes and confirm them with the dialog's primary action.
async function applyStaged(button: string) {
  fireEvent.click(screen.getByTestId("apply-firmware-changes"));
  const dialog = screen.getByTestId("apply-firmware-dialog");
  const text = dialog.textContent ?? "";
  await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: button })));
  return text;
}

describe("release channel firmware components", () => {
  // The firmware picker positions its options against its button.
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });
  afterEach(() => vi.restoreAllMocks());

  test("offers a model's PSU firmware once a capsule for it is uploaded", () => {
    const { unmount } = render(
      <ReleaseChannelManageView {...manageViewProps()} channel={channel(hashboards)} firmwareFiles={[archive]} />,
    );
    expect(screen.getByTestId("model-group-CT1")).toHaveTextContent("Proto CT1Hashboard firmware");
    expect(screen.queryByTestId("model-group-CT1-psu")).not.toBeInTheDocument();
    unmount();

    renderManage(channel(hashboards), [archive, capsule]);
    // A container model lists each firmware component as a row under it.
    const row = screen.getByTestId("model-group-CT1-psu");
    expect(row).toHaveTextContent("Proto CT1PSU firmware");
    expect(row).toHaveTextContent("12");
    expect(row).toHaveTextContent("No firmware assigned");
    expect(screen.getByRole("button", { name: "PSU firmware for Proto CT1" })).toBeInTheDocument();
    expect(screen.getByTestId("release-channel-Production")).toHaveTextContent("1 model");
    expect(screen.getByTestId("release-channel-Production")).not.toHaveTextContent("2 models");

    // Each row offers only firmware for its own component.
    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1"));
    expect(options().join("|")).toContain("1.39.0");
    expect(options().join("|")).not.toContain("app=1410;bl=1110");
    fireEvent.click(screen.getByRole("option", { name: /1\.39\.0/ }));
    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1-psu"));
    expect(options().join("|")).toContain("app=1410;bl=1110");
    expect(options().join("|")).not.toContain("1.39.0");
  });

  test("assigns a capsule as the model's PSU component and leaves the hashboard assignment alone", async () => {
    const { onApply } = renderManage(channel(hashboards), [archive, capsule]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1-psu"));
    fireEvent.click(screen.getByRole("option", { name: /app=1410;bl=1110/ }));

    const review = await applyStaged("Start update");
    expect(review).toContain("Proto CT1 PSU");
    expect(review).toContain("app=1410;bl=1110");
    expect(review).toContain("A module stops mining while its PSU updates");
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "CT1", component: "psu", firmwareFileId: "capsule" },
    ]);
  });

  test("shows an assigned PSU component as its own row with its own status", async () => {
    const { onApply } = renderManage(channel(hashboards, psu), [archive, capsule]);
    expect(screen.getByTestId("model-group-CT1")).toHaveTextContent("Up to date");
    const row = screen.getByTestId("model-group-CT1-psu");
    expect(row).toHaveTextContent("Proto CT1PSU firmware");
    expect(row).toHaveTextContent("3 of 12 on target");
    expect(screen.getByTestId("channel-firmware-select-CT1-psu")).toHaveTextContent("app=1410;bl=1110");
    expect(screen.getByTestId("channel-firmware-select-CT1")).toHaveTextContent("1.39.0");
    expect(screen.getByTestId("release-channel-Production")).toHaveTextContent("1 model");

    // Clearing the PSU assignment names the component, so nothing else clears.
    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1-psu"));
    fireEvent.click(screen.getByRole("option", { name: /No firmware/ }));
    const review = await applyStaged("Clear assignments");
    expect(review).toContain("Proto CT1 PSU");
    expect(review).not.toContain("A module stops mining while its PSU updates");
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "CT1", component: "psu", firmwareFileId: "" },
    ]);
  });

  test("an assignment of the model's main firmware names no component", async () => {
    const newer: FirmwareFileInfo = { ...archive, id: "newer", firmware_version: "1.40.0" };
    const { onApply } = renderManage(channel(hashboards, psu), [archive, newer, capsule]);
    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1"));
    fireEvent.click(screen.getByRole("option", { name: /1\.40\.0/ }));
    const review = await applyStaged("Start update");
    expect(review).not.toContain("Proto CT1 PSU");
    expect(review).not.toContain("A module stops mining while its PSU updates");
    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "CT1", firmwareFileId: "newer" },
    ]);
  });

  test("reads a row's miners for its component", async () => {
    const { listChannelMiners } = renderManage(channel(hashboards, psu), [archive, capsule]);
    fireEvent.click(screen.getByTestId("view-miners-CT1-psu"));
    expect(await screen.findByText("PSU firmware on Proto CT1 containers")).toBeInTheDocument();
    await waitFor(() =>
      expect(listChannelMiners).toHaveBeenCalledExactlyOnceWith(1n, "Proto", "CT1", "psu", expect.any(AbortSignal)),
    );
  });

  // The controller installs every firmware a channel sends a container, so a
  // container behind the target is held for each component.
  test("says a PSU row waits for controller software as the hashboard row does", async () => {
    const controllerSoftware: ControllerSoftwareApi = {
      canSet: true,
      list: vi.fn<ControllerSoftwareApi["list"]>().mockResolvedValue([
        create(ReleaseChannelControllerSoftwareSchema, {
          manufacturer: "Proto",
          model: "CT1",
          targetVersion: "1.6.0",
          containerCount: 12,
          onTargetCount: 11,
        }),
      ]),
      listContainers: vi.fn<ControllerSoftwareApi["listContainers"]>().mockResolvedValue([]),
      set: vi.fn<ControllerSoftwareApi["set"]>().mockResolvedValue(undefined),
      listVersions: vi.fn<ControllerSoftwareApi["listVersions"]>().mockResolvedValue(["1.6.0"]),
    };
    render(
      <MemoryRouter>
        <ReleaseChannelManageView
          {...manageViewProps()}
          channel={channel(hashboards, psu)}
          firmwareFiles={[archive, capsule]}
          controllerSoftware={controllerSoftware}
        />
      </MemoryRouter>,
    );

    const held = "1 container is held until its controller software is 1.6.0.";
    expect(await screen.findByTestId("controller-software-hold-CT1-psu")).toHaveTextContent(held);
    expect(screen.getByTestId("controller-software-hold-CT1")).toHaveTextContent(held);
    // The target is set once per model, on the row above its firmware rows.
    const rows = within(screen.getByRole("table", { name: "Assigned Proto Containers by model" })).getAllByRole("row");
    expect(rows.slice(1)).toEqual([
      screen.getByTestId("controller-software-row-CT1"),
      screen.getByTestId("model-group-CT1"),
      screen.getByTestId("model-group-CT1-psu"),
    ]);
  });

  test("lists a model's components under one model in the channels table", () => {
    render(<ReleaseChannelsTable channels={[channel(hashboards, psu)]} rollouts={[]} onManage={vi.fn()} />);
    expect(screen.getByTestId("channels-table")).toHaveTextContent("1 model");
    fireEvent.click(screen.getByTestId("channel-toggle-Production"));
    expect(screen.getByTestId("model-row-Production-CT1")).toHaveTextContent(/^Proto CT1$/);
    expect(screen.getByTestId("model-row-Production-CT1-psu")).toHaveTextContent(/^Proto CT1 PSU$/);
    expect(screen.getByTestId("model-status-Production-CT1")).toHaveTextContent("Up to date");
    expect(screen.getByTestId("model-status-Production-CT1-psu")).toHaveTextContent("3 of 12 on target");
  });

  test("names and keys each component of a pair separately", () => {
    expect(pairLabel(hashboards)).toBe("Proto CT1");
    expect(pairLabel(psu)).toBe("Proto CT1 PSU");
    expect(pairLabel({ manufacturer: "", model: "", component: "psu" })).toBe("Unknown model PSU");
    expect(pairKey(psu)).not.toBe(pairKey(hashboards));
    expect(pairKey({ manufacturer: " proto ", model: "ct1", component: "psu" })).toBe(pairKey(psu));
    expect(pairKey({ manufacturer: "Proto", model: "CT1" })).toBe(pairKey(hashboards));
    expect(modelCount([hashboards, psu])).toBe(1);
    expect(modelCount([hashboards, psu, { manufacturer: "Proto", model: "Rig" }])).toBe(2);
  });

  // Each component has its own assignment generations. A newer hashboard
  // assignment says nothing about a PSU rollout of the same model.
  test("keeps rollback and supersede acknowledgements per component", () => {
    const rollout = (component: string, assignmentGeneration: bigint) =>
      create(RolloutSchema, {
        id: assignmentGeneration,
        channelId: 1n,
        manufacturer: "Proto",
        model: "CT1",
        component,
        assignmentGeneration,
        status: RolloutStatus.ACTIVE,
      });
    const psuRollout = rollout("psu", 1n);
    const channels = [channel(hashboards, psu)];

    expect(hashboards.assignmentGeneration).toBeGreaterThan(psuRollout.assignmentGeneration);
    expect(isRolloutSuperseded(psuRollout, channels, [rollout("", 9n)])).toBe(false);
    expect(acknowledgeRollout(psuRollout, [rollout("", 9n)], channels, [rollout("", 9n)])).toBe(psuRollout);
    expect(isRollbackAcknowledged(psuRollout, [rollout("", 9n)])).toBe(false);

    expect(isRolloutSuperseded(psuRollout, [channel(hashboards, { ...psu, assignmentGeneration: 2n })], [])).toBe(true);
    expect(isRolloutSuperseded(psuRollout, channels, [rollout("psu", 2n)])).toBe(true);
    expect(isRollbackAcknowledged(psuRollout, [rollout("psu", 1n)])).toBe(true);
    expect(acknowledgeRollout(psuRollout, [rollout("psu", 1n)], channels, []).cancelReason).toBe(
      RolloutCancelReason.ROLLED_BACK,
    );
  });
});
