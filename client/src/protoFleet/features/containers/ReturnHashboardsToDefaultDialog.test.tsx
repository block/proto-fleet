import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import ReturnHashboardsToDefaultDialog from "./ReturnHashboardsToDefaultDialog";
import {
  DefaultReturnReviewSchema,
  GetStateResponseSchema,
  ReviewReturnToDefaultResponseSchema,
} from "@/protoFleet/api/generated/mininggroups/v1/mininggroups_pb";

const mocks = vi.hoisted(() => ({
  reviewReturnToDefault: vi.fn(),
  returnToDefault: vi.fn(),
  getState: vi.fn(),
  refresh: vi.fn(),
  permissions: ["miner:update_pools", "miner:start_mining", "miner:stop_mining"],
}));
vi.mock("@/protoFleet/api/clients", () => ({ miningGroupsClient: mocks }));
vi.mock("@/protoFleet/store", () => ({
  useFleetStore: { getState: () => ({ auth: { sessionGeneration: 1 } }) },
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/protoFleet/features/poolConfigurations/assignmentProgress", () => ({
  refreshPoolAssignments: mocks.refresh,
}));

const review = () =>
  create(DefaultReturnReviewSchema, {
    deviceIdentifier: "ct1",
    boards: [{ hashboardId: 9n, macAddress: "02:00:00:00:00:09", name: "Hashboard CT1-0009" }],
    reviewToken: "a".repeat(64),
    defaultGroup: {
      id: "default",
      mining: true,
      pools: [{ id: 1, url: "stratum+tcp://host.example:3333", username: "host.worker", enabled: true }],
    },
  });
beforeEach(() => {
  vi.resetAllMocks();
  mocks.permissions = ["miner:update_pools", "miner:start_mining", "miner:stop_mining"];
  mocks.reviewReturnToDefault.mockResolvedValue(
    create(ReviewReturnToDefaultResponseSchema, { containers: [review()] }),
  );
  mocks.returnToDefault.mockResolvedValue({ operationId: "return-9" });
  mocks.getState.mockResolvedValue(create(GetStateResponseSchema, { operationId: "return-9", status: "complete" }));
});
afterEach(() => vi.restoreAllMocks());

