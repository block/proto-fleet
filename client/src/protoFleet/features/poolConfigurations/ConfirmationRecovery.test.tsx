import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { markConfirmationUnknown, startConfirmationAttempt, useConfirmationRecovery } from "./confirmationAttempts";
import { ConfirmationRecovery } from "./ConfirmationRecovery";
import { FleetErrorDetailsSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import {
  ConfigurationSchema,
  ConfirmReviewErrorCode,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({
  confirmReview: vi.fn(),
  track: vi.fn(),
  toast: vi.fn(),
  removeToast: vi.fn(),
  accountId: "account-a",
  change: true,
}));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({ client: mocks, accountId: mocks.accountId }),
  usePoolConfigurationPermissions: () => ({ change: mocks.change }),
}));
vi.mock("./assignmentProgress", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./assignmentProgress")>()),
  trackPoolOperation: mocks.track,
}));
vi.mock("@/shared/features/toaster", () => ({
  pushToast: mocks.toast,
  removeToast: mocks.removeToast,
  STATUSES: { error: "error" },
}));
const saved = {
  configuration: create(ConfigurationSchema, { configurationId: "config-a", name: "Production" }),
  operationId: "operation-a",
};
const seed = () => {
  const { attempt } = startConfirmationAttempt("alice", "account-a", {
    reviewId: "review-a",
    configurationId: "config-a",
    name: "Production",
    deviceIdentifiers: ["miner-a"],
    kind: "worker_names",
  });
  markConfirmationUnknown(attempt);
};
beforeEach(() => {
  vi.clearAllMocks();
  mocks.accountId = "account-a";
  mocks.change = true;
  mocks.toast.mockReturnValue(123);
  mocks.confirmReview.mockResolvedValue(saved);
  useConfirmationRecovery.setState({ attempts: [] });
  useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "alice", isAuthenticated: true } }));
});

it("reopens an unresolved attempt, requires a fresh password, and tracks the original accepted operation", async () => {
  seed();
  render(<ConfirmationRecovery />);
  expect(mocks.confirmReview).not.toHaveBeenCalled();
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledOnce());
  act(() => mocks.toast.mock.calls[0][0].actions[0].onClick());
  const user = userEvent.setup();
  expect(screen.getByRole("button", { name: "Retry confirmation" })).toBeDisabled();
  await user.type(screen.getByLabelText("Your password"), "new-secret");
  await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
  await waitFor(() =>
    expect(mocks.track).toHaveBeenCalledWith(mocks, "account-a", saved.configuration, "operation-a", [], undefined),
  );
  expect(mocks.confirmReview).toHaveBeenCalledExactlyOnceWith(
    { accountId: "account-a", reviewId: "review-a", password: "new-secret" },
    expect.anything(),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  expect(sessionStorage.getItem("proto-pool-confirmation-recovery")).not.toContain("new-secret");
});

it("keeps recovery after closing without submitting and hides it from other users and accounts", async () => {
  seed();
  const view = render(<ConfirmationRecovery />);
  act(() => mocks.toast.mock.calls[0][0].actions[0].onClick());
  await userEvent.setup().click(screen.getByRole("button", { name: "Close" }));
  expect(mocks.confirmReview).not.toHaveBeenCalled();
  expect(useConfirmationRecovery.getState().attempts).toHaveLength(1);
  act(() => useFleetStore.getState().auth.logout());
  expect(mocks.removeToast).toHaveBeenCalledWith(123);
  act(() => useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "bob", isAuthenticated: true } })));
  mocks.toast.mockClear();
  view.rerender(<ConfirmationRecovery />);
  expect(mocks.toast).not.toHaveBeenCalled();
  act(() => useFleetStore.setState((state) => ({ auth: { ...state.auth, username: "alice" } })));
  expect(mocks.toast).toHaveBeenCalledOnce();
  mocks.accountId = "account-b";
  mocks.toast.mockClear();
  view.rerender(<ConfirmationRecovery />);
  expect(mocks.toast).not.toHaveBeenCalled();
  expect(mocks.confirmReview).not.toHaveBeenCalled();
});

it("ignores a delayed recovery response after logout", async () => {
  seed();
  let reply!: (value: typeof saved) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  render(<ConfirmationRecovery />);
  act(() => mocks.toast.mock.calls[0][0].actions[0].onClick());
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Your password"), "secret");
  await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
  act(() => useFleetStore.getState().auth.logout());
  await act(async () => reply(saved));
  expect(mocks.track).not.toHaveBeenCalled();
  expect(useConfirmationRecovery.getState().attempts[0].status).toBe("unknown");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("permanently clears a never-accepted expired review after retry, dismissal, and reload", async () => {
  seed();
  mocks.confirmReview.mockRejectedValueOnce(
    new ConnectError("Review expired", Code.FailedPrecondition, undefined, [
      {
        desc: FleetErrorDetailsSchema,
        value: { code: { case: "endpoint", value: ConfirmReviewErrorCode.EXPIRED_UNACCEPTED } },
      },
    ]),
  );
  const view = render(<ConfirmationRecovery />);
  act(() => mocks.toast.mock.calls[0][0].actions[0].onClick());
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Your password"), "secret");
  await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
  expect(await screen.findByRole("dialog", { name: "Update not submitted" })).toHaveTextContent(
    "expired without being accepted",
  );
  expect(screen.queryByRole("button", { name: "Retry confirmation" })).not.toBeInTheDocument();
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  expect(mocks.removeToast).toHaveBeenCalledWith(123);
  expect(mocks.track).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  view.unmount();
  await useConfirmationRecovery.persist.rehydrate();
  mocks.toast.mockClear();
  render(<ConfirmationRecovery />);
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  expect(mocks.toast).not.toHaveBeenCalled();
  expect(mocks.confirmReview).toHaveBeenCalledOnce();
});
