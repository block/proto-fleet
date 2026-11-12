import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { deferred } from "./__tests__/helpers";
import ContainerSelectionModal from "./ContainerSelectionModal";
import ScopeEditor from "./ScopeEditor";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  type PreviewReleaseChannelScopeResponse,
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

const { permissions, selections, useContainers } = vi.hoisted(() => ({
  useContainers: vi.fn(),
  permissions: new Set<string>(),
  selections: {
    siteIds: ["8"],
    buildingIds: [] as string[],
    rackIds: [] as string[],
    groupIds: [] as string[],
    deviceIdentifiers: [] as string[],
    containerIdentifiers: [] as string[],
  },
}));
vi.mock("@/protoFleet/api/useContainers", () => ({ useContainers }));
vi.mock("./ContainerSelectionModal", () => ({
  default: vi.fn(({ onSave }: { onSave: (identifiers: string[]) => void }) => (
    <button onClick={() => onSave(selections.containerIdentifiers)}>Save containers</button>
  )),
}));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: (permission: string) => permissions.has(permission) }));
// Placement dialogs fetch on mount. These stand-ins let us verify that a
// closed or unauthorized picker is never mounted, before any data hook runs.
vi.mock("@/protoFleet/components/TargetSelectionModal", () => ({
  SiteSelectionModal: vi.fn(({ onSave }: { onSave: (selection: { siteIds: string[] }) => void }) => (
    <button onClick={() => onSave({ siteIds: selections.siteIds })}>Save sites</button>
  )),
  BuildingSelectionModal: vi.fn(({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(selections.buildingIds)}>Save buildings</button>
  )),
  RackSelectionModal: vi.fn(({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(selections.rackIds)}>Save racks</button>
  )),
  GroupSelectionModal: vi.fn(({ onSave }: { onSave: (ids: string[]) => void }) => (
    <button onClick={() => onSave(selections.groupIds)}>Save groups</button>
  )),
  MinerSelectionModal: vi.fn(({ onSave }: { onSave: (selection: { selectedMinerIds: string[] }) => void }) => (
    <button onClick={() => onSave({ selectedMinerIds: selections.deviceIdentifiers })}>Save miners</button>
  )),
}));

const pickers = [
  SiteSelectionModal,
  BuildingSelectionModal,
  RackSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
  ContainerSelectionModal,
];
const labels = ["Sites", "Buildings", "Racks", "Groups", "Miners", "Containers"];
const catalog = (...identifiers: string[]) => ({
  containers: identifiers.map((deviceIdentifier) => create(ContainerSchema, { deviceIdentifier })),
  error: undefined,
  loading: false,
});
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
  selections.siteIds = ["8"];
  selections.buildingIds = [];
  selections.rackIds = [];
  selections.groupIds = [];
  selections.deviceIdentifiers = [];
  selections.containerIdentifiers = [];
  vi.clearAllMocks();
  useContainers.mockReturnValue(catalog());
});

