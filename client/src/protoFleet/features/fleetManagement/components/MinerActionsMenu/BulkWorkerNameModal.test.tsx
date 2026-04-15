import { type ComponentProps, type ReactNode } from "react";
import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  bulkRenameModes,
  bulkRenamePropertyIds,
  createDefaultBulkRenamePreferences,
  updateBulkRenameProperty,
} from "./bulkRenameDefinitions";
import BulkWorkerNameModal from "./BulkWorkerNameModal";
import { customPropertyTypes } from "./RenameOptionsModals/types";
import { SortConfigSchema, SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type MinerStateSnapshot,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const {
  mockBulkRenameDialogs,
  mockCompleteBatchOperation,
  mockFullScreenTwoPaneModal,
  mockHandleAuthErrors,
  mockListMinerStateSnapshots,
  mockPushToast,
  mockRemoveToast,
  mockStartBatchOperation,
  mockStreamCommandBatchUpdates,
  mockUpdateToast,
  mockUseBulkWorkerNamePreferences,
  mockUseSetBulkWorkerNamePreferences,
  mockUpdateWorkerNames,
} = vi.hoisted(() => ({
  mockBulkRenameDialogs: vi.fn(() => null),
  mockCompleteBatchOperation: vi.fn(),
  mockFullScreenTwoPaneModal: vi.fn(() => null),
  mockHandleAuthErrors: vi.fn(),
  mockListMinerStateSnapshots: vi.fn(),
  mockPushToast: vi.fn(),
  mockRemoveToast: vi.fn(),
  mockStartBatchOperation: vi.fn(),
  mockStreamCommandBatchUpdates: vi.fn(),
  mockUpdateToast: vi.fn(),
  mockUseBulkWorkerNamePreferences: vi.fn(),
  mockUseSetBulkWorkerNamePreferences: vi.fn(),
  mockUpdateWorkerNames: vi.fn(),
}));

const workerNamePreferences = updateBulkRenameProperty(
  createDefaultBulkRenamePreferences(bulkRenameModes.worker),
  bulkRenamePropertyIds.custom,
  (property) => ({
    ...property,
    enabled: true,
    options: {
      ...property.options,
      type: customPropertyTypes.stringAndCounter,
      prefix: "worker-",
      suffix: "",
      counterStart: 1,
      counterScale: 1,
    },
  }),
);

type MinerOverrides = Partial<Pick<MinerStateSnapshot, "manufacturer" | "model" | "name" | "workerName">>;

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: mockHandleAuthErrors,
  })),
  useBulkWorkerNamePreferences: mockUseBulkWorkerNamePreferences,
  useSetBulkWorkerNamePreferences: mockUseSetBulkWorkerNamePreferences,
}));

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    listMinerStateSnapshots: mockListMinerStateSnapshots,
  },
}));

vi.mock("@/protoFleet/api/useUpdateWorkerNames", () => ({
  default: vi.fn(() => ({
    updateWorkerNames: mockUpdateWorkerNames,
  })),
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: vi.fn(() => ({
    streamCommandBatchUpdates: mockStreamCommandBatchUpdates,
  })),
}));

vi.mock("@/protoFleet/features/fleetManagement/hooks/useBatchOperations", () => ({
  useBatchOperations: vi.fn(() => ({
    startBatchOperation: mockStartBatchOperation,
    completeBatchOperation: mockCompleteBatchOperation,
  })),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: vi.fn(() => ({
    isPhone: false,
    isTablet: false,
  })),
}));

vi.mock("@/protoFleet/components/FullScreenTwoPaneModal", () => ({
  default: mockFullScreenTwoPaneModal,
}));

vi.mock("./BulkRenamePropertyForm", () => ({
  default: () => <div data-testid="bulk-worker-name-form" />,
}));

vi.mock("./BulkRenamePreviewPanel", () => ({
  default: () => <div data-testid="bulk-worker-name-preview" />,
}));

vi.mock("./BulkRenameDialogs", () => ({
  default: mockBulkRenameDialogs,
}));

vi.mock("./BulkRenameOptionModals", () => ({
  default: () => null,
}));

