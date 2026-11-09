import { act, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { useConfirmationRecovery } from "./confirmationAttempts";
import { WorkerNameReview } from "./WorkerNameReview";
import { ReviewWorkerNamesResponseSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { ConfigurationSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({ confirmReview: vi.fn(), authorizeWorkerNameReviews: vi.fn(), track: vi.fn() }));
vi.mock("./api", () => ({
  usePoolConfigurationClient: () => ({
    client: { confirmReview: mocks.confirmReview, authorizeWorkerNameReviews: mocks.authorizeWorkerNameReviews },
    accountId: "",
  }),
}));
vi.mock("./assignmentProgress", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./assignmentProgress")>()),
  trackPoolOperation: mocks.track,
}));
const review = create(ReviewWorkerNamesResponseSchema, {
  unchangedCount: 3,
  failedCount: 1,
  reviews: [
    {
      reviewId: "review-a",
      configurationId: "config-a",
      configurationName: "Production",
      changes: [{ deviceIdentifier: "miner-a", before: "old-a", after: "new-a" }],
    },
    {
      reviewId: "review-b",
      configurationId: "config-b",
      configurationName: "Backup",
      changes: [{ deviceIdentifier: "miner-b", before: "old-b", after: "new-b" }],
    },
  ],
});
const configuration = create(ConfigurationSchema, { configurationId: "config-a", name: "Production" });

beforeEach(() => {
  vi.clearAllMocks();
  useConfirmationRecovery.setState({ attempts: [] });
  mocks.confirmReview.mockReset().mockResolvedValue({ configuration, operationId: "operation" });
  mocks.track.mockResolvedValue(undefined);
  mocks.authorizeWorkerNameReviews.mockReset().mockResolvedValue({});
});

describe("worker name review", () => {
  it("shows the exact changes grouped by current configuration and cancels without confirming", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    render(<WorkerNameReview review={review} password="actor-secret" onDone={vi.fn()} onCancel={onCancel} />);
    const dialog = screen.getByRole("dialog", { name: "Review worker names" });
    const production = within(within(dialog).getByRole("heading", { name: "Production" }).closest("section")!);
    const backup = within(within(dialog).getByRole("heading", { name: "Backup" }).closest("section")!);
    await waitFor(() => expect(production.getByText("miner-a: old-a → new-a")).toBeVisible());
    expect(backup.getByText("miner-b: old-b → new-b")).toBeVisible();
    expect(screen.getByText(/1 worker names could not be generated/)).toBeVisible();
    expect(mocks.confirmReview).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(mocks.confirmReview).not.toHaveBeenCalled();
    expect(mocks.track).not.toHaveBeenCalled();
  });

  it("completes saved-name acceptance while hardware operations are still being tracked", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    mocks.track.mockReturnValue(new Promise(() => {}));
    render(<WorkerNameReview review={review} password="actor-secret" onDone={onDone} onCancel={vi.fn()} />);
    expect(mocks.confirmReview).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
    await waitFor(() => expect(onDone).toHaveBeenCalledWith({ updatedCount: 2, unchangedCount: 3, failedCount: 1 }));
    expect(mocks.confirmReview).toHaveBeenNthCalledWith(
      1,
      {
        accountId: "",
        reviewId: "review-a",
        password: "",
      },
      expect.objectContaining({ timeoutMs: 30_000 }),
    );
    expect(mocks.confirmReview).toHaveBeenNthCalledWith(
      2,
      {
        accountId: "",
        reviewId: "review-b",
        password: "",
      },
      expect.objectContaining({ timeoutMs: 30_000 }),
    );
    expect(mocks.track).toHaveBeenCalledTimes(2);
    expect(mocks.track).toHaveBeenCalledWith(
      expect.objectContaining({ confirmReview: mocks.confirmReview }),
      "",
      configuration,
      "operation",
      [],
      undefined,
    );
  });

  it("retries only unconfirmed configurations after a partial failure", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    mocks.confirmReview
      .mockResolvedValueOnce({ configuration, operationId: "operation-a" })
      .mockRejectedValueOnce(new Error("Confirmation unavailable"));
    render(<WorkerNameReview review={review} password="actor-secret" onDone={onDone} onCancel={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
    await waitFor(() => expect(screen.getByText(/may still be running/)).toBeVisible());
    expect(screen.getByRole("heading", { name: "Production — update submitted" })).toBeVisible();
    expect(onDone).not.toHaveBeenCalled();
    expect(mocks.track).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
    expect(mocks.confirmReview.mock.calls.map(([request]) => request.reviewId)).toEqual([
      "review-a",
      "review-b",
      "review-b",
    ]);
    expect(mocks.track).toHaveBeenCalledTimes(2);
  });

  it("closing a partial review leaves remaining changes unconfirmed and reports accepted work", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    const onCancel = vi.fn();
    mocks.confirmReview
      .mockResolvedValueOnce({ configuration, operationId: "operation-a" })
      .mockRejectedValueOnce(new Error("Confirmation unavailable"));
    render(<WorkerNameReview review={review} password="actor-secret" onDone={onDone} onCancel={onCancel} />);
    await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
    await screen.findByText(/may still be running/);
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(onDone).toHaveBeenCalledWith({ updatedCount: 1, unchangedCount: 3, failedCount: 1, unknownCount: 1 });
    expect(onCancel).not.toHaveBeenCalled();
    expect(mocks.confirmReview).toHaveBeenCalledTimes(2);
    expect(mocks.track).toHaveBeenCalledOnce();
  });
});