describe("release-channel scope preview cancellation", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  const tick = (ms = 300) => act(async () => vi.advanceTimersByTimeAsync(ms));
  const deferredPreview = deferred<PreviewReleaseChannelScopeResponse>;

  it("cancels obsolete debounce timers without starting requests", async () => {
    const preview = vi.fn();
    const props = { onChange: vi.fn(), previewScope: preview };
    const { rerender, unmount } = render(<ScopeEditor {...props} scope={scope} />);
    await tick(299);
    expect(preview).not.toHaveBeenCalled();
    rerender(<ScopeEditor {...props} scope={create(ReleaseChannelScopeSchema, { siteIds: [2n] })} />);
    await tick(299);
    expect(preview).not.toHaveBeenCalled();
    unmount();
    await tick();
    expect(preview).not.toHaveBeenCalled();
  });

  it.each(["scope", "loader", "empty scope", "invalid scope", "unmount"])(
    "aborts the old request after a changed %s and suppresses its late error",
    async (change) => {
      const pending = deferredPreview();
      const result = create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 6 });
      const preview = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue(result);
      const replacement = vi.fn().mockResolvedValue(result);
      const onPreview = vi.fn();
      const props = { onChange: vi.fn(), previewScope: preview, onPreview, scope };
      const { rerender, unmount } = render(<ScopeEditor {...props} />);
      await tick();
      const signal = preview.mock.calls[0][1] as AbortSignal;
      expect(signal).toBeInstanceOf(AbortSignal);
      expect(signal.aborted).toBe(false);

      if (change === "unmount") unmount();
      else if (change === "loader") rerender(<ScopeEditor {...props} previewScope={replacement} />);
      else {
        const next = create(ReleaseChannelScopeSchema, {
          siteIds:
            change === "scope"
              ? [2n]
              : change === "invalid scope"
                ? Array.from({ length: 101 }, (_, i) => BigInt(i + 1))
                : [],
        });
        rerender(<ScopeEditor {...props} scope={next} />);
      }
      expect(signal.aborted).toBe(true);
      const notifications = onPreview.mock.calls.length;
      await act(async () => pending.reject(new ConnectError("old session expired", Code.Unauthenticated)));
      expect(screen.queryByText(/old session expired/)).not.toBeInTheDocument();
      expect(onPreview).toHaveBeenCalledTimes(notifications);
      await tick();
      if (change === "scope" || change === "loader") {
        expect(screen.getByTestId("scope-preview")).toHaveTextContent("covers 6 miners");
        expect(onPreview).toHaveBeenLastCalledWith(result);
        const currentSignal = (
          change === "scope" ? preview.mock.calls[1][1] : replacement.mock.calls[0][1]
        ) as AbortSignal;
        expect(currentSignal).not.toBe(signal);
        expect(currentSignal.aborted).toBe(false);
      } else {
        expect(preview).toHaveBeenCalledOnce();
        expect(onPreview).not.toHaveBeenCalledWith(result);
      }
    },
  );

  it("shows a deadline failure and retries with a fresh request signal", async () => {
    const result = create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 7 });
    const retry = deferredPreview();
    const preview = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<PreviewReleaseChannelScopeResponse>((_resolve, reject) => {
            setTimeout(() => reject(new ConnectError("preview deadline exceeded", Code.DeadlineExceeded)), 30_000);
          }),
      )
      .mockReturnValueOnce(retry.promise);
    const onPreview = vi.fn();
    render(<ScopeEditor scope={scope} onChange={vi.fn()} previewScope={preview} onPreview={onPreview} />);
    await tick();
    const firstSignal = preview.mock.calls[0][1] as AbortSignal;
    await tick(29_999);
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving");
    expect(screen.queryByRole("button", { name: "Retry preview" })).not.toBeInTheDocument();
    await tick(1);
    expect(screen.getByRole("alert")).toHaveTextContent("preview deadline exceeded");
    expect(onPreview).toHaveBeenLastCalledWith(null);
    fireEvent.click(screen.getByRole("button", { name: "Retry preview" }));
    expect(firstSignal.aborted).toBe(true);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving");
    await tick();
    const retrySignal = preview.mock.calls[1][1] as AbortSignal;
    expect(retrySignal).not.toBe(firstSignal);
    expect(retrySignal.aborted).toBe(false);
    await act(async () => retry.resolve(result));
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("covers 7 miners");
    expect(onPreview).toHaveBeenLastCalledWith(result);
  });
});

