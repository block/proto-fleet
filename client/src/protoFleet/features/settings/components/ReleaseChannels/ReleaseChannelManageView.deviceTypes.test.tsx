import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { manageViewProps, openChannelSettings } from "./__tests__/helpers";
import { defaultBehavior } from "./behaviorUtils";
import ReleaseChannelManageView from "./ReleaseChannelManageView";
import { DEVICE_NOUNS } from "./rolloutStatus";
import {
  ReleaseChannelMinerSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
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

const rigGroup = create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model: "Rig", minerCount: 4 });
const s21Group = create(ReleaseChannelModelGroupSchema, { manufacturer: "Bitmain", model: "S21", minerCount: 2 });
// Observed spelling varies; the container is still recognized by its model pair.
const containerGroup = create(ReleaseChannelModelGroupSchema, { manufacturer: "proto", model: "CT1", minerCount: 3 });

const hashboardFile: FirmwareFileInfo = {
  id: "hashboard-1.39.0",
  filename: "hashboard-firmware-1.39.0-prod.zip",
  size: 100,
  uploaded_at: "2026-11-12T00:00:00Z",
  target_manufacturer: "Proto",
  target_model: "CT1",
  firmware_version: "1.39.0",
  hashboard_build: "prod",
};

const channelWith = (...modelGroups: ChannelView["modelGroups"]): ChannelView => ({
  ...create(ReleaseChannelSchema, {
    id: 1n,
    name: "Production",
    scope: { siteIds: [1n] },
    behavior: defaultBehavior(),
  }),
  modelGroups,
});

function renderManage(channel: ChannelView, firmwareFiles: FirmwareFileInfo[] = []) {
  const props = { ...manageViewProps(), channel, firmwareFiles };
  render(<ReleaseChannelManageView {...props} />);
  return props;
}

const minersTable = () => screen.queryByRole("table", { name: "Assigned miners by model" });
const containersTable = () => screen.queryByRole("table", { name: "Assigned Proto Containers by model" });
const headers = (table: HTMLElement) =>
  within(table)
    .getAllByRole("columnheader")
    .map((header) => header.textContent);

