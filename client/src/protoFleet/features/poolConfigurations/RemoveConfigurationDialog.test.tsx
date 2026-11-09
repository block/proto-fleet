import { useState } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";
import { RemoveConfigurationDialog } from "./RemoveConfigurationDialog";
import {
  EquipmentAssignmentSchema,
  GetOperationResponseSchema,
  ReviewAssignmentResponseSchema,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
const api = vi.hoisted(() => ({
  reviewAssignment: vi.fn(),
  authorizeReviews: vi.fn(),
  confirmReview: vi.fn(),
  getOperation: vi.fn(),
  reviewRetry: vi.fn(),
}));
vi.mock("./api", () => ({ usePoolConfigurationClient: () => ({ client: api, accountId: "", readOnly: false }) }));
const assignment = create(EquipmentAssignmentSchema, {
  deviceId: 1n,
  configurationId: "config",
  deviceIdentifier: "miner",
});
const review = (id = "review", verificationState = "verified", clearSupported = true, operationId = "") =>
  create(ReviewAssignmentResponseSchema, { reviewId: id, removal: { verificationState, clearSupported, operationId } });
const operation = (operationId = "review", state = "completed", keep = false) => {
  const id = operationId === "review" ? 1n : BigInt(operationId.replace("review-", ""));
  const target = api.reviewAssignment.mock.calls.map(([r]) => r.removalTarget).find((t) => t.targetId === id) ?? {
    kind: id >= 10n ? "hashboard_installation" : "device",
    targetId: id,
  };
  return create(GetOperationResponseSchema, {
    operationId,
    kind: keep ? "unassign" : "clear",
    targets: [{ ...target, state }],
  });
};
beforeEach(() => {
  vi.resetAllMocks();
  api.reviewAssignment.mockImplementation(async ({ removalTarget }) =>
    review(removalTarget.targetId === 1n ? "review" : `review-${removalTarget.targetId}`),
  );
  api.authorizeReviews.mockResolvedValue({});
  api.confirmReview.mockImplementation(async ({ reviewId }) => ({ operationId: reviewId }));
  api.getOperation.mockImplementation(async ({ operationId }) => operation(operationId));
  api.reviewRetry.mockResolvedValue({ reviewId: "review" });
});
const show = (assignments = [assignment], onRemoved = vi.fn()) =>
  render(
    <RemoveConfigurationDialog
      assignments={assignments}
      password="password"
      onDismiss={vi.fn()}
      onRemoved={onRemoved}
    />,
  );
const boards = () =>
  [11n, 12n].map((installationId) => create(EquipmentAssignmentSchema, { ...assignment, installationId }));
const submit = async () => {
  const button = screen.getByRole("button", { name: "Clear pools and remove assignment" });
  await waitFor(() => expect(button).toBeEnabled());
  await userEvent.setup().click(button);
};
const tick = async (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
const fakeSubmit = async () => {
  await tick(300);
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Clear pools and remove assignment" }));
  });
};
it("checks on opening and defaults to clearing through shared confirmation and operation progress", async () => {
  show();
  await submit();
  expect(api.reviewAssignment).toHaveBeenCalledOnce();
  expect(screen.getByRole("checkbox")).not.toBeChecked();
  expect(api.confirmReview).toHaveBeenCalledWith(
    expect.objectContaining({ keepDeviceSettings: false, reviewId: "review" }),
    expect.anything(),
  );
  expect(api.getOperation).toHaveBeenCalledOnce();
  expect(await screen.findByText("Device pools cleared and Fleet assignment removed.")).toBeInTheDocument();
});
it("requires an explicit keep-settings choice when unverified and checks again on reopening", async () => {
  api.reviewAssignment.mockResolvedValue(review("review", "applied_unverified"));
  const view = show();
  await screen.findByText(/couldn’t verify/);
  expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeDisabled();
  await userEvent.setup().click(screen.getByRole("checkbox"));
  expect(screen.getByRole("button", { name: "Remove assignment only" })).toBeEnabled();
  expect(screen.queryByRole("button", { name: "Recheck verification" })).not.toBeInTheDocument();
  view.unmount();
  api.reviewAssignment.mockResolvedValue(review());
  show();
  await waitFor(() => expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeEnabled());
  expect(api.reviewAssignment).toHaveBeenCalledTimes(2);
});
it("blocks both choices when a write is outstanding", async () => {
  api.reviewAssignment.mockRejectedValue(new Error("Wait for the update to finish"));
  show();
  await screen.findByText("Wait for the update to finish");
  await userEvent.setup().click(screen.getByRole("checkbox"));
  expect(screen.getByRole("button", { name: "Remove assignment only" })).toBeDisabled();
});
it("warns that clearing removes mismatched device settings", async () => {
  api.reviewAssignment.mockResolvedValue(review("review", "different"));
  show();
  await waitFor(() => expect(screen.getByText(/current pools differ/)).toBeVisible());
});
it("retains the exact review and choice after a lost confirmation response", async () => {
  api.confirmReview.mockRejectedValueOnce(new Error("connection lost"));
  show();
  await submit();
  expect(await screen.findByText(/Fleet has not confirmed removal/)).toBeVisible();
  expect(screen.getByRole("checkbox")).toBeDisabled();
  await submit();
  expect(api.confirmReview).toHaveBeenCalledTimes(2);
  expect(api.confirmReview.mock.calls[1][0]).toEqual(api.confirmReview.mock.calls[0][0]);
  expect(api.authorizeReviews).toHaveBeenCalledOnce();
});
it("blocks resubmission after a definitive rejection and allows closing", async () => {
  api.confirmReview.mockRejectedValueOnce(new ConnectError("Assignment changed", Code.FailedPrecondition));
  show();
  await submit();
  expect(await screen.findByText(/Assignment changed/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
});
it("authorizes a mixed-configuration selection once and confirms each without repeating the password", async () => {
  show([
    assignment,
    create(EquipmentAssignmentSchema, { ...assignment, deviceId: 2n, configurationId: "other-config" }),
  ]);
  await submit();
  await waitFor(() => expect(api.confirmReview).toHaveBeenCalledTimes(2));
  expect(api.authorizeReviews).toHaveBeenCalledExactlyOnceWith(
    { accountId: "", reviewIds: ["review", "review-2"], password: "password" },
    expect.anything(),
  );
  for (const [request] of api.confirmReview.mock.calls) expect(request.password).toBe("");
});
it("does not submit changes when password approval fails", async () => {
  api.authorizeReviews.mockRejectedValueOnce(new Error("Password approval failed"));
  show();
  await submit();
  expect(await screen.findByText(/Password approval failed/)).toBeVisible();
  expect(api.confirmReview).not.toHaveBeenCalled();
  expect(screen.getByRole("checkbox")).toBeEnabled();
});
it("allows assignment-only removal for three unsupported hashboards", async () => {
  api.reviewAssignment.mockImplementation(async ({ removalTarget }) =>
    review(`review-${removalTarget.targetId}`, "verified", false),
  );
  api.getOperation.mockImplementation(async ({ operationId }) => operation(operationId, "completed", true));
  show([...boards(), create(EquipmentAssignmentSchema, { ...assignment, installationId: 13n })]);
  expect(await screen.findAllByText(/current software does not support verified pool clearing/)).toHaveLength(3);
  expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeDisabled();
  await userEvent.setup().click(screen.getByRole("checkbox"));
  expect(screen.queryByText(/current software does not support/)).not.toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button", { name: "Remove assignment only" }));
  await waitFor(() => expect(api.confirmReview).toHaveBeenCalledTimes(3));
  for (const [request] of api.confirmReview.mock.calls) expect(request.keepDeviceSettings).toBe(true);
  expect(await screen.findAllByText("Fleet assignment removed. Device pool settings were preserved.")).toHaveLength(3);
});
it("monitors queued work and continues boards without another click", async () => {
  vi.useFakeTimers();
  api.getOperation.mockResolvedValueOnce(operation("review-11", "pending"));
  const onRemoved = vi.fn();
  const view = show(boards(), onRemoved);
  try {
    await fakeSubmit();
    expect(api.confirmReview).toHaveBeenCalledOnce();
    expect(screen.getByText(/Waiting for the previous removal/)).toBeInTheDocument();
    expect(screen.getByText(/Clearing pools and verifying removal/)).toBeInTheDocument();
    await tick(3000);
    expect(api.confirmReview.mock.calls.map(([r]) => r.reviewId)).toEqual(["review-11", "review-12"]);
    expect(onRemoved).toHaveBeenCalledTimes(2);
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});
it("stops monitoring and later confirmations after unmount", async () => {
  vi.useFakeTimers();
  api.getOperation.mockImplementation(async ({ operationId }) => operation(operationId, "applying"));
  const view = show(boards());
  try {
    await fakeSubmit();
    view.unmount();
    await tick(3000);
    expect(api.confirmReview).toHaveBeenCalledOnce();
    expect(api.getOperation).toHaveBeenCalledOnce();
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});
it.each(["applying", "unavailable"])("leaves an actionable idle state when monitoring stops: %s", async (state) => {
  vi.useFakeTimers();
  api.getOperation.mockImplementation(async ({ operationId }) => operation(operationId, "applying"));
  if (state === "unavailable") api.getOperation.mockRejectedValue(new Error("connection lost"));
  const view = show(boards());
  try {
    await fakeSubmit();
    await tick(60_000);
    expect(api.confirmReview).toHaveBeenCalledOnce();
    expect(api.getOperation).toHaveBeenCalledTimes(state === "applying" ? 21 : 3);
    expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    expect(screen.getByText("Not started.")).toBeInTheDocument();
    expect(screen.queryByText(/Waiting for the previous removal/)).not.toBeInTheDocument();
    expect(view.container.querySelector(".animate-spin")).toBeNull();
    if (state === "applying") {
      expect(screen.getByText(/Select “Clear pools and remove assignment” to check/)).toBeInTheDocument();
      api.getOperation.mockImplementation(async ({ operationId }) => operation(operationId));
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "Clear pools and remove assignment" }));
      });
      expect(api.confirmReview.mock.calls.map(([r]) => r.reviewId)).toEqual(["review-11", "review-12"]);
    } else expect(screen.getByText(/Fleet has not confirmed removal/)).toBeInTheDocument();
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});
it.each([false, true])("identifies a single assigned miner in a mixed selection: %s", async (mixed) => {
  const selected = create(EquipmentAssignmentSchema, { ...assignment, deviceName: "Miner A" });
  show(mixed ? [selected, create(EquipmentAssignmentSchema, { deviceId: 2n, deviceName: "Miner B" })] : [selected]);
  expect(await screen.findByText("Miner A")).toBeInTheDocument();
  expect(screen.queryByText("Miner B")).not.toBeInTheDocument();
  expect(api.reviewAssignment).toHaveBeenCalledOnce();
  expect(api.reviewAssignment.mock.calls[0][0].removalTarget.targetId).toBe(1n);
});
it("reopens an accepted operation and checks progress without reconfirming", async () => {
  api.reviewAssignment.mockResolvedValue(review("review", "verified", true, "review"));
  api.getOperation.mockResolvedValueOnce(operation("review", "applying"));
  const view = show();
  expect(await screen.findByText(/Select “Clear pools and remove assignment” to check/)).toBeInTheDocument();
  expect(view.container.querySelector(".animate-spin")).toBeNull();
  expect(api.getOperation).toHaveBeenCalledOnce();
  await submit();
  expect(api.reviewRetry).toHaveBeenCalledWith(
    { accountId: "", operationId: "review", targets: [{ kind: "device", targetId: 1n }] },
    { timeoutMs: 35_000 },
  );
  expect(api.confirmReview).not.toHaveBeenCalled();
  expect(api.authorizeReviews).not.toHaveBeenCalled();
  expect(await screen.findByText("Device pools cleared and Fleet assignment removed.")).toBeInTheDocument();
});
it("uses shared retry to verify an uncertain result without another confirmation", async () => {
  api.reviewAssignment.mockResolvedValue(review("review", "verified", true, "review"));
  api.getOperation.mockResolvedValueOnce(operation("review", "uncertain"));
  show();
  await screen.findByText(/Fleet has not confirmed removal/);
  await submit();
  expect(api.reviewRetry).toHaveBeenCalledOnce();
  expect(api.confirmReview).not.toHaveBeenCalled();
});
it.each(["Close", "Escape"])("dismisses via %s during polling without submitting the next board", async (method) => {
  vi.useFakeTimers();
  let finishRead!: (result: ReturnType<typeof operation>) => void;
  api.getOperation.mockImplementation(
    () =>
      new Promise((resolve) => {
        finishRead = resolve;
      }),
  );
  const Harness = () => {
    const [open, setOpen] = useState(true);
    return open ? (
      <RemoveConfigurationDialog
        assignments={boards()}
        password="password"
        onDismiss={() => setOpen(false)}
        onRemoved={vi.fn()}
      />
    ) : null;
  };
  const view = render(<Harness />);
  try {
    await fakeSubmit();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    await act(async () => {
      if (method === "Close") fireEvent.click(screen.getByRole("button", { name: "Close" }));
      else fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await act(async () => {
      finishRead(operation("review-11"));
    });
    await tick(60_000);
    expect(api.confirmReview).toHaveBeenCalledOnce();
    expect(api.getOperation).toHaveBeenCalledOnce();
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});

it.each(["applying", "uncertain"])("directs %s with unknown delivery to administrator recovery", async (state) => {
  api.reviewAssignment.mockResolvedValue(review("review", "verified", true, "review"));
  const result = operation("review", state);
  result.targets[0].resultCode = "operator_recovery_required";
  api.getOperation.mockResolvedValue(result);
  show();
  expect(await screen.findByText(/Contact an administrator to recover/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeDisabled();
  expect(screen.queryByText(/Retry this action/)).not.toBeInTheDocument();
  expect(api.reviewRetry).not.toHaveBeenCalled();
});
it.each(["runtime_pools_remain", "clear_verification_unavailable"])(
  "shows actionable background verification for %s",
  async (code) => {
    api.reviewAssignment.mockResolvedValue(review("review", "verified", true, "review"));
    const result = operation("review", "uncertain");
    result.targets[0].resultCode = code;
    api.getOperation.mockResolvedValue(result);
    const view = show();
    expect(await screen.findByText(/Fleet will keep checking/)).toBeInTheDocument();
    expect(view.container.querySelector(".animate-spin")).toBeNull();
    expect(screen.getByRole("button", { name: "Clear pools and remove assignment" })).toBeEnabled();
  },
);