vi.mock("@/shared/components/Callout", () => ({
  default: () => null,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: mockPushToast,
  removeToast: mockRemoveToast,
  updateToast: mockUpdateToast,
  STATUSES: {
    loading: "loading",
    success: "success",
    error: "error",
  },
}));

const makeMiner = (deviceIdentifier: string, name: string, workerName = "", overrides: MinerOverrides = {}) =>
  create(MinerStateSnapshotSchema, {
    deviceIdentifier,
    name,
    workerName,
    manufacturer: "Bitmain",
    model: "S19",
    macAddress: `${deviceIdentifier}-mac`,
    serialNumber: `${deviceIdentifier}-serial`,
    rackLabel: "",
    rackPosition: "",
    ...overrides,
  });

const renderModal = (props: Partial<ComponentProps<typeof BulkWorkerNameModal>> = {}) =>
  render(
    <BulkWorkerNameModal
      open
      selectedMinerIds={["miner-1", "miner-2", "miner-3"]}
      selectionMode="subset"
      originalSelectionMode="subset"
      totalCount={3}
      miners={{
        "miner-1": makeMiner("miner-1", "Miner 1"),
        "miner-2": makeMiner("miner-2", "Miner 2"),
        "miner-3": makeMiner("miner-3", "Miner 3"),
      }}
      minerIds={["miner-1", "miner-2", "miner-3"]}
      getWorkerNameCredentials={() => ({
        username: "testuser",
        password: "testpass",
      })}
      onDismiss={vi.fn()}
      {...props}
    />,
  );

const getLatestFullScreenModalProps = () => {
  const fullScreenModalCalls = mockFullScreenTwoPaneModal.mock.calls as unknown as Array<
    [
      {
        buttons: Array<{ onClick: () => void }>;
        primaryPane: ReactNode;
        secondaryPane: ReactNode;
      },
    ]
  >;
  const latestFullScreenModalProps = fullScreenModalCalls[fullScreenModalCalls.length - 1]?.[0];
  if (latestFullScreenModalProps === undefined) {
    throw new Error("FullScreenTwoPaneModal was not rendered with props");
  }
  return latestFullScreenModalProps;
};

const getLatestPreviewPanelProps = () => {
  const latestPreviewPanelProps = (
    getLatestFullScreenModalProps().secondaryPane as {
      props?: { previewRows?: Array<{ currentName: string; newName: string }> };
    }
  )?.props;
  if (latestPreviewPanelProps === undefined) {
    throw new Error("BulkRenamePreviewPanel was not rendered with props");
  }
  return latestPreviewPanelProps;
};