it("shows Default settings and moves only the reviewed boards after confirmation", async () => {
  const onComplete = vi.fn();
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={onComplete} />);
  expect(await screen.findByText("Mining: Running")).toBeVisible();
  expect(screen.getByText(/host.worker/)).toBeVisible();
  expect(screen.queryByText(/Hashboards recalled/)).not.toBeInTheDocument();
  expect(screen.getByRole("list", { name: "Selected hashboards" })).toHaveTextContent("Hashboard CT1-0009");
  expect(screen.queryByText("1 hashboard")).not.toBeInTheDocument();
  expect(mocks.returnToDefault).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  await waitFor(() =>
    expect(mocks.returnToDefault).toHaveBeenCalledWith({
      deviceIdentifier: "ct1",
      hashboardIds: [9n],
      reviewToken: "a".repeat(64),
      requestId: expect.any(String),
      acknowledgeMiningChange: true,
    }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Done" }));
  expect(onComplete).toHaveBeenCalledOnce();
  expect(mocks.refresh).toHaveBeenCalledOnce();
});

it("shows preparation steps after recall instead of a disabled move confirmation", async () => {
  const blocked = review();
  blocked.blockedReason = "Wait for pool cleanup. The boards remain recalled.";
  blocked.reviewToken = "";
  mocks.reviewReturnToDefault.mockResolvedValue(create(ReviewReturnToDefaultResponseSchema, { containers: [blocked] }));
  const onClose = vi.fn();
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={onClose} onComplete={vi.fn()} />);
  expect(await screen.findByText(blocked.blockedReason)).toBeVisible();
  expect(screen.getByRole("dialog", { name: "Preparing hashboards" })).toBeVisible();
  expect(screen.queryByText(/Hashboards recalled/)).not.toBeInTheDocument();
  expect(screen.getByText("Finish pending changes")).toBeVisible();
  expect(screen.getByText("Check hashboards and default group settings")).toBeVisible();
  expect(screen.getByRole("button", { name: "Move hashboards" })).toBeDisabled();
  expect(screen.queryByText(/host.worker/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(mocks.returnToDefault).not.toHaveBeenCalled();
});

it("opens the confirmation with its action enabled only after all pending work finishes", async () => {
  let poll!: () => void;
  const schedule = globalThis.setInterval;
  vi.spyOn(window, "setInterval").mockImplementation((callback, timeout, ...args) => {
    if (timeout === 4000) poll = callback as () => void;
    return schedule(callback, timeout, ...args);
  });
  let finish!: (response: ReturnType<typeof create<typeof ReviewReturnToDefaultResponseSchema>>) => void;
  mocks.reviewReturnToDefault.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  expect(await screen.findByRole("dialog", { name: "Preparing hashboards" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Move hashboards" })).toBeDisabled();
  const blocked = review();
  blocked.reviewToken = "";
  blocked.blockedReason = "Wait for the current mining-group change or recovery to finish.";
  await act(async () => finish(create(ReviewReturnToDefaultResponseSchema, { containers: [blocked] })));
  expect(await screen.findByText(blocked.blockedReason)).toBeVisible();
  expect(screen.queryByRole("dialog", { name: "Move hashboards to default group?" })).not.toBeInTheDocument();
  act(() => poll());
  await waitFor(() => expect(screen.getByRole("button", { name: "Move hashboards" })).toBeEnabled());
  expect(screen.queryByRole("dialog", { name: "Preparing hashboards" })).not.toBeInTheDocument();
  expect(screen.queryByText(blocked.blockedReason)).not.toBeInTheDocument();
  expect(screen.getByText("Hashboard CT1-0009")).toBeVisible();
  expect(mocks.returnToDefault).not.toHaveBeenCalled();
});

it("keeps a failed preparation out of the confirmation and allows checking again", async () => {
  mocks.reviewReturnToDefault.mockRejectedValueOnce(new ConnectError("Controller offline", Code.Unavailable));
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Controller offline");
  expect(screen.getByRole("button", { name: "Move hashboards" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Move hashboards" })).toBeEnabled());
  expect(mocks.returnToDefault).not.toHaveBeenCalled();
});

it("retains the exact request and selection after a lost submission reply", async () => {
  mocks.returnToDefault.mockRejectedValueOnce(new ConnectError("Lost reply", Code.Unavailable));
  let finish!: (response: ReturnType<typeof create<typeof GetStateResponseSchema>>) => void;
  mocks.getState.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  await screen.findByText("Mining: Running");
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  await screen.findByRole("alert");
  expect(screen.queryByText("Moving hashboards…")).not.toBeInTheDocument();
  expect(screen.getByText("Mining: Running")).toBeVisible();
  expect(screen.getByText(/host.worker/)).toBeVisible();
  expect(screen.getByText(/If you move these hashboards/)).toBeVisible();
  expect(screen.getByRole("list", { name: "Selected hashboards" })).toHaveTextContent("Hashboard CT1-0009");
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  expect(await screen.findByText("Moving hashboards…")).toBeVisible();
  expect(screen.queryByText("Mining: Running")).not.toBeInTheDocument();
  expect(screen.queryByText(/host.worker/)).not.toBeInTheDocument();
  expect(screen.queryByText(/If you move these hashboards/)).not.toBeInTheDocument();
  await act(async () => finish(create(GetStateResponseSchema, { operationId: "return-9", status: "complete" })));
  await screen.findByRole("button", { name: "Done" });
  expect(mocks.returnToDefault.mock.calls[1][0]).toEqual(mocks.returnToDefault.mock.calls[0][0]);
  expect(mocks.reviewReturnToDefault).toHaveBeenCalledOnce();
});

it("keeps the confirmed review when a pre-submit poll finishes after a lost reply", async () => {
  let poll!: () => void;
  const schedule = globalThis.setInterval;
  vi.spyOn(window, "setInterval").mockImplementation((callback, timeout, ...args) => {
    if (timeout === 4000) poll = callback as () => void;
    return schedule(callback, timeout, ...args);
  });
  let finish!: (response: ReturnType<typeof create<typeof ReviewReturnToDefaultResponseSchema>>) => void;
  mocks.reviewReturnToDefault
    .mockImplementationOnce(async () => create(ReviewReturnToDefaultResponseSchema, { containers: [review()] }))
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
  mocks.returnToDefault.mockRejectedValueOnce(new ConnectError("Lost reply", Code.Unavailable));
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  await screen.findByText("Mining: Running");
  act(() => poll());
  await waitFor(() => expect(mocks.reviewReturnToDefault).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  await screen.findByRole("alert");
  const stale = review();
  stale.reviewToken = "";
  stale.blockedReason = "Another change is pending";
  await act(async () => finish(create(ReviewReturnToDefaultResponseSchema, { containers: [stale] })));
  expect(screen.getByRole("button", { name: "Move hashboards" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  await screen.findByRole("button", { name: "Done" });
  expect(mocks.returnToDefault.mock.calls[1][0]).toEqual(mocks.returnToDefault.mock.calls[0][0]);
});

it("requires a new confirmation after changed settings reject the request", async () => {
  mocks.returnToDefault.mockRejectedValueOnce(new ConnectError("Default settings changed", Code.FailedPrecondition));
  render(<ReturnHashboardsToDefaultDialog deviceIdentifier="ct1" onClose={vi.fn()} onComplete={vi.fn()} />);
  await screen.findByText("Mining: Running");
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  fireEvent.click(await screen.findByRole("button", { name: "Review hashboards" }));
  await waitFor(() => expect(mocks.reviewReturnToDefault).toHaveBeenCalledTimes(2));
  expect(mocks.returnToDefault).toHaveBeenCalledOnce();
  expect(await screen.findByRole("button", { name: "Move hashboards" })).toBeEnabled();
});

it("waits for the exact operation and reports terminal failure", async () => {
  mocks.getState.mockResolvedValue(
    create(GetStateResponseSchema, {
      operationId: "return-9",
      status: "failed",
      message: "Move failed. Boards remain recalled.",
    }),
  );
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  await screen.findByText("Mining: Running");
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  expect(await screen.findByText("Move failed. Boards remain recalled.")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Review hashboards" })).toBeEnabled();
});

it("does not submit another controller after the initiating dialog closes", async () => {
  const second = create(DefaultReturnReviewSchema, { ...review(), deviceIdentifier: "ct1-other" });
  mocks.reviewReturnToDefault.mockResolvedValue(
    create(ReviewReturnToDefaultResponseSchema, { containers: [review(), second] }),
  );
  let finish!: (value: { operationId: string }) => void;
  mocks.returnToDefault.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(
    <ReturnHashboardsToDefaultDialog hashboardIds={[9n, 10n]} onClose={vi.fn()} onComplete={vi.fn()} />,
  );
  await waitFor(() => expect(screen.getByRole("button", { name: "Move hashboards" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "Move hashboards" }));
  view.unmount();
  await act(async () => finish({ operationId: "return-9" }));
  expect(mocks.returnToDefault).toHaveBeenCalledOnce();
});

it("requires pool and mining permissions before reviewing or moving boards", async () => {
  mocks.permissions = ["miner:update_pools"];
  render(<ReturnHashboardsToDefaultDialog hashboardIds={[9n]} onClose={vi.fn()} onComplete={vi.fn()} />);
  await waitFor(() => expect(screen.getByText(/need permission to change pools/)).toBeVisible());
  expect(mocks.reviewReturnToDefault).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Move hashboards" })).not.toBeInTheDocument();
});
