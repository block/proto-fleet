import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { WorkerNameDialog } from "./WorkerNameDialog";
import {
  ReviewWorkerNamesResponseSchema,
  WorkerNameMode,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({ review: vi.fn(), verify: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: { reviewWorkerNames: mocks.review },
  authClient: { verifyCredentials: mocks.verify },
}));
vi.mock("./WorkerNameReview", () => ({ WorkerNameReview: () => <div>Frozen worker review</div> }));

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  useFleetStore
    .getState()
    .auth.setPermissions([
      "miner:read",
      "miner:update_worker_names",
      "pool:read",
      "miner:update_pools",
      "miner:start_mining",
      "miner:stop_mining",
    ]);
  mocks.verify.mockResolvedValue({});
  mocks.review.mockResolvedValue(create(ReviewWorkerNamesResponseSchema, { reviews: [{ reviewId: "review" }] }));
});

afterEach(() => vi.restoreAllMocks());

it.each(["none", "macAddress", "custom"])("reviews the %s choice for the selected boards", async (mode) => {
  const user = userEvent.setup();
  render(<WorkerNameDialog hashboardIds={[17n, 18n]} onDismiss={vi.fn()} />);
  await user.type(screen.getByLabelText("Username"), "operator");
  await user.type(screen.getByLabelText("Password"), "test-password");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("dialog", { name: "Update worker names" });
  await user.click(screen.getByRole("button", { name: "Worker name" }));
  await user.click(
    await screen.findByRole("option", {
      name: mode === "custom" ? "Custom worker name" : mode === "none" ? "No worker name" : "MAC address",
    }),
  );
  if (mode === "custom") {
    expect(screen.getByRole("button", { name: "Review changes" })).toBeDisabled();
    await user.type(screen.getByLabelText("Custom worker name"), "rack-1");
  }
  await user.click(screen.getByRole("button", { name: "Review changes" }));
  await waitFor(() =>
    expect(mocks.review).toHaveBeenCalledWith(
      expect.objectContaining({
        hashboardIds: [17n, 18n],
        workerName:
          mode === "custom"
            ? { mode: WorkerNameMode.CUSTOM, name: "rack-1" }
            : { mode: mode === "macAddress" ? WorkerNameMode.MAC_ADDRESS : WorkerNameMode.NONE },
      }),
    ),
  );
  expect(await screen.findByText("Frozen worker review")).toBeVisible();
});

it("hides worker controls without worker-name permission", () => {
  useFleetStore.getState().auth.setPermissions(["miner:read", "pool:read"]);
  render(<WorkerNameDialog moduleIds={[9n]} onDismiss={vi.fn()} />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

async function authenticate() {
  fireEvent.change(screen.getByLabelText("Username"), { target: { value: "operator" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByRole("dialog", { name: "Update worker names" });
}

it.each([
  { label: "No worker name", mode: "none", names: ["", ""] },
  { label: "MAC address", mode: "mac_address", names: ["02:00:00:00:00:01", "02:00:00:00:00:02"] },
  { label: "Custom worker name", mode: "custom", names: ["rack-one", "rack-one"] },
  { label: "Custom worker name", mode: "custom", names: ["02:00:00:00:00:01"] },
])("preselects $label for matching saved settings ($names)", async ({ label, mode, names }) => {
  const hashboards = names.map((workerName, index) => ({
    hashboardId: BigInt(index + 1),
    workerName,
    workerNameMode: mode,
  }));
  render(
    <WorkerNameDialog
      hashboardIds={hashboards.map((board) => board.hashboardId)}
      hashboards={hashboards}
      onDismiss={vi.fn()}
    />,
  );
  await authenticate();
  expect(screen.getByRole("button", { name: "Worker name" })).toHaveTextContent(label);
  expect(screen.getByRole("button", { name: "Review changes" })).toBeEnabled();
  if (mode === "custom") expect(screen.getByLabelText("Custom worker name")).toHaveValue(names[0]);
});

it.each([
  { label: "module", moduleIds: [9n], modes: ["custom", "custom"], names: ["shared", "shared"] },
  { label: "mixed strategies", modes: ["custom", "mac_address"], names: ["02:00:00:00:00:01", "02:00:00:00:00:01"] },
  { label: "different custom names", modes: ["custom", "custom"], names: ["one", "two"] },
  { label: "missing board data", modes: ["none"], names: [""] },
  { label: "unknown strategy", modes: ["", ""], names: ["", ""] },
])("requires an explicit choice for $label", async ({ moduleIds, modes, names }) => {
  const hashboards = names.map((workerName, index) => ({
    hashboardId: BigInt(index + 1),
    workerName,
    workerNameMode: modes[index],
  }));
  render(
    <WorkerNameDialog moduleIds={moduleIds} hashboardIds={[1n, 2n]} hashboards={hashboards} onDismiss={vi.fn()} />,
  );
  await authenticate();
  expect(screen.getByRole("button", { name: "Worker name" })).toHaveTextContent("Select worker name");
  expect(screen.getByRole("button", { name: "Review changes" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  expect(mocks.review).not.toHaveBeenCalled();
});