describe("release-channel scope bounds", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    for (const permission of ["site:read", "rack:read", "miner:read"]) permissions.add(permission);
  });
  afterEach(() => vi.useRealTimers());

  const resolvePreview = async () =>
    act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

  it.each([
    ["Sites", "sites", "siteIds", 100, SiteSelectionModal, "selectedSiteIds"],
    ["Buildings", "buildings", "buildingIds", 100, BuildingSelectionModal, "selectedBuildingIds"],
    ["Racks", "racks", "rackIds", 500, RackSelectionModal, "selectedRackIds"],
    ["Groups", "groups", "groupIds", 100, GroupSelectionModal, "selectedGroupIds"],
    ["Miners", "miners and containers", "deviceIdentifiers", 10000, MinerSelectionModal, "selectedMinerIds"],
  ] as const)(
    "retains oversized %s (%s), skips invalid previews, and recovers at the limit",
    async (label, noun, field, limit, picker, selectionProp) => {
      const result = create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 5 });
      const preview = vi.fn().mockResolvedValue(result);
      const onPreview = vi.fn();
      const changed = vi.fn();
      function Editor() {
        const [draft, setDraft] = useState(create(ReleaseChannelScopeSchema));
        return (
          <ScopeEditor
            scope={draft}
            onChange={(next) => {
              changed(next);
              setDraft(next);
            }}
            previewScope={preview}
            onPreview={onPreview}
          />
        );
      }
      render(<Editor />);
      const selectCount = (count: number) => {
        selections[field] = Array.from({ length: count }, (_, index) => String(index + 1));
        fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${label} `) }));
        fireEvent.click(screen.getByRole("button", { name: `Save ${label.toLowerCase()}` }));
      };
      selectCount(limit);
      await resolvePreview();
      expect(preview).toHaveBeenCalledOnce();
      expect(preview.mock.calls[0][0][field]).toHaveLength(limit);
      expect(onPreview).toHaveBeenLastCalledWith(result);
      selectCount(limit + 1);
      expect(changed.mock.calls[changed.mock.calls.length - 1][0][field]).toHaveLength(limit + 1);
      expect(screen.getByRole("alert")).toHaveTextContent(
        `Select no more than ${limit.toLocaleString()} ${noun} (${(limit + 1).toLocaleString()} selected).`,
      );
      expect(onPreview).toHaveBeenLastCalledWith(null);
      await resolvePreview();
      expect(preview).toHaveBeenCalledOnce();
      fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${label} `) }));
      const calls = vi.mocked(picker).mock.calls;
      expect(calls[calls.length - 1][0]).toMatchObject({ [selectionProp]: selections[field] });
      selections[field] = selections[field].slice(0, limit);
      fireEvent.click(screen.getByRole("button", { name: `Save ${label.toLowerCase()}` }));
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      await resolvePreview();
      expect(preview).toHaveBeenCalledTimes(2);
      expect(preview.mock.calls[1][0][field]).toHaveLength(limit);
      expect(onPreview).toHaveBeenLastCalledWith(result);
    },
  );

  it("keeps oversized permission-hidden IDs when another dimension changes", async () => {
    permissions.delete("rack:read");
    const hiddenScope = create(ReleaseChannelScopeSchema, {
      siteIds: [1n],
      rackIds: Array.from({ length: 501 }, (_, i) => BigInt(i + 1)),
    });
    const onChange = vi.fn();
    const preview = vi.fn();
    render(<ScopeEditor scope={hiddenScope} onChange={onChange} previewScope={preview} />);
    expect(screen.queryByRole("button", { name: /^Racks / })).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Select no more than 500 racks (501 selected).");
    fireEvent.click(screen.getByRole("button", { name: /^Sites / }));
    fireEvent.click(screen.getByRole("button", { name: "Save sites" }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(
      create(ReleaseChannelScopeSchema, { ...hiddenScope, siteIds: [8n] }),
    );
    await resolvePreview();
    expect(preview).not.toHaveBeenCalled();
  });

  it("labels the last valid preview and suppresses stale conflicts and late responses after invalidation", async () => {
    const oldResult = create(PreviewReleaseChannelScopeResponseSchema, {
      minerCount: 5,
      conflicts: [{ channelId: 2n, channelName: "Other channel", minerCount: 5 }],
      conflictCount: 1,
    });
    let finishPending!: (value: typeof oldResult) => void;
    const pending = new Promise<typeof oldResult>((resolve) => {
      finishPending = resolve;
    });
    const preview = vi
      .fn()
      .mockResolvedValueOnce(oldResult)
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 6 }));
    const onPreview = vi.fn();
    const props = { onChange: vi.fn(), previewScope: preview, onPreview };
    const { rerender } = render(
      <ScopeEditor {...props} scope={create(ReleaseChannelScopeSchema, { siteIds: [1n] })} />,
    );
    await resolvePreview();
    expect(screen.getByTestId("scope-conflicts")).toHaveTextContent("Other channel");
    rerender(<ScopeEditor {...props} scope={create(ReleaseChannelScopeSchema, { siteIds: [2n] })} />);
    expect(screen.queryByTestId("scope-conflicts")).not.toBeInTheDocument();
    expect(onPreview).toHaveBeenLastCalledWith(null);
    await resolvePreview();
    const pendingSignal = preview.mock.calls[1][1] as AbortSignal;
    expect(pendingSignal.aborted).toBe(false);
    rerender(
      <ScopeEditor
        {...props}
        scope={create(ReleaseChannelScopeSchema, { siteIds: Array.from({ length: 101 }, (_, i) => BigInt(i + 1)) })}
      />,
    );
    expect(pendingSignal.aborted).toBe(true);
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Last valid preview: 1 site · covers 5 miners");
    expect(screen.getByTestId("scope-preview")).not.toHaveTextContent("Other channel");
    await act(async () => finishPending(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 999 })));
    await resolvePreview();
    expect(preview).toHaveBeenCalledTimes(2);
    expect(onPreview).toHaveBeenLastCalledWith(null);
    expect(screen.getByTestId("scope-preview")).not.toHaveTextContent("999");
    rerender(<ScopeEditor {...props} scope={create(ReleaseChannelScopeSchema, { siteIds: [3n] })} />);
    await resolvePreview();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("covers 6 miners");
    expect(screen.getByTestId("scope-preview")).not.toHaveTextContent("Last valid preview");
  });
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
    const onPreview = vi.fn();
    render(
      <ScopeEditor
        scope={scope}
        onChange={onChange}
        previewScope={previewScope}
        onPreview={onPreview}
        editingExistingChannel
      />,
    );
    await waitFor(() => expect(previewScope).toHaveBeenCalledWith(scope, expect.any(AbortSignal)));
    expect(onPreview).toHaveBeenLastCalledWith(create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 5 }));
    expect(screen.queryByTestId("scope-preview")).not.toBeInTheDocument();
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
    expect(ContainerSelectionModal).not.toHaveBeenCalled();
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