describe("BulkWorkerNameModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPushToast.mockReturnValueOnce(1).mockReturnValueOnce(2);
    mockUseBulkWorkerNamePreferences.mockReturnValue(workerNamePreferences);
    mockUseSetBulkWorkerNamePreferences.mockReturnValue(vi.fn());
  });

  it("waits for the batch result, tracks the batch, updates successful visible rows, and shows mixed-result toasts", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 3,
      unchangedCount: 0,
      failedCount: 0,
      batchIdentifier: "batch-1",
    });
    mockStreamCommandBatchUpdates.mockImplementation(async ({ onStreamData }) => {
      onStreamData({
        status: {
          commandBatchDeviceCount: {
            total: 3,
            success: 1,
            failure: 2,
            successDeviceIdentifiers: ["miner-2"],
            failureDeviceIdentifiers: ["miner-1", "miner-3"],
          },
        },
      });
    });

    const onDismiss = vi.fn();
    const onRefetchMiners = vi.fn();
    const onWorkerNameUpdated = vi.fn();

    renderModal({
      onDismiss,
      onRefetchMiners,
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    getLatestFullScreenModalProps().buttons[0]?.onClick();

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
      expect(mockStreamCommandBatchUpdates).toHaveBeenCalledTimes(1);
    });

    expect(mockStartBatchOperation).toHaveBeenCalledWith({
      batchIdentifier: "batch-1",
      action: "update-worker-names",
      deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
    });
    expect(mockCompleteBatchOperation).toHaveBeenCalledWith("batch-1");
    expect(mockPushToast).toHaveBeenNthCalledWith(1, {
      message: "Updating worker names",
      status: "loading",
      longRunning: true,
    });
    expect(mockUpdateToast).toHaveBeenCalledWith(1, {
      message: "Updated 1 miner",
      status: "success",
    });
    expect(mockPushToast).toHaveBeenNthCalledWith(2, {
      message: "Failed to update worker names for 2 miners",
      status: "error",
      longRunning: true,
    });
    expect(onWorkerNameUpdated).toHaveBeenCalledTimes(1);
    expect(onWorkerNameUpdated).toHaveBeenCalledWith("miner-2", "worker-2");
    expect(onRefetchMiners).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("returns to the miners table when the no-changes warning is confirmed", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 0,
      unchangedCount: 2,
      failedCount: 0,
    });

    const onDismiss = vi.fn();

    renderModal({
      selectedMinerIds: ["miner-1", "miner-2"],
      totalCount: 2,
      miners: {
        "miner-1": makeMiner("miner-1", "Miner 1", "worker-1"),
        "miner-2": makeMiner("miner-2", "Miner 2", "worker-2"),
      },
      minerIds: ["miner-1", "miner-2"],
      onDismiss,
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    getLatestFullScreenModalProps().buttons[0]?.onClick();

    await waitFor(() => {
      expect(
        (
          mockBulkRenameDialogs.mock.calls as unknown as Array<
            [{ showNoChangesWarning: boolean; onContinueNoChanges: () => void }]
          >
        )
          .map(([props]) => props)
          .find((props) => props.showNoChangesWarning),
      ).toBeDefined();
    });

    const latestDialogProps = (
      mockBulkRenameDialogs.mock.calls as unknown as Array<
        [{ showNoChangesWarning: boolean; onContinueNoChanges: () => void }]
      >
    )
      .map(([props]) => props)
      .find((props) => props.showNoChangesWarning);

    await act(async () => {
      latestDialogProps?.onContinueNoChanges();
    });

    expect(mockUpdateWorkerNames).not.toHaveBeenCalled();
    expect(mockPushToast).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("returns to the miners table without validation errors when no worker-name properties are enabled", async () => {
    mockUseBulkWorkerNamePreferences.mockReturnValue(createDefaultBulkRenamePreferences(bulkRenameModes.worker));

    const onDismiss = vi.fn();

    renderModal({
      selectedMinerIds: ["miner-1", "miner-2"],
      totalCount: 2,
      onDismiss,
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    getLatestFullScreenModalProps().buttons[0]?.onClick();

    const latestDialogProps = await waitFor(() => {
      const dialogProps = (
        mockBulkRenameDialogs.mock.calls as unknown as Array<
          [{ showNoChangesWarning: boolean; onContinueNoChanges: () => void }]
        >
      )
        .map(([props]) => props)
        .find((props) => props.showNoChangesWarning);

      expect(dialogProps).toBeDefined();
      return dialogProps;
    });

    await act(async () => {
      latestDialogProps?.onContinueNoChanges();
    });

    expect(mockUpdateWorkerNames).not.toHaveBeenCalled();
    expect(mockPushToast).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("updates visible worker names immediately when the request completes without a batch", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 3,
      unchangedCount: 0,
      failedCount: 0,
    });

    const onDismiss = vi.fn();
    const onRefetchMiners = vi.fn();
    const onWorkerNameUpdated = vi.fn();

    renderModal({
      onDismiss,
      onRefetchMiners,
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    const loadingToastId = mockPushToast.mock.results[0]?.value;

    expect(mockUpdateToast).toHaveBeenCalledWith(loadingToastId, {
      message: "Updated 3 miners",
      status: "success",
    });
    expect(onWorkerNameUpdated).toHaveBeenCalledTimes(3);
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(1, "miner-1", "worker-1");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(2, "miner-2", "worker-2");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(3, "miner-3", "worker-3");
    expect(onRefetchMiners).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("sorts local subset previews and visible updates with the default preview sort", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 2,
      unchangedCount: 0,
      failedCount: 0,
    });

    const onWorkerNameUpdated = vi.fn();

    renderModal({
      selectedMinerIds: ["miner-beta", "miner-alpha"],
      totalCount: 2,
      miners: {
        "miner-alpha": makeMiner("miner-alpha", "Alpha", "alpha-worker"),
        "miner-beta": makeMiner("miner-beta", "Beta", "beta-worker"),
      },
      minerIds: ["miner-beta", "miner-alpha"],
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(getLatestPreviewPanelProps().previewRows).toEqual([
        {
          currentName: "alpha-worker",
          newName: "worker-1",
        },
        {
          currentName: "beta-worker",
          newName: "worker-2",
        },
      ]);
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    const overwriteWarningProps = await waitFor(() => {
      const dialogProps = (
        mockBulkRenameDialogs.mock.calls as unknown as Array<
          [{ showOverwriteWarning: boolean; onContinueOverwriteWarning: () => void }]
        >
      )
        .map(([props]) => props)
        .find((props) => props.showOverwriteWarning);

      expect(dialogProps).toBeDefined();
      return dialogProps;
    });

    await act(async () => {
      overwriteWarningProps?.onContinueOverwriteWarning();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(1, "miner-alpha", "worker-1");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(2, "miner-beta", "worker-2");
  });

  it("sorts default name previews with the backend miner-name fallback", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 2,
      unchangedCount: 0,
      failedCount: 0,
    });

    const onWorkerNameUpdated = vi.fn();

    renderModal({
      selectedMinerIds: ["miner-z", "miner-a"],
      totalCount: 2,
      miners: {
        "miner-a": makeMiner("miner-a", "", "pool-a", {
          manufacturer: "Bitmain",
          model: "S19",
        }),
        "miner-z": makeMiner("miner-z", "", "pool-z", {
          manufacturer: "Avalon",
          model: "1246",
        }),
      },
      minerIds: ["miner-a", "miner-z"],
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(getLatestPreviewPanelProps().previewRows).toEqual([
        {
          currentName: "pool-z",
          newName: "worker-1",
        },
        {
          currentName: "pool-a",
          newName: "worker-2",
        },
      ]);
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    const overwriteWarningProps = await waitFor(() => {
      const dialogProps = (
        mockBulkRenameDialogs.mock.calls as unknown as Array<
          [{ showOverwriteWarning: boolean; onContinueOverwriteWarning: () => void }]
        >
      )
        .map(([props]) => props)
        .find((props) => props.showOverwriteWarning);

      expect(dialogProps).toBeDefined();
      return dialogProps;
    });

    await act(async () => {
      overwriteWarningProps?.onContinueOverwriteWarning();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(1, "miner-z", "worker-1");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(2, "miner-a", "worker-2");
  });

  it("sorts blank worker names after populated values in worker-name previews", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 3,
      unchangedCount: 0,
      failedCount: 0,
    });

    const onWorkerNameUpdated = vi.fn();

    renderModal({
      currentSort: create(SortConfigSchema, {
        field: SortField.WORKER_NAME,
        direction: SortDirection.ASC,
      }),
      selectedMinerIds: ["miner-blank", "miner-space", "miner-alpha"],
      totalCount: 3,
      miners: {
        "miner-alpha": makeMiner("miner-alpha", "Miner Alpha", "alpha"),
        "miner-blank": makeMiner("miner-blank", "Miner Blank", ""),
        "miner-space": makeMiner("miner-space", "Miner Space", "   "),
      },
      minerIds: ["miner-blank", "miner-space", "miner-alpha"],
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(getLatestPreviewPanelProps().previewRows).toEqual([
        {
          currentName: "alpha",
          newName: "worker-1",
        },
        {
          currentName: "",
          newName: "worker-2",
        },
        {
          currentName: "   ",
          newName: "worker-3",
        },
      ]);
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    const overwriteWarningProps = await waitFor(() => {
      const dialogProps = (
        mockBulkRenameDialogs.mock.calls as unknown as Array<
          [{ showOverwriteWarning: boolean; onContinueOverwriteWarning: () => void }]
        >
      )
        .map(([props]) => props)
        .find((props) => props.showOverwriteWarning);

      expect(dialogProps).toBeDefined();
      return dialogProps;
    });

    await act(async () => {
      overwriteWarningProps?.onContinueOverwriteWarning();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(1, "miner-alpha", "worker-1");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(2, "miner-blank", "worker-2");
    expect(onWorkerNameUpdated).toHaveBeenNthCalledWith(3, "miner-space", "worker-3");
  });

  it("uses the same default sort for the preview head and tail queries when no sort is provided", async () => {
    mockListMinerStateSnapshots.mockResolvedValue({
      miners: [makeMiner("miner-1", "Miner 1"), makeMiner("miner-2", "Miner 2"), makeMiner("miner-3", "Miner 3")],
    });

    renderModal({
      selectedMinerIds: ["miner-1"],
      selectionMode: "all",
      totalCount: 8,
      miners: {},
      minerIds: [],
    });

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    });

    const [headCall, tailCall] = mockListMinerStateSnapshots.mock.calls as unknown as Array<
      [{ sort: Array<{ field: SortField; direction: SortDirection }> }]
    >;

    expect(headCall?.[0].sort).toEqual([
      expect.objectContaining({
        field: SortField.NAME,
        direction: SortDirection.ASC,
      }),
    ]);
    expect(tailCall?.[0].sort).toEqual([
      expect.objectContaining({
        field: SortField.NAME,
        direction: SortDirection.DESC,
      }),
    ]);
  });

  it("submits worker-name updates with the preview sort when no current sort is provided", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 3,
      unchangedCount: 0,
      failedCount: 0,
    });

    renderModal();

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    const updateWorkerNamesCalls = mockUpdateWorkerNames.mock.calls as unknown as Array<
      [unknown, unknown, string, string, { field: SortField; direction: SortDirection }]
    >;

    expect(updateWorkerNamesCalls[0]?.[4]).toEqual(
      expect.objectContaining({
        field: SortField.NAME,
        direction: SortDirection.ASC,
      }),
    );
  });

  it("keeps the overwrite warning when a capability-filtered all-selection extends beyond loaded miners", async () => {
    renderModal({
      selectedMinerIds: ["miner-1", "miner-2", "miner-3", "miner-4"],
      selectionMode: "subset",
      originalSelectionMode: "all",
      totalCount: 4,
      miners: {
        "miner-1": makeMiner("miner-1", "Miner 1"),
      },
      minerIds: ["miner-1"],
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    getLatestFullScreenModalProps().buttons[0]?.onClick();

    await waitFor(() => {
      expect(
        (
          mockBulkRenameDialogs.mock.calls as unknown as Array<
            [{ showOverwriteWarning: boolean; onContinueOverwriteWarning: () => void }]
          >
        )
          .map(([props]) => props)
          .find((props) => props.showOverwriteWarning),
      ).toBeDefined();
    });

    expect(mockUpdateWorkerNames).not.toHaveBeenCalled();
  });

  it("skips optimistic visible updates when the capability-filtered target is not fully loaded locally", async () => {
    mockUpdateWorkerNames.mockResolvedValue({
      updatedCount: 4,
      unchangedCount: 0,
      failedCount: 0,
    });

    const onWorkerNameUpdated = vi.fn();

    renderModal({
      selectedMinerIds: ["miner-1", "miner-2", "miner-3", "miner-4"],
      selectionMode: "subset",
      originalSelectionMode: "all",
      totalCount: 4,
      miners: {
        "miner-1": makeMiner("miner-1", "Miner 1"),
        "miner-2": makeMiner("miner-2", "Miner 2"),
      },
      minerIds: ["miner-1", "miner-2"],
      onWorkerNameUpdated,
    });

    await waitFor(() => {
      expect(mockFullScreenTwoPaneModal).toHaveBeenCalled();
    });

    await act(async () => {
      getLatestFullScreenModalProps().buttons[0]?.onClick();
    });

    const latestDialogProps = await waitFor(() => {
      const dialogProps = (
        mockBulkRenameDialogs.mock.calls as unknown as Array<
          [{ showOverwriteWarning: boolean; onContinueOverwriteWarning: () => void }]
        >
      )
        .map(([props]) => props)
        .find((props) => props.showOverwriteWarning);

      expect(dialogProps).toBeDefined();
      return dialogProps;
    });

    await act(async () => {
      latestDialogProps?.onContinueOverwriteWarning();
    });

    await waitFor(() => {
      expect(mockUpdateWorkerNames).toHaveBeenCalledTimes(1);
    });

    expect(onWorkerNameUpdated).not.toHaveBeenCalled();
  });
});
