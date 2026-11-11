import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { FRIGATE_UPDATE_RUNBOOK_URL } from "./frigateUpdate";
import FrigateUpdatePanel from "./FrigateUpdatePanel";
import {
  FrigateAgentPhase,
  FrigateRolloutPhase,
  FrigateUpdateState,
  InstanceProgressSchema,
  InstanceStageState,
  type UpdateJob,
  UpdateJobSchema,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { STATUSES } from "@/shared/features/toaster";

const mocks = vi.hoisted(() => ({
  retryUpdate: vi.fn(),
  cancelUpdate: vi.fn(),
  pushToast: vi.fn(),
}));

vi.mock("@/protoFleet/api/useFrigateUpdates", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFrigateUpdates")>()),
  useFrigateUpdateActions: () => ({ retryUpdate: mocks.retryUpdate, cancelUpdate: mocks.cancelUpdate }),
}));
vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: mocks.pushToast,
}));

const instance = (overrides: Record<string, unknown> = {}) =>
  create(InstanceProgressSchema, {
    instanceId: "frigate-a",
    stageState: InstanceStageState.STAGED,
    agentPhase: FrigateAgentPhase.STAGED,
    lastContact: timestampFromDate(new Date("2026-03-04T05:06:07Z")),
    ...overrides,
  });

const job = (overrides: Record<string, unknown> = {}): UpdateJob =>
  create(UpdateJobSchema, {
    jobId: "11111111-1111-4111-8111-111111111111",
    state: FrigateUpdateState.RUNNING,
    rolloutPhase: FrigateRolloutPhase.INSTALLING,
    fromVersion: "1.3.0",
    targetVersion: "1.4.0",
    driverInstanceId: "frigate-a",
    instances: [instance(), instance({ instanceId: "frigate-b", agentPhase: FrigateAgentPhase.INSTALLED })],
    ...overrides,
  });

const renderPanel = (jobs: UpdateJob[], extra: Record<string, unknown> = {}) => {
  const onChanged = vi.fn();
  render(<FrigateUpdatePanel jobs={jobs} loading={false} onChanged={onChanged} {...extra} />);
  return { onChanged };
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.retryUpdate.mockResolvedValue(undefined);
  mocks.cancelUpdate.mockResolvedValue(undefined);
});

describe("FrigateUpdatePanel progress", () => {
  it("shows the overall state, rollout phase, versions and the driver controller", () => {
    renderPanel([job()]);

    expect(screen.getByTestId("frigate-update-state")).toHaveTextContent("Update in progress: Installing");
    expect(screen.getByText("1.3.0 to 1.4.0")).toBeInTheDocument();
    expect(
      within(screen.getByTestId("frigate-instance-frigate-a")).getByText("Driving the update"),
    ).toBeInTheDocument();
    expect(within(screen.getByTestId("frigate-instance-frigate-b")).queryByText("Driving the update")).toBeNull();
  });

  it("shows each controller's phase and last contact, and flags only the stale one", () => {
    renderPanel([
      job({
        instances: [
          instance({ agentPhase: FrigateAgentPhase.STOPPED }),
          instance({ instanceId: "frigate-b", agentPhase: FrigateAgentPhase.INSTALLED, contactStale: true }),
        ],
      }),
    ]);

    expect(within(screen.getByTestId("frigate-instance-frigate-a")).getByText("Frigate stopped")).toBeInTheDocument();
    expect(within(screen.getByTestId("frigate-instance-frigate-b")).getByText("Installed")).toBeInTheDocument();
    expect(screen.getByTestId("frigate-instance-frigate-b-stale")).toHaveTextContent("No report for over 30 seconds");
    expect(screen.queryByTestId("frigate-instance-frigate-a-stale")).toBeNull();
    expect(within(screen.getByTestId("frigate-instance-frigate-a")).getByText(/Last contact/)).not.toHaveTextContent(
      "never",
    );
  });

  it("says a controller was never heard from when there is no contact time", () => {
    renderPanel([job({ instances: [instance({ lastContact: undefined })] })]);
    expect(screen.getByText("Last contact never")).toBeInTheDocument();
  });

  it("shows package copy progress while staging", () => {
    renderPanel([
      job({
        state: FrigateUpdateState.STAGING,
        rolloutPhase: FrigateRolloutPhase.UNSPECIFIED,
        instances: [
          instance({
            stageState: InstanceStageState.STAGING,
            agentPhase: FrigateAgentPhase.UNSPECIFIED,
            stagedBytes: 1048576n,
            sizeBytes: 4194304n,
          }),
        ],
      }),
    ]);

    expect(screen.getByTestId("frigate-update-state")).toHaveTextContent("Copying package to controllers");
    expect(screen.getByTestId("frigate-instance-frigate-a")).toHaveTextContent("Copying, 1.0 MB of 4.0 MB");
  });

  it("shows the next step", () => {
    renderPanel([job({ nextStep: "Wait for frigate-b to restart." })]);
    expect(screen.getByTestId("frigate-update-next-step")).toHaveTextContent(
      "Next step: Wait for frigate-b to restart.",
    );
  });

  it("shows a banner while the maintenance service is answering", () => {
    renderPanel([job({ servedByMaintenance: true })]);
    expect(screen.getByTestId("frigate-update-maintenance")).toHaveTextContent("Frigate is stopped for the install.");
  });

  it("does not show the maintenance banner otherwise", () => {
    renderPanel([job({ servedByMaintenance: false })]);
    expect(screen.queryByTestId("frigate-update-maintenance")).toBeNull();
  });

  it("warns when no controller is answering", () => {
    renderPanel([job({ unreachableSince: timestampFromDate(new Date("2026-03-04T05:06:07Z")) })]);
    expect(screen.getByTestId("frigate-update-unreachable")).toHaveTextContent("Fleet can't reach either controller.");
  });

  it("renders nothing without jobs or an error", () => {
    renderPanel([]);
    expect(screen.queryByTestId("frigate-update-panel")).toBeNull();
  });

  it("shows a read failure without hiding the last job", () => {
    renderPanel([job()], { error: "controller unreachable" });
    expect(screen.getByRole("alert")).toHaveTextContent("controller unreachable");
    expect(screen.getByTestId("frigate-update-state")).toBeInTheDocument();
  });
});