describe("release channel assignments by device type", () => {
  // The firmware picker positions its options from the trigger's layout box.
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 120, 40));
  });
  afterEach(() => vi.restoreAllMocks());

  it("keeps the single miner table for a channel without containers", () => {
    renderManage(channelWith(rigGroup, s21Group));

    expect(screen.getByText("Assigned miners")).toBeInTheDocument();
    expect(screen.queryByText("Assigned Proto Containers")).not.toBeInTheDocument();
    expect(containersTable()).not.toBeInTheDocument();
    expect(headers(minersTable()!)).toEqual(["Model", "Miners", "Firmware", "Update status", "Actions"]);
    expect(within(minersTable()!).getAllByRole("row")).toHaveLength(3);
  });

  it("lists containers in their own table, apart from miners", () => {
    renderManage(channelWith(rigGroup, containerGroup, s21Group));

    const miners = minersTable()!;
    const containers = containersTable()!;
    expect(within(miners).getByTestId("model-group-Rig")).toBeInTheDocument();
    expect(within(miners).getByTestId("model-group-S21")).toBeInTheDocument();
    expect(within(miners).queryByTestId("model-group-CT1")).not.toBeInTheDocument();
    expect(within(miners).getAllByRole("button", { name: "View miners" })).toHaveLength(2);

    expect(screen.getByText("Assigned Proto Containers")).toBeInTheDocument();
    expect(headers(containers)).toEqual(["Model", "Containers", "Version", "Update status", "Actions"]);
    const row = within(containers).getByTestId("model-group-CT1");
    // The row names the container component its version applies to.
    const [model, count] = within(row).getAllByRole("cell");
    expect(model).toHaveTextContent("proto CT1");
    expect(model).toHaveTextContent("Hashboard firmware");
    expect(count).toHaveTextContent("3");
    expect(within(miners).queryByText("Hashboard firmware")).not.toBeInTheDocument();
    expect(within(row).getByRole("button", { name: "View containers" })).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "View miners" })).not.toBeInTheDocument();
    expect(within(row).getByRole("button", { name: "Hashboard firmware for proto CT1" })).toBeInTheDocument();
  });

  it("shows only the container table when every model is a container", () => {
    renderManage(channelWith(containerGroup));

    expect(containersTable()).toBeInTheDocument();
    expect(minersTable()).not.toBeInTheDocument();
    expect(screen.queryByText("Assigned miners")).not.toBeInTheDocument();
  });

  it("keeps the miner section and its empty state when nothing is in scope", () => {
    renderManage(channelWith());

    expect(screen.getByText("Assigned miners")).toBeInTheDocument();
    expect(screen.getByText(/No miners in scope yet/)).toBeInTheDocument();
    expect(screen.queryByText("Assigned Proto Containers")).not.toBeInTheDocument();
  });

  it.each([
    ["miners only", [rigGroup, s21Group], "miner", "miners"],
    ["containers only", [containerGroup], "container", "containers"],
    ["miners and containers", [rigGroup, containerGroup], "device", "devices"],
  ] as const)("words channel settings for a channel of %s", (_, groups, singular, plural) => {
    renderManage(channelWith(...groups));
    openChannelSettings();
    const settings = within(screen.getByTestId("channel-settings-modal"));

    expect(settings.getByText(`Every ${singular} not on the new version updates at once.`)).toBeInTheDocument();
    expect(settings.getByLabelText(`Max ${plural} offline at once (0 for no limit)`)).toBeInTheDocument();
    expect(
      settings.getByText(`Choose which ${plural} belong to this channel. Changes stay pending until you apply them.`),
    ).toBeInTheDocument();
  });

  it("falls back to the fleet's wording for a channel with nothing in scope", () => {
    const props = { ...manageViewProps(), channel: channelWith(), firmwareFiles: [] };
    const { rerender } = render(<ReleaseChannelManageView {...props} />);
    expect(screen.getByText(/No miners in scope yet/)).toBeInTheDocument();

    rerender(<ReleaseChannelManageView {...props} fleetNouns={DEVICE_NOUNS} />);
    expect(
      screen.getByText("No devices in scope yet. Open Channel settings and change Applies to to include devices."),
    ).toBeInTheDocument();
  });

  it("assigns hashboard firmware to a container model", async () => {
    const { onApply } = renderManage(channelWith(rigGroup, containerGroup), [hashboardFile]);

    fireEvent.click(screen.getByTestId("channel-firmware-select-CT1"));
    fireEvent.click(screen.getByRole("option", { name: /1\.39\.0/ }));
    fireEvent.click(screen.getByTestId("apply-firmware-changes"));
    const dialog = screen.getByTestId("apply-firmware-dialog");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Start update" })));

    expect(onApply).toHaveBeenCalledExactlyOnceWith(1n, [
      { manufacturer: "Proto", model: "CT1", firmwareFileId: hashboardFile.id },
    ]);
  });

  it("opens a container model's members worded as containers", async () => {
    const props = { ...manageViewProps(), channel: channelWith(rigGroup, containerGroup), firmwareFiles: [] };
    props.listChannelMiners.mockResolvedValue([
      create(ReleaseChannelMinerSchema, { deviceIdentifier: "ct1:cluster-a", manufacturer: "proto", model: "CT1" }),
    ]);
    props.minerNames = { "ct1:cluster-a": "Container A" };
    render(<ReleaseChannelManageView {...props} />);

    fireEvent.click(screen.getByRole("button", { name: "View containers" }));

    expect(await screen.findByText("proto CT1 containers")).toBeInTheDocument();
    expect(await screen.findByText("Container A")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Container" })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Miner" })).not.toBeInTheDocument();
    expect(props.listChannelMiners).toHaveBeenCalledWith(1n, "proto", "CT1", expect.any(AbortSignal));
  });
});
