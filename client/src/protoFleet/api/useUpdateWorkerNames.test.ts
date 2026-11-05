import type { ComponentProps, ReactElement } from "react";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { fleetManagementClient } from "./clients";
import useUpdateWorkerNames from "./useUpdateWorkerNames";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import { SortConfigSchema, SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  DeviceSelectorSchema,
  MinerNameConfigSchema,
  NamePropertySchema,
  ReviewWorkerNamesResponseSchema,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  WorkerNameReview,
  type WorkerNameUpdateResult,
} from "@/protoFleet/features/poolConfigurations/WorkerNameReview";
import { useFleetStore } from "@/protoFleet/store";

vi.mock("./clients", () => ({ fleetManagementClient: { reviewWorkerNames: vi.fn() } }));
vi.mock("@/protoFleet/features/poolConfigurations/WorkerNameReview", () => ({ WorkerNameReview: vi.fn(() => null) }));
const mockHandleAuthErrors = vi.fn();
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  useAuthErrors: vi.fn(() => ({ handleAuthErrors: mockHandleAuthErrors })),
}));

const deviceSelector = create(DeviceSelectorSchema, {
  selectionType: {
    case: "includeDevices",
    value: create(DeviceIdentifierListSchema, { deviceIdentifiers: ["miner-1"] }),
  },
});
const nameConfig = create(MinerNameConfigSchema, {
  properties: [
    create(NamePropertySchema, {
      kind: { case: "stringValue", value: create(StringPropertySchema, { value: "worker-new" }) },
    }),
  ],
});
const review = create(ReviewWorkerNamesResponseSchema, {
  unchangedCount: 2,
  reviews: [
    {
      reviewId: "review-1",
      configurationId: "config-1",
      configurationName: "Production",
      changes: [{ deviceIdentifier: "miner-1", before: "old", after: "worker-new" }],
    },
  ],
});
const props = (element: ReactElement | null) =>
  (element as ReactElement<ComponentProps<typeof WorkerNameReview>>).props;

beforeEach(() => {
  vi.clearAllMocks();
});

describe("useUpdateWorkerNames", () => {
  it("requests a review without credentials, then waits for the visible review to finish", async () => {
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockResolvedValue(review);
    const sort = create(SortConfigSchema, { field: SortField.NAME, direction: SortDirection.ASC });
    const { result } = renderHook(() => useUpdateWorkerNames());
    let finished!: Promise<WorkerNameUpdateResult>;
    let settled = false;
    await act(async () => {
      finished = result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass", sort);
      void finished.then(() => {
        settled = true;
      });
    });
    expect(fleetManagementClient.reviewWorkerNames).toHaveBeenCalledWith(
      expect.objectContaining({ deviceSelector, nameConfig, sort: [sort] }),
    );
    const request = vi.mocked(fleetManagementClient.reviewWorkerNames).mock.calls[0][0];
    expect(request).not.toHaveProperty("userUsername");
    expect(request).not.toHaveProperty("userPassword");
    expect(settled).toBe(false);
    expect(result.current.workerNameReview?.type).toBe(WorkerNameReview);
    expect(props(result.current.workerNameReview).review).toBe(review);
    expect(props(result.current.workerNameReview).password).toBe("fleet-pass");
    const completed = { updatedCount: 1, unchangedCount: 2, failedCount: 0 };
    await act(async () => {
      props(result.current.workerNameReview).onDone(completed);
      await finished;
    });
    await expect(finished).resolves.toEqual(completed);
    expect(result.current.workerNameReview).toBeNull();
  });

  it("returns unchanged counts immediately when no canonical review is needed", async () => {
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockResolvedValue(
      create(ReviewWorkerNamesResponseSchema, { unchangedCount: 1, failedCount: 2 }),
    );
    const { result } = renderHook(() => useUpdateWorkerNames());
    await expect(
      result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass"),
    ).resolves.toEqual({ updatedCount: 0, unchangedCount: 1, failedCount: 2 });
    expect(fleetManagementClient.reviewWorkerNames).toHaveBeenCalledWith(expect.objectContaining({ sort: [] }));
    expect(result.current.workerNameReview).toBeNull();
  });

  it("rejects cancellation and clears the pending review", async () => {
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockResolvedValue(review);
    const { result } = renderHook(() => useUpdateWorkerNames());
    let finished!: Promise<WorkerNameUpdateResult>;
    await act(async () => {
      finished = result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass");
    });
    const rejected = expect(finished).rejects.toThrow("Worker name update canceled.");
    await act(async () => {
      props(result.current.workerNameReview).onCancel();
      await rejected;
    });
    expect(result.current.workerNameReview).toBeNull();
  });

  it("handles review auth errors and rethrows the original error", async () => {
    const error = new Error("request failed");
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockRejectedValue(error);
    const { result } = renderHook(() => useUpdateWorkerNames());
    await expect(
      result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass"),
    ).rejects.toThrow(error);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error });
  });
  it("closes an open review when the login session changes", async () => {
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockResolvedValue(review);
    const { result } = renderHook(() => useUpdateWorkerNames());
    let finished!: Promise<WorkerNameUpdateResult>;
    await act(async () => {
      finished = result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass");
    });
    const rejected = expect(finished).rejects.toThrow("Worker name review closed.");
    await act(async () => {
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
      }));
      await rejected;
    });
    expect(result.current.workerNameReview).toBeNull();
  });
  it("rejects a late review response after the caller unmounts", async () => {
    let respond!: (value: typeof review) => void;
    vi.mocked(fleetManagementClient.reviewWorkerNames).mockReturnValue(
      new Promise((resolve) => {
        respond = resolve;
      }),
    );
    const { result, unmount } = renderHook(() => useUpdateWorkerNames());
    const finished = result.current.updateWorkerNames(deviceSelector, nameConfig, "fleet-user", "fleet-pass");
    const rejected = expect(finished).rejects.toThrow("Worker name review closed.");
    unmount();
    await act(async () => {
      respond(review);
      await rejected;
    });
  });
});

it("does not let an old review callback settle or clear a replacement session's review", async () => {
  vi.mocked(fleetManagementClient.reviewWorkerNames).mockResolvedValue(review);
  const { result } = renderHook(() => useUpdateWorkerNames());
  let old!: Promise<WorkerNameUpdateResult>;
  await act(async () => {
    old = result.current.updateWorkerNames(deviceSelector, nameConfig, "alice", "old-password");
  });
  const oldCallbacks = props(result.current.workerNameReview);
  const canceled = expect(old).rejects.toThrow("Worker name review closed.");
  await act(async () => {
    useFleetStore.getState().auth.logout();
    await canceled;
  });
  let next!: Promise<WorkerNameUpdateResult>;
  await act(async () => {
    next = result.current.updateWorkerNames(deviceSelector, nameConfig, "alice", "new-password");
  });
  const currentReview = result.current.workerNameReview;
  const completed = { updatedCount: 1, unchangedCount: 0, failedCount: 0 };
  act(() => {
    oldCallbacks.onDone(completed);
    oldCallbacks.onCancel();
  });
  expect(result.current.workerNameReview).toBe(currentReview);
  await act(async () => {
    props(currentReview).onDone(completed);
    await next;
  });
  expect(result.current.workerNameReview).toBeNull();
});