describe("FrigateUpdatePanel errors", () => {
  it("maps a known error code to readable text with the server detail and the code", () => {
    renderPanel([
      job({
        state: FrigateUpdateState.FAILED,
        errorCode: "UPDATE_MIGRATE_FAILED",
        errorMessage: "relation does not exist",
      }),
    ]);

    const callout = screen.getByTestId("frigate-update-error");
    expect(callout).toHaveTextContent("The database migration failed after the package was installed.");
    expect(callout).toHaveTextContent("relation does not exist");
    expect(callout).toHaveTextContent("Code: UPDATE_MIGRATE_FAILED");
  });

  it("falls back to the server message for an unknown code and still shows the code", () => {
    renderPanel([
      job({ state: FrigateUpdateState.FAILED, errorCode: "UPDATE_BRAND_NEW", errorMessage: "something odd" }),
    ]);

    const callout = screen.getByTestId("frigate-update-error");
    expect(callout).toHaveTextContent("something odd");
    expect(callout).toHaveTextContent("Code: UPDATE_BRAND_NEW");
  });

  it("uses a generic message when an unknown code arrives with no message", () => {
    renderPanel([job({ state: FrigateUpdateState.FAILED, errorCode: "FLEET_MYSTERY" })]);

    const callout = screen.getByTestId("frigate-update-error");
    expect(callout).toHaveTextContent("The update failed.");
    expect(callout).toHaveTextContent("Code: FLEET_MYSTERY");
  });

  it("shows a controller's own failure", () => {
    renderPanel([
      job({
        instances: [instance({ errorCode: "UPDATE_INSTALL_FAILED", errorMessage: "dpkg exited 1" })],
      }),
    ]);

    const row = screen.getByTestId("frigate-instance-frigate-a");
    expect(row).toHaveTextContent("Installing the package failed on a controller.");
    expect(row).toHaveTextContent("Code: UPDATE_INSTALL_FAILED");
  });

  it("links to the runbook from one constant when the update failed", () => {
    renderPanel([job({ state: FrigateUpdateState.FAILED, errorCode: "UPDATE_INSTALL_FAILED" })]);
    const link = screen.getByRole("link", { name: "Frigate update runbook" });
    expect(link).toHaveAttribute("href", FRIGATE_UPDATE_RUNBOOK_URL);
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
  });

  it("does not link the runbook for a healthy running update", () => {
    renderPanel([job()]);
    expect(screen.queryByRole("link", { name: "Frigate update runbook" })).toBeNull();
  });
});

