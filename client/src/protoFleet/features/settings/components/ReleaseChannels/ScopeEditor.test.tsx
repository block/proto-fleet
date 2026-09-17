import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ScopeEditor from "./ScopeEditor";
import {
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelScopeSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  BuildingSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
  RackSelectionModal,
  SiteSelectionModal,
} from "@/protoFleet/components/TargetSelectionModal";

const { permissions } = vi.hoisted(() => ({ permissions: new Set<string>() }));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: (permission: string) => permissions.has(permission) }));
// Placement dialogs fetch on mount. These stand-ins let us verify that a
// closed or unauthorized picker is never mounted, before any data hook runs.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: vi.fn(({ onSave }: { onSave: (selection: { siteIds: string[] }) => void }) => (
    <button onClick={() => onSave({ siteIds: ["8"] })}>Save sites</button>
  )),
  BuildingSelectionModal: vi.fn(() => <div>Building picker</div>),
  RackSelectionModal: vi.fn(() => <div>Rack picker</div>),
  GroupSelectionModal: vi.fn(() => <div>Group picker</div>),
  MinerSelectionModal: vi.fn(() => <div>Miner picker</div>),
}));

const pickers = [
  SiteSelectionModal,
  BuildingSelectionModal,
  RackSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
];
const labels = ["Sites", "Buildings", "Racks", "Groups", "Miners"];
const scope = create(ReleaseChannelScopeSchema, {
  siteIds: [1n],
  buildingIds: [2n],
  rackIds: [3n],
  groupIds: [4n],
  deviceIdentifiers: ["rig-5"],
});
const previewScope = vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 5 }));

beforeEach(() => {
  permissions.clear();
  vi.clearAllMocks();
});

describe("release-channel scope permissions", () => {
  it.each([
    { permission: "miner:firmware_update", visible: [] },
    { permission: "site:read", visible: ["Sites", "Buildings"] },
    { permission: "rack:read", visible: ["Racks", "Groups"] },
    { permission: "miner:read", visible: ["Miners"] },
  ])("shows only selectors permitted by $permission without mounting closed dialogs", ({ permission, visible }) => {
    permissions.add(permission);
    render(<ScopeEditor scope={scope} onChange={vi.fn()} previewScope={previewScope} />);
    for (const label of labels) {
      const button = screen.queryByRole("button", { name: new RegExp(`^${label} `) });
      if (visible.includes(label)) expect(button).toBeInTheDocument();
      else expect(button).not.toBeInTheDocument();
    }
    for (const picker of pickers) expect(picker).not.toHaveBeenCalled();
  });

  it("keeps existing unreadable selections in the live preview and when another dimension changes", async () => {
    permissions.add("site:read");
    const onChange = vi.fn();
    render(<ScopeEditor scope={scope} onChange={onChange} previewScope={previewScope} editingExistingChannel />);
    await waitFor(() => expect(previewScope).toHaveBeenCalledWith(scope));
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("1 site, 1 building, 1 rack, 1 group, 1 miner");
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    expect(SiteSelectionModal).toHaveBeenCalled();
    for (const picker of pickers.slice(1)) expect(picker).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save sites" }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(create(ReleaseChannelScopeSchema, { ...scope, siteIds: [8n] }));
  });

  it("hides unreadable rack and group filters inside the permitted miner picker", () => {
    permissions.add("miner:read");
    render(<ScopeEditor scope={scope} onChange={vi.fn()} previewScope={previewScope} />);
    fireEvent.click(screen.getByRole("button", { name: /^Miners / }));
    const calls = vi.mocked(MinerSelectionModal).mock.calls;
    expect(calls[calls.length - 1]?.[0]).toMatchObject({
      selectedMinerIds: ["rig-5"],
      filterConfig: { showRackFilter: false, showGroupFilter: false },
    });
    for (const picker of pickers.slice(0, 4)) expect(picker).not.toHaveBeenCalled();
  });

  it("unmounts an open picker when its read permission is revoked", () => {
    permissions.add("site:read");
    const props = { scope, onChange: vi.fn(), previewScope };
    const { rerender } = render(<ScopeEditor {...props} />);
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    expect(screen.getByRole("button", { name: "Save sites" })).toBeInTheDocument();
    permissions.clear();
    rerender(<ScopeEditor {...props} />);
    expect(screen.queryByRole("button", { name: "Save sites" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Sites / })).not.toBeInTheDocument();
    expect(props.onChange).not.toHaveBeenCalled();
  });

  it("explains that an empty channel is allowed without catalog-read permission", () => {
    const onChange = vi.fn();
    render(<ScopeEditor scope={create(ReleaseChannelScopeSchema)} onChange={onChange} previewScope={previewScope} />);
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("You can save an empty channel.");
    expect(onChange).not.toHaveBeenCalled();
    for (const picker of pickers) expect(picker).not.toHaveBeenCalled();
  });
});