describe("release-channel scope containers", () => {
  const mixedScope = create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["rig-1", "ct-1", "rig-2"] });
  const grant = () => {
    for (const permission of ["site:read", "fleet:read", "miner:read"]) permissions.add(permission);
  };

  it.each(["site:read", "fleet:read", "miner:read"])("hides the container selector without %s", (missing) => {
    grant();
    permissions.delete(missing);
    render(<ScopeEditor scope={mixedScope} onChange={vi.fn()} previewScope={previewScope} />);
    expect(screen.queryByRole("button", { name: /^Containers / })).not.toBeInTheDocument();
    expect(useContainers).toHaveBeenLastCalledWith([], false, false);
  });

  it("holds the miner and container counts until the catalog can tell them apart", () => {
    grant();
    useContainers.mockReturnValue({ containers: [], error: undefined, loading: true });
    const props = { scope: mixedScope, onChange: vi.fn(), previewScope, editingExistingChannel: true };
    const { rerender } = render(<ScopeEditor {...props} />);
    expect(useContainers).toHaveBeenLastCalledWith([], false, true);
    for (const label of ["Miners", "Containers"]) {
      expect(screen.getByRole("button", { name: `${label} Loading…` })).toBeDisabled();
    }
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving selection…");
    expect(screen.getByTestId("scope-preview")).not.toHaveTextContent("miner");
    useContainers.mockReturnValue(catalog("ct-1"));
    rerender(<ScopeEditor {...props} />);
    expect(screen.getByRole("button", { name: "Miners 2 miners" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Containers 1 container" })).toBeEnabled();
    expect(screen.getByTestId("scope-preview")).toHaveTextContent("Resolving 2 miners, 1 container…");
  });

  it("offers both selectors at once for an empty scope while the catalog loads", () => {
    grant();
    useContainers.mockReturnValue({ containers: [], error: undefined, loading: true });
    render(<ScopeEditor scope={create(ReleaseChannelScopeSchema)} onChange={vi.fn()} previewScope={previewScope} />);
    expect(screen.getByRole("button", { name: "Miners Select" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Containers Select" }));
    const calls = vi.mocked(ContainerSelectionModal).mock.calls;
    expect(calls[calls.length - 1][0]).toMatchObject({ loading: true, selectedIdentifiers: [] });
  });

  it("counts directly selected containers apart from miners", async () => {
    grant();
    useContainers.mockReturnValue(catalog("ct-1", "ct-2"));
    render(<ScopeEditor scope={mixedScope} onChange={vi.fn()} previewScope={previewScope} />);
    expect(useContainers).toHaveBeenLastCalledWith([], false, true);
    expect(screen.getByRole("button", { name: "Miners 2 miners" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Containers 1 container" })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("scope-preview")).toHaveTextContent("2 miners, 1 container · covers 5 miners"),
    );
  });

  it("keeps the containers when the miner selection is saved", () => {
    grant();
    useContainers.mockReturnValue(catalog("ct-1"));
    const onChange = vi.fn();
    const { rerender } = render(<ScopeEditor scope={mixedScope} onChange={onChange} previewScope={previewScope} />);
    fireEvent.click(screen.getByRole("button", { name: /^Miners / }));
    const calls = vi.mocked(MinerSelectionModal).mock.calls;
    expect(calls[calls.length - 1][0]).toMatchObject({ selectedMinerIds: ["rig-1", "rig-2"] });
    // A failed poll empties the catalog. The open picker keeps the identifiers
    // it was given, and the container is still not its to replace.
    useContainers.mockReturnValue({ containers: [], error: "unavailable", loading: false });
    rerender(<ScopeEditor scope={mixedScope} onChange={onChange} previewScope={previewScope} />);
    expect(calls[calls.length - 1][0]).toMatchObject({ selectedMinerIds: ["rig-1", "rig-2"] });
    selections.deviceIdentifiers = ["rig-2", "rig-3"];
    fireEvent.click(screen.getByRole("button", { name: "Save miners" }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(
      create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["ct-1", "rig-2", "rig-3"] }),
    );
  });

  it("keeps the miners when the container selection is saved", () => {
    grant();
    useContainers.mockReturnValue(catalog("ct-1", "ct-2"));
    const onChange = vi.fn();
    render(<ScopeEditor scope={mixedScope} onChange={onChange} previewScope={previewScope} />);
    fireEvent.click(screen.getByRole("button", { name: /^Containers / }));
    const calls = vi.mocked(ContainerSelectionModal).mock.calls;
    expect(calls[calls.length - 1][0]).toMatchObject({
      selectedIdentifiers: ["rig-1", "ct-1", "rig-2"],
      loading: false,
      hasLoadError: false,
    });
    selections.containerIdentifiers = ["ct-2"];
    fireEvent.click(screen.getByRole("button", { name: "Save containers" }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith(
      create(ReleaseChannelScopeSchema, { deviceIdentifiers: ["rig-1", "rig-2", "ct-2"] }),
    );
  });
});