it.each(["session", "unmount"])("stops a delayed bulk confirmation after %s ends", async (boundary) => {
  let reply!: (saved: { configuration: typeof configuration; operationId: string }) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  const onDone = vi.fn();
  const onCancel = vi.fn();
  const view = render(<WorkerNameReview review={review} password="secret" onDone={onDone} onCancel={onCancel} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Save and update equipment" }));
  if (boundary === "unmount") view.unmount();
  else
    act(() =>
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
      })),
    );
  await act(async () => reply({ configuration, operationId: "accepted-before-close" }));
  expect(mocks.confirmReview).toHaveBeenCalledTimes(1);
  expect(mocks.track).not.toHaveBeenCalled();
  expect(onDone).not.toHaveBeenCalled();
  expect(onCancel).not.toHaveBeenCalled();
});

it("closing after the first lost reply reports unknown and not-submitted counts", async () => {
  mocks.confirmReview.mockRejectedValueOnce(new Error("Reply lost"));
  const onDone = vi.fn();
  const onCancel = vi.fn();
  render(<WorkerNameReview review={review} password="secret" onDone={onDone} onCancel={onCancel} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
  await screen.findByText(/may still be running/);
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(onCancel).not.toHaveBeenCalled();
  expect(onDone).toHaveBeenCalledWith({
    updatedCount: 0,
    unchangedCount: 3,
    failedCount: 1,
    unknownCount: 1,
    notSubmittedCount: 1,
  });
});

it("retries a lost first confirmation without submitting the untouched configuration", async () => {
  mocks.confirmReview.mockRejectedValueOnce(new Error("Reply lost"));
  const onDone = vi.fn();
  render(<WorkerNameReview review={review} password="secret" onDone={onDone} onCancel={vi.fn()} />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
  await screen.findByText(/may still be running/);
  expect(useConfirmationRecovery.getState().attempts.map((a) => a.reviewId)).toEqual(["review-a"]);
  await user.click(screen.getByRole("button", { name: "Retry confirmation" }));
  await waitFor(() =>
    expect(onDone).toHaveBeenCalledWith({ updatedCount: 1, unchangedCount: 3, failedCount: 1, notSubmittedCount: 1 }),
  );
  expect(mocks.confirmReview.mock.calls.map(([request]) => request.reviewId)).toEqual(["review-a", "review-a"]);
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
});

it("authorizes the exact worker-name batch once before confirming each group without its password", async () => {
  const user = userEvent.setup();
  const onDone = vi.fn();
  const batch = create(ReviewWorkerNamesResponseSchema, {
    reviews: Array.from({ length: 12 }, (_, i) => ({
      reviewId: `review-${i}`,
      configurationId: `config-${i}`,
      configurationName: `Config ${i}`,
      changes: [{ deviceIdentifier: `miner-${i}`, before: "old", after: "new" }],
    })),
  });
  render(<WorkerNameReview review={batch} password="secret" onDone={onDone} onCancel={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
  await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
  expect(mocks.authorizeWorkerNameReviews).toHaveBeenCalledExactlyOnceWith(
    { reviewIds: batch.reviews.map((item) => item.reviewId), password: "secret" },
    expect.objectContaining({ timeoutMs: 30_000 }),
  );
  expect(mocks.confirmReview).toHaveBeenCalledTimes(12);
  expect(mocks.confirmReview.mock.calls.every(([request]) => !request.password)).toBe(true);
  expect(mocks.authorizeWorkerNameReviews.mock.invocationCallOrder[0]).toBeLessThan(
    mocks.confirmReview.mock.invocationCallOrder[0],
  );
});

it("does not confirm any group when batch authorization fails", async () => {
  mocks.authorizeWorkerNameReviews.mockRejectedValueOnce(new Error("Password approval failed"));
  const user = userEvent.setup();
  render(<WorkerNameReview review={review} password="secret" onDone={vi.fn()} onCancel={vi.fn()} />);
  await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
  await screen.findByText("Password approval failed");
  expect(mocks.confirmReview).not.toHaveBeenCalled();
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
  await user.click(screen.getByRole("button", { name: "Save and update equipment" }));
  await waitFor(() => expect(mocks.confirmReview).toHaveBeenCalledTimes(2));
});

it.each(["unmount", "logout"])("does not submit after a late batch authorization following %s", async (end) => {
  let resolve!: (value: object) => void;
  mocks.authorizeWorkerNameReviews.mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const onDone = vi.fn();
  const view = render(<WorkerNameReview review={review} password="secret" onDone={onDone} onCancel={vi.fn()} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Save and update equipment" }));
  if (end === "unmount") view.unmount();
  else act(() => useFleetStore.getState().auth.logout());
  await act(async () => resolve({}));
  expect(mocks.confirmReview).not.toHaveBeenCalled();
  expect(onDone).not.toHaveBeenCalled();
  expect(useConfirmationRecovery.getState().attempts).toEqual([]);
});