describe("FrigateUpdatePanel retry and cancel", () => {
  it("offers neither action when the job is neither retryable nor cancellable", () => {
    renderPanel([job({ retryable: false, cancellable: false })]);
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Cancel update" })).toBeNull();
  });

  it("shows a pending cancel and offers no second cancel", () => {
    renderPanel([
      job({
        state: FrigateUpdateState.STARTING,
        rolloutPhase: FrigateRolloutPhase.UNSPECIFIED,
        cancellable: false,
        cancelRequestedAt: timestampFromDate(new Date("2026-01-01T00:00:00Z")),
      }),
    ]);
    expect(screen.getByTestId("frigate-update-state")).toHaveTextContent("Starting update: cancel requested");
    expect(screen.queryByRole("button", { name: "Cancel update" })).toBeNull();
  });

  it("offers only retry when the job is retryable", () => {
    renderPanel([job({ state: FrigateUpdateState.FAILED, retryable: true })]);
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel update" })).toBeNull();
  });

  it("offers only cancel when the job is cancellable", () => {
    renderPanel([job({ state: FrigateUpdateState.STAGING, cancellable: true })]);
    expect(screen.getByRole("button", { name: "Cancel update" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("confirms before retrying and sends nothing until confirmed", async () => {
    const { onChanged } = renderPanel([job({ state: FrigateUpdateState.FAILED, retryable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    const dialog = await screen.findByTestId("frigate-update-retry-dialog");
    expect(dialog).toHaveTextContent("Both controllers stop mining control together, briefly.");
    expect(mocks.retryUpdate).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Retry update" }));

    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mocks.retryUpdate).toHaveBeenCalledWith("11111111-1111-4111-8111-111111111111", expect.any(String));
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Frigate update retry requested.",
      status: STATUSES.success,
    });
    expect(screen.queryByTestId("frigate-update-retry-dialog")).toBeNull();
  });

  it("confirms before cancelling and sends nothing when the confirmation is dismissed", async () => {
    renderPanel([job({ state: FrigateUpdateState.STAGED, cancellable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    const dialog = await screen.findByTestId("frigate-update-cancel-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Keep update" }));

    expect(mocks.cancelUpdate).not.toHaveBeenCalled();
    expect(screen.queryByTestId("frigate-update-cancel-dialog")).toBeNull();
  });

  it("cancels once confirmed", async () => {
    mocks.cancelUpdate.mockResolvedValueOnce(job({ state: FrigateUpdateState.CANCELLED }));
    const { onChanged } = renderPanel([job({ state: FrigateUpdateState.STAGED, cancellable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    const dialog = await screen.findByTestId("frigate-update-cancel-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel update" }));

    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mocks.cancelUpdate).toHaveBeenCalledWith("11111111-1111-4111-8111-111111111111", expect.any(String));
    expect(mocks.pushToast).toHaveBeenCalledWith({ message: "Frigate update cancelled.", status: STATUSES.success });
  });

  it.each([
    ["a Start is still in flight", { state: FrigateUpdateState.STARTING, cancelRequestedAt: { seconds: 1n } }],
    ["Frigate has queued the cancel", { state: FrigateUpdateState.RUNNING }],
  ])("reports a pending cancel when %s", async (_, returned) => {
    mocks.cancelUpdate.mockResolvedValueOnce(job(returned));
    const { onChanged } = renderPanel([job({ state: FrigateUpdateState.STARTING, cancellable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    const dialog = await screen.findByTestId("frigate-update-cancel-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel update" }));

    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(mocks.pushToast).toHaveBeenCalledWith({
      message: "Cancel requested. The update stops only if no controller has stopped yet.",
      status: STATUSES.success,
    });
    expect(mocks.pushToast).not.toHaveBeenCalledWith(expect.objectContaining({ message: "Frigate update cancelled." }));
  });

  it("reuses the idempotency key when the same retry is resubmitted after a failure", async () => {
    mocks.retryUpdate
      .mockRejectedValueOnce(new ConnectError("deadline exceeded", Code.DeadlineExceeded))
      .mockResolvedValueOnce(undefined);
    renderPanel([job({ state: FrigateUpdateState.FAILED, retryable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    const dialog = await screen.findByTestId("frigate-update-retry-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry update" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("deadline exceeded");

    fireEvent.click(within(dialog).getByRole("button", { name: "Retry update" }));
    await waitFor(() => expect(mocks.retryUpdate).toHaveBeenCalledTimes(2));

    const [first, second] = mocks.retryUpdate.mock.calls;
    expect(second[1]).toBe(first[1]);
  });

  it("uses a new key for a new confirmation after the first was dismissed", async () => {
    mocks.retryUpdate.mockRejectedValue(new ConnectError("unavailable", Code.Unavailable));
    renderPanel([job({ state: FrigateUpdateState.FAILED, retryable: true })]);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    let dialog = await screen.findByTestId("frigate-update-retry-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry update" }));
    await within(dialog).findByRole("alert");
    fireEvent.click(within(dialog).getByRole("button", { name: "Not now" }));

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    dialog = await screen.findByTestId("frigate-update-retry-dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry update" }));
    await waitFor(() => expect(mocks.retryUpdate).toHaveBeenCalledTimes(2));

    expect(mocks.retryUpdate.mock.calls[1][1]).not.toBe(mocks.retryUpdate.mock.calls[0][1]);
  });
});

describe("FrigateUpdatePanel history", () => {
  it("lists earlier updates under the latest one", () => {
    renderPanel([
      job(),
      job({
        jobId: "22222222-2222-4222-8222-222222222222",
        state: FrigateUpdateState.SUCCEEDED,
        fromVersion: "1.2.0",
        targetVersion: "1.3.0",
      }),
      job({
        jobId: "33333333-3333-4333-8333-333333333333",
        state: FrigateUpdateState.CANCELLED,
        fromVersion: "",
        targetVersion: "1.2.0",
      }),
    ]);

    const history = screen.getByRole("list", { name: "Recent Frigate updates" });
    expect(within(history).getAllByRole("listitem")).toHaveLength(2);
    expect(within(history).getByText("1.2.0 to 1.3.0")).toBeInTheDocument();
    expect(within(history).getByText(/Update complete/)).toBeInTheDocument();
    expect(within(history).getByText("To 1.2.0")).toBeInTheDocument();
  });

  it("has no history section for a single job", () => {
    renderPanel([job()]);
    expect(screen.queryByText("Recent updates")).toBeNull();
  });
});
