import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { useConfirmationRecovery } from "./confirmationAttempts";
import { useReviewedConfirmation } from "./useReviewedConfirmation";
import { FleetErrorDetailsSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import {
  ConfigurationSchema,
  ConfirmReviewErrorCode,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({ confirmReview: vi.fn(), signal: undefined as AbortSignal | undefined }));
vi.mock("./api", () => ({ usePoolConfigurationClient: () => ({ client: mocks, accountId: "account-a" }) }));
vi.mock("@/protoFleet/features/hostedAccounts/AccountScope", () => ({
  useAccountScope: () => (mocks.signal ? { signal: mocks.signal } : null),
}));
const details = {
  reviewId: "review-a",
  configurationId: "config-a",
  name: "Production",
  deviceIdentifiers: ["miner-a"],
  kind: "worker_names" as const,
};
const saved = {
  configuration: create(ConfigurationSchema, { configurationId: "config-a", name: "Production" }),
  operationId: "operation-a",
};
beforeEach(() => {
  vi.clearAllMocks();
  mocks.signal = undefined;
  useConfirmationRecovery.setState({ attempts: [] });
  useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "alice", isAuthenticated: true } }));
});

it("persists only recovery metadata before sending and replays the same ID after a lost reply", async () => {
  mocks.confirmReview
    .mockRejectedValueOnce(new ConnectError("Reply lost", Code.Unavailable))
    .mockResolvedValueOnce(saved);
  const first = renderHook(() => useReviewedConfirmation());
  await act(async () => {
    await expect(first.result.current.confirm(details, "do-not-store-this-password")).rejects.toThrow(
      /may still be running/,
    );
  });
  const stored = sessionStorage.getItem("proto-pool-confirmation-recovery")!;
  expect(stored).not.toContain("do-not-store-this-password");
  expect(JSON.parse(stored).state.attempts).toEqual([
    expect.objectContaining({ username: "alice", accountId: "account-a", reviewId: "review-a", status: "unknown" }),
  ]);
  first.unmount();
  // Simulate rehydrating the same browser tab. No automatic RPC is sent.
  await useConfirmationRecovery.persist.rehydrate();
  const recovery = renderHook(() => useReviewedConfirmation());
  expect(mocks.confirmReview).toHaveBeenCalledTimes(1);
  await act(async () => {
    await expect(recovery.result.current.confirm(details, "new-password")).resolves.toEqual(saved);
  });
  expect(mocks.confirmReview.mock.calls.map(([request]) => request.reviewId)).toEqual(["review-a", "review-a"]);
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
});

it("retains an in-flight attempt after unmount and stores it as unknown for reload", async () => {
  let reply!: (value: typeof saved) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  const view = renderHook(() => useReviewedConfirmation());
  const pending = view.result.current.confirm(details, "secret");
  expect(JSON.parse(sessionStorage.getItem("proto-pool-confirmation-recovery")!).state.attempts[0].status).toBe(
    "unknown",
  );
  expect(useConfirmationRecovery.getState().attempts[0].status).toBe("submitting");
  view.unmount();
  expect(useConfirmationRecovery.getState().attempts[0].status).toBe("unknown");
  reply(saved);
  await expect(pending).resolves.toBeUndefined();
  expect(useConfirmationRecovery.getState().attempts[0].reviewId).toBe("review-a");
});

it("does not let an old session response erase a newer recovery attempt", async () => {
  let replyOld!: (value: typeof saved) => void;
  let replyNew!: (value: typeof saved) => void;
  mocks.confirmReview
    .mockReturnValueOnce(
      new Promise((resolve) => {
        replyOld = resolve;
      }),
    )
    .mockReturnValueOnce(
      new Promise((resolve) => {
        replyNew = resolve;
      }),
    );
  const old = renderHook(() => useReviewedConfirmation());
  const pendingOld = old.result.current.confirm(details, "secret");
  act(() => useFleetStore.getState().auth.logout());
  act(() => useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "alice", isAuthenticated: true } })));
  const next = renderHook(() => useReviewedConfirmation());
  const pendingNew = next.result.current.confirm(details, "new-secret");
  const requestId = useConfirmationRecovery.getState().attempts[0].requestId;
  await act(async () => {
    replyOld(saved);
    await pendingOld;
  });
  expect(useConfirmationRecovery.getState().attempts[0]).toMatchObject({ requestId, status: "submitting" });
  await act(async () => {
    replyNew(saved);
    await pendingNew;
  });
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
});

it("distinguishes definitive first rejection from a rejected retry of an unknown attempt", async () => {
  mocks.confirmReview
    .mockRejectedValueOnce(new ConnectError("Bad password", Code.PermissionDenied))
    .mockRejectedValueOnce(new ConnectError("Reply lost", Code.Unavailable))
    .mockRejectedValueOnce(new ConnectError("Permission removed", Code.PermissionDenied));
  const view = renderHook(() => useReviewedConfirmation());
  await act(async () => {
    await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: false });
  });
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  await act(async () => {
    await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
  });
  await act(async () => {
    await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
  });
  expect(useConfirmationRecovery.getState().attempts).toHaveLength(1);
});

it("stops an account confirmation as soon as its scope is aborted", async () => {
  const scope = new AbortController();
  mocks.signal = scope.signal;
  let reply!: (value: typeof saved) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  const view = renderHook(() => useReviewedConfirmation());
  const pending = view.result.current.confirm(details, "secret");
  scope.abort();
  expect(view.result.current.current()).toBe(false);
  expect(useConfirmationRecovery.getState().attempts[0].status).toBe("unknown");
  reply(saved);
  await expect(pending).resolves.toBeUndefined();
});

it("does not submit a second concurrent confirmation of the same review", async () => {
  let reply!: (value: typeof saved) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  const first = renderHook(() => useReviewedConfirmation());
  const second = renderHook(() => useReviewedConfirmation());
  const pending = first.result.current.confirm(details, "secret");
  await expect(second.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
  expect(mocks.confirmReview).toHaveBeenCalledTimes(1);
  reply(saved);
  await pending;
});

it("clears an unknown attempt only after authoritative expiry without acceptance", async () => {
  const expired = new ConnectError(
    "This review expired without being accepted. Review your changes again.",
    Code.FailedPrecondition,
    undefined,
    [
      {
        desc: FleetErrorDetailsSchema,
        value: { code: { case: "endpoint", value: ConfirmReviewErrorCode.EXPIRED_UNACCEPTED } },
      },
    ],
  );
  mocks.confirmReview
    .mockRejectedValueOnce(new ConnectError("Reply lost", Code.Unavailable))
    .mockRejectedValueOnce(expired);
  const view = renderHook(() => useReviewedConfirmation());
  await act(async () => {
    await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
  });
  await act(async () => {
    await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({
      uncertain: false,
      expiredUnaccepted: true,
    });
  });
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  await useConfirmationRecovery.persist.rehydrate();
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
});

it.each([Code.FailedPrecondition, Code.NotFound, Code.PermissionDenied, Code.Unavailable])(
  "keeps unknown recovery for a generic %s rejection",
  async (code) => {
    mocks.confirmReview
      .mockRejectedValueOnce(new ConnectError("Reply lost", Code.Unavailable))
      .mockRejectedValueOnce(new ConnectError("Review unavailable", code));
    const view = renderHook(() => useReviewedConfirmation());
    await act(async () => {
      await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
    });
    await act(async () => {
      await expect(view.result.current.confirm(details, "secret")).rejects.toMatchObject({ uncertain: true });
    });
    expect(useConfirmationRecovery.getState().attempts).toHaveLength(1);
  },
);
