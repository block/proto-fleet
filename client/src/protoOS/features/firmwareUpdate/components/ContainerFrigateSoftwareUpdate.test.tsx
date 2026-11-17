import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BinaryWriter, WireType } from "@bufbuild/protobuf/wire";
import { Code, ConnectError } from "@connectrpc/connect";
import ContainerFrigateSoftwareUpdate from "./ContainerFrigateSoftwareUpdate";
import {
  type FrigatePackage,
  type FrigateRollout,
  type FrigateServerUpdate,
  type FrigateUpdatePlan,
  type FrigateUpdateRecord,
  FrigateUpdateRefusedError,
} from "@/protoOS/api/adapters/frigateServerUpdate";
import { createFrigateServerUpdate, type ServerUpdateClient } from "@/protoOS/api/adapters/mdkV2/frigateServerUpdate";

const prefix = "proto-os:frigate-update:device:";
const pkg: FrigatePackage = {
  deb: new File([new Uint8Array(10)], "frigate_9.9.9_linux_arm64.deb"),
  statement: new Uint8Array(),
  signature: new Uint8Array(1),
  version: "9.9.9",
  architecture: "arm64",
  repository: "btc-mining/frigate",
  sha256: "a".repeat(64),
  sizeBytes: 10,
};
const plan = (blockers: string[] = []): FrigateUpdatePlan => ({
  controllers: [
    { instanceId: "ct1-a", runningVersion: "1.0.0", architecture: "arm64", spoolFreeBytes: 1e9, trustedKeyIds: ["k"] },
    { instanceId: "ct1-b", runningVersion: "1.0.0", architecture: "arm64", spoolFreeBytes: 1e9, trustedKeyIds: ["k"] },
  ],
  blockers,
});
const rollout = (fields: Partial<FrigateRollout> = {}): FrigateRollout => ({
  jobId: "job",
  targetVersion: "9.9.9",
  fromVersion: "1.0.0",
  phase: "verifying",
  ended: false,
  actor: "proto-os",
  releaseKeyId: "sha256:key",
  retryable: false,
  cancellable: true,
  instances: [],
  ...fields,
});
const staged = (fields: Partial<FrigateUpdateRecord> = {}): FrigateUpdateRecord => ({
  jobId: "job",
  idempotencyKey: "key",
  targetVersion: "9.9.9",
  packageName: pkg.deb.name,
  instanceIds: ["ct1-a", "ct1-b"],
  fingerprints: { "ct1-a": "fa", "ct1-b": "fb" },
  step: "staged",
  ...fields,
});
const saved = () =>
  Object.keys(localStorage)
    .filter((key) => key.startsWith(prefix))
    .map((key) => JSON.parse(localStorage.getItem(key)!) as FrigateUpdateRecord);

type FakeUpdate = { [K in keyof FrigateServerUpdate]: ReturnType<typeof vi.fn<FrigateServerUpdate[K]>> };
const setup = (record?: FrigateUpdateRecord, configure?: (update: FakeUpdate) => void, duringDowntime = false) => {
  const update = {
    readPackage: vi.fn<FrigateServerUpdate["readPackage"]>().mockResolvedValue(pkg),
    plan: vi.fn<FrigateServerUpdate["plan"]>().mockResolvedValue(plan()),
    stage: vi.fn<FrigateServerUpdate["stage"]>(async (_, planned, onProgress) => {
      const fingerprints = Object.fromEntries(planned.instanceIds.map((id) => [id, `m-${id}`]));
      onProgress(50, fingerprints);
      return fingerprints;
    }),
    discard: vi.fn<FrigateServerUpdate["discard"]>().mockResolvedValue(),
    start: vi.fn<FrigateServerUpdate["start"]>(),
    status: vi.fn<FrigateServerUpdate["status"]>().mockResolvedValue({ servedByMaintenance: false }),
    retry: vi.fn<FrigateServerUpdate["retry"]>(),
    cancel: vi.fn<FrigateServerUpdate["cancel"]>(),
  };
  configure?.(update);
  if (record) localStorage.setItem(prefix + record.jobId, JSON.stringify(record));
  render(<ContainerFrigateSoftwareUpdate update={update} deviceId="device" duringDowntime={duringDowntime} />);
  return update;
};
const open = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const choose = () =>
  fireEvent.change(screen.getByLabelText("Controller software release files"), {
    target: { files: [pkg.deb, new File(["{}"], "x.deb.statement.json"), new File(["s"], "x.deb.statement.sig")] },
  });

describe("ContainerFrigateSoftwareUpdate", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("shows what will be installed and keeps Update off while a controller refuses remote updates", async () => {
    const update = setup(undefined, (fake) =>
      fake.plan.mockResolvedValue(plan(["Software updates through the controller API are turned off."])),
    );
    open("Update controller software");
    choose();
    expect(await screen.findByText("frigate_9.9.9_linux_arm64.deb", { exact: false })).toBeInTheDocument();
    expect(update.readPackage).toHaveBeenCalledOnce();
    expect(update.plan).toHaveBeenLastCalledWith(pkg, expect.anything());
    const controllers = screen.getByRole("list", { name: "Controllers" });
    expect(
      within(controllers)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["ct1-aVersion 1.0.0 → 9.9.9", "ct1-bVersion 1.0.0 → 9.9.9"]);
    expect(screen.getByRole("alert")).toHaveTextContent("turned off");
    expect(screen.getByRole("button", { name: "Update" })).toBeDisabled();
  });

  it("saves the record before staging and before Start, then follows the rollout to the end", async () => {
    let savedBeforeStart: FrigateUpdateRecord[] = [];
    const update = setup(undefined, (fake) => {
      fake.start.mockImplementation(async (record) => {
        savedBeforeStart = saved();
        return rollout({ jobId: record.jobId });
      });
      fake.status.mockImplementation(async (jobId) =>
        jobId
          ? {
              rollout: rollout({ jobId, phase: "succeeded", ended: true, cancellable: false }),
              servedByMaintenance: false,
            }
          : { servedByMaintenance: false },
      );
    });
    open("Update controller software");
    choose();
    await waitFor(() => expect(screen.getByRole("button", { name: "Update" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Update" }));

    expect(await screen.findByText("Update complete")).toBeInTheDocument();
    const [, planned] = update.stage.mock.calls[0];
    expect(planned.jobId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(planned.instanceIds).toEqual(["ct1-a", "ct1-b"]);
    expect(update.start).toHaveBeenCalledWith(
      expect.objectContaining({ fingerprints: { "ct1-a": "m-ct1-a", "ct1-b": "m-ct1-b" } }),
      expect.anything(),
    );
    // The record is saved as "starting" before Start is sent, so a lost answer is never sent again blindly.
    expect(savedBeforeStart).toEqual([expect.objectContaining({ jobId: planned.jobId, step: "starting" })]);
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(saved()).toEqual([]);
  });

  it("explains a refusal and offers to start again with the same key", async () => {
    const update = setup(staged(), (fake) => {
      fake.start
        .mockRejectedValueOnce(
          new FrigateUpdateRefusedError("UPDATE_FIRMWARE_ACTIVE", "a firmware update is running: ct1-b/flash-1"),
        )
        .mockResolvedValueOnce(rollout());
      // The controllers have no rollout for this job until the second Start is accepted.
      fake.status.mockImplementation(async () =>
        fake.start.mock.calls.length > 1
          ? { rollout: rollout(), servedByMaintenance: false }
          : { servedByMaintenance: false },
      );
    });
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("A hashboard or PSU firmware update is running on a controller.");
    expect(alert).toHaveTextContent("a firmware update is running: ct1-b/flash-1");
    expect(saved()).toEqual([expect.objectContaining({ step: "staged", notStarted: expect.anything() })]);
    // Even a refusal is confirmed against the controllers before the package is offered again.
    expect(update.status).toHaveBeenCalledWith("job", expect.anything());

    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    expect(await screen.findByText("Verifying the package")).toBeInTheDocument();
    expect(update.start.mock.calls.map(([record]) => record.idempotencyKey)).toEqual(["key", "key"]);
  });

  it("follows the rollout when a refused Start finds one already there", async () => {
    const update = setup(staged(), (fake) => {
      fake.start.mockRejectedValue(new FrigateUpdateRefusedError("UPDATE_IDEMPOTENCY_CONFLICT", "key already used"));
      fake.status.mockResolvedValue({ rollout: rollout({ phase: "installing" }), servedByMaintenance: false });
    });
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
    expect(update.start).toHaveBeenCalledOnce();
  });

  it("settles a lost Start answer from status, once Start can't be in flight, instead of sending Start again", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const update = setup(staged(), (fake) => fake.start.mockRejectedValue(new Error("Failed to fetch")));
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    // Status has no rollout yet, but Frigate saves it only after its checks, so the update may still be starting.
    expect(await screen.findByText(/didn't confirm the start/)).toBeInTheDocument();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(update.status).toHaveBeenCalledWith("job", expect.anything());
    expect(saved()).toEqual([expect.objectContaining({ step: "starting" })]);
    expect(screen.getByText("Checking whether the update started…")).toBeInTheDocument();

    await vi.advanceTimersByTimeAsync(50_000);
    expect(await screen.findByText(/no record of this update starting/)).toBeInTheDocument();
    expect(screen.queryByText(/didn't confirm the start/)).not.toBeInTheDocument();
    expect(update.start).toHaveBeenCalledOnce();
    expect(saved()).toEqual([expect.objectContaining({ step: "staged" })]);
    expect(screen.getByRole("button", { name: "Start update" })).toBeEnabled();
    vi.useRealTimers();
  });

  it("keeps a reload from settling a Start that may still be in flight", async () => {
    const update = setup(staged({ step: "starting", startSettlesAt: Date.now() + 60_000 }));
    open("Controller software update status");
    await waitFor(() => expect(update.status).toHaveBeenCalledWith("job", expect.anything()));
    expect(screen.getByText("Checking whether the update started…")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "starting" })]);
  });

  it("does not ask status while Start is in flight, and sends one Start for two presses", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let answer!: (started: FrigateRollout) => void;
    const update = setup(staged(), (fake) => {
      fake.start.mockReturnValue(new Promise((resolve) => (answer = resolve)));
      fake.status.mockResolvedValue({ rollout: rollout(), servedByMaintenance: false });
    });
    open("Controller software update status");
    const start = screen.getByRole("button", { name: "Start update" });
    // Both presses land before the dialog re-renders with the button disabled.
    act(() => {
      start.click();
      start.click();
    });
    await vi.advanceTimersByTimeAsync(10_000);
    expect(update.start).toHaveBeenCalledOnce();
    expect(update.status).not.toHaveBeenCalledWith("job", expect.anything());
    answer(rollout());
    expect(await screen.findByText("Verifying the package")).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("settles Start through the real adapter when Frigate can't say whether its write landed", async () => {
    const reason = new BinaryWriter()
      .tag(1, WireType.LengthDelimited)
      .string("UPDATE_ETCD_UNAVAILABLE")
      .tag(2, WireType.LengthDelimited)
      .string("mdk.v2.frigate")
      .finish();
    const failure = new ConnectError("the shared update state is unavailable", Code.Unavailable);
    failure.details.push({ type: "google.rpc.ErrorInfo", value: reason });
    const client = {
      startServerUpdate: vi.fn().mockRejectedValue(failure),
      getServerUpdateStatus: vi.fn().mockResolvedValue({
        // The wire rollout: phase 3 is INSTALLING.
        rollout: { jobId: "job", phase: 3, instances: [], errorCode: "", errorMessage: "", nextStep: "" },
        servedByMaintenance: false,
      }),
    };
    localStorage.setItem(prefix + "job", JSON.stringify(staged()));
    render(
      <ContainerFrigateSoftwareUpdate
        update={createFrigateServerUpdate(client as unknown as ServerUpdateClient, (params = {}) => params)}
        deviceId="device"
      />,
    );
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    // The reason reads like a refusal, but the commit may have landed: status finds the rollout.
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
    expect(client.startServerUpdate).toHaveBeenCalledOnce();
  });

  it("follows a rollout whose Start answer was lost", async () => {
    const update = setup(staged({ step: "starting" }), (fake) =>
      fake.status.mockResolvedValue({
        rollout: rollout({ phase: "installing", cancellable: false }),
        servedByMaintenance: true,
      }),
    );
    open("Controller software update status");
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(screen.getByText(/maintenance service is answering/)).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
    expect(update.start).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Cancel update" })).not.toBeInTheDocument();
  });

  it("keeps the last answer while the controller is down, and lets the operator stop tracking", async () => {
    setup(staged({ step: "started" }), (fake) => fake.status.mockRejectedValue(new Error("Failed to fetch")));
    open("Controller software update status");
    expect(await screen.findByText(/The controller isn't answering \(Failed to fetch\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Stop tracking" }));
    const question = screen.getByRole("dialog", { name: "Stop tracking this update?" });
    fireEvent.click(within(question).getByRole("button", { name: "Stop tracking" }));
    expect(saved()).toEqual([]);
  });

  it("reuses a retry key after a lost answer and uses a new one after a refusal", async () => {
    const update = setup(staged({ step: "started" }), (fake) => {
      fake.status.mockResolvedValue({
        rollout: rollout({
          phase: "failed",
          ended: true,
          retryable: true,
          cancellable: false,
          problem: { code: "UPDATE_MIGRATE_FAILED", message: "exit 1" },
        }),
        servedByMaintenance: true,
      });
      fake.retry
        .mockRejectedValueOnce(new Error("Failed to fetch"))
        .mockRejectedValueOnce(new FrigateUpdateRefusedError("UPDATE_HELPER_UNAVAILABLE", "helper is down"))
        .mockImplementationOnce(async () => {
          // The poll that resumes after the answer must agree with it.
          fake.status.mockResolvedValue({ rollout: rollout({ phase: "migrating" }), servedByMaintenance: true });
          return rollout({ phase: "migrating" });
        });
    });
    open("Controller software update status");
    expect(
      await screen.findByText("The database migration failed after the package was installed."),
    ).toBeInTheDocument();
    for (let attempt = 0; attempt < 3; attempt++) {
      await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await waitFor(() => expect(update.retry).toHaveBeenCalledTimes(attempt + 1));
    }
    const keys = update.retry.mock.calls.map(([, key]) => key);
    expect(keys[1]).toBe(keys[0]);
    expect(keys[2]).not.toBe(keys[1]);
    expect(await screen.findByText("Migrating the database")).toBeInTheDocument();
  });

  it("keeps polling a retryable failure, so a retry whose answer was lost shows its outcome", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const failed = rollout({
      phase: "failed",
      ended: true,
      retryable: true,
      cancellable: false,
      problem: { code: "UPDATE_MIGRATE_FAILED", message: "exit 1" },
    });
    const update = setup(staged({ step: "started" }), (fake) => {
      fake.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
      fake.retry.mockRejectedValue(new Error("Failed to fetch"));
    });
    open("Controller software update status");
    await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(update.retry).toHaveBeenCalledTimes(1));
    update.status.mockResolvedValue({
      rollout: rollout({ phase: "succeeded", ended: true, cancellable: false }),
      servedByMaintenance: false,
    });
    await vi.advanceTimersByTimeAsync(3_000);
    expect(await screen.findByText("Update complete")).toBeInTheDocument();
    expect(update.retry).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it("adopts a retryable failure found without a record and offers Retry", async () => {
    setup(undefined, (fake) =>
      fake.status.mockResolvedValue({
        rollout: rollout({
          jobId: "fleet-job",
          phase: "failed",
          ended: true,
          retryable: true,
          cancellable: false,
          problem: { code: "UPDATE_MIGRATE_FAILED", message: "exit 1" },
        }),
        servedByMaintenance: true,
      }),
    );
    expect(await screen.findByRole("button", { name: "Controller software update status" })).toBeInTheDocument();
    open("Controller software update status");
    expect(screen.getByText("Update failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(saved()).toEqual([]);
  });

  it("does not start an update that another tab discarded while it was copying", async () => {
    let copyStopped: boolean | undefined;
    const update = setup(undefined, (fake) =>
      fake.stage.mockImplementation(async (_, planned, onProgress, params) => {
        const fingerprints = Object.fromEntries(planned.instanceIds.map((id) => [id, `m-${id}`]));
        onProgress(50, fingerprints);
        // Another tab adopts the record and discards it.
        localStorage.removeItem(prefix + planned.jobId);
        onProgress(60, fingerprints);
        copyStopped = params?.signal?.aborted;
        throw new Error("aborted");
      }),
    );
    open("Update controller software");
    choose();
    await waitFor(() => expect(screen.getByRole("button", { name: "Update" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("discarded in another tab");
    expect(copyStopped).toBe(true);
    expect(update.start).not.toHaveBeenCalled();
    expect(update.discard).toHaveBeenCalledWith(
      expect.objectContaining({ fingerprints: { "ct1-a": "m-ct1-a", "ct1-b": "m-ct1-b" } }),
      expect.anything(),
    );
    expect(saved()).toEqual([]);
    expect(screen.getByRole("dialog", { name: "Update controller software" })).toBeInTheDocument();
  });

  it("does not start a copied package that another tab discarded", async () => {
    const update = setup(staged());
    open("Controller software update status");
    localStorage.clear();
    fireEvent.click(screen.getByRole("button", { name: "Start update" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("discarded in another tab");
    expect(update.start).not.toHaveBeenCalled();
  });

  it("finds a rollout that began after the page loaded, even while only maintenance answers", async () => {
    const update = setup(undefined, (fake) => fake.plan.mockRejectedValue(new Error("The controller is restarting.")));
    await waitFor(() => expect(update.status).toHaveBeenCalledTimes(1));
    update.status.mockResolvedValue({
      rollout: rollout({
        jobId: "fleet-job",
        phase: "failed",
        ended: true,
        retryable: true,
        cancellable: false,
        problem: { code: "UPDATE_MIGRATE_FAILED", message: "exit 1" },
      }),
      servedByMaintenance: true,
    });
    open("Update controller software");
    expect(await screen.findByText("Update failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(update.plan).not.toHaveBeenCalled();
  });

  it("discards a copied package that never started", async () => {
    const update = setup(staged());
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(saved()).toEqual([]));
    expect(update.discard).toHaveBeenCalledWith(expect.objectContaining({ jobId: "job" }), expect.anything());
    expect(screen.getByRole("dialog", { name: "Update controller software" })).toBeInTheDocument();
  });

  it("follows a rollout started elsewhere instead of offering another", async () => {
    const update = setup(undefined, (fake) =>
      fake.status.mockResolvedValue({
        rollout: rollout({ jobId: "fleet-job", actor: "proto-fleet:ops" }),
        servedByMaintenance: false,
      }),
    );
    expect(await screen.findByRole("button", { name: "Controller software update status" })).toBeInTheDocument();
    open("Controller software update status");
    expect(screen.getByText("Verifying the package")).toBeInTheDocument();
    expect(screen.getByText(/Started by proto-fleet:ops/)).toBeInTheDocument();
    expect(saved()).toEqual([]);
    expect(update.status).toHaveBeenCalledWith(undefined, expect.anything());
  });

  it("keeps the package when a controller doesn't confirm the discard, and follows a job that had started", async () => {
    const update = setup(staged(), (fake) => {
      fake.discard
        .mockRejectedValueOnce(new Error("Controller ct1-b didn't confirm the package was removed."))
        .mockRejectedValueOnce(new FrigateUpdateRefusedError("UPDATE_ACTIVE_JOB", "cancel it instead"));
      fake.status.mockImplementation(async (jobId) =>
        jobId
          ? { rollout: rollout({ phase: "installing" }), servedByMaintenance: false }
          : { servedByMaintenance: false },
      );
    });
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("didn't confirm the package was removed");
    expect(saved()).toEqual([expect.objectContaining({ step: "staged" })]);

    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
    expect(update.discard).toHaveBeenCalledTimes(2);
  });

  it("follows another tab's progress instead of acting on a stale copy", async () => {
    const update = setup(staged(), (fake) =>
      fake.status.mockResolvedValue({ rollout: rollout({ phase: "stopping" }), servedByMaintenance: false }),
    );
    open("Controller software update status");
    expect(screen.getByRole("button", { name: "Start update" })).toBeEnabled();
    // Another tab starts the update. This tab hears about it.
    const key = prefix + "job";
    localStorage.setItem(key, JSON.stringify(staged({ step: "started" })));
    act(() => void window.dispatchEvent(new StorageEvent("storage", { key })));
    expect(await screen.findByText("Stopping controller software")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discard" })).not.toBeInTheDocument();
    expect(update.start).not.toHaveBeenCalled();
    expect(update.discard).not.toHaveBeenCalled();
  });

  it("re-reads the saved step before Start and Discard, even without hearing of the change", async () => {
    const update = setup(staged(), (fake) =>
      fake.status.mockResolvedValue({ rollout: rollout({ phase: "stopping" }), servedByMaintenance: false }),
    );
    open("Controller software update status");
    localStorage.setItem(prefix + "job", JSON.stringify(staged({ step: "started" })));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Stopping controller software")).toBeInTheDocument();
    expect(update.discard).not.toHaveBeenCalled();
    expect(update.start).not.toHaveBeenCalled();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
  });

  it("refuses to copy when the recovery record can't be saved", async () => {
    const update = setup();
    open("Update controller software");
    choose();
    await waitFor(() => expect(screen.getByRole("button", { name: "Update" })).toBeEnabled());
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("The quota has been exceeded.");
    });
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The quota has been exceeded.");
    expect(update.stage).not.toHaveBeenCalled();
    expect(update.start).not.toHaveBeenCalled();
  });

  it("cancels an update, and keeps a retryable failure on screen", async () => {
    const failed = rollout({ phase: "failed", ended: true, retryable: true, cancellable: true });
    const update = setup(staged({ step: "started" }), (fake) => {
      fake.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
      fake.cancel.mockImplementation(async () => {
        const cancelled = rollout({ phase: "cancelled", ended: true, cancellable: false });
        fake.status.mockResolvedValue({ rollout: cancelled, servedByMaintenance: false });
        return cancelled;
      });
    });
    open("Controller software update status");
    expect(await screen.findByText("Update failed")).toBeInTheDocument();
    // Frigate may be stopped until this is retried or cancelled, so it can't be dismissed.
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    expect(await screen.findByText("Update cancelled")).toBeInTheDocument();
    expect(update.cancel).toHaveBeenCalledExactlyOnceWith("job", expect.any(String), expect.anything());
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
  });

  it("replays a retry until status shows it took effect, then uses a new key", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const failed = rollout({ phase: "failed", ended: true, retryable: true, cancellable: false });
    const update = setup(staged({ step: "started" }), (fake) => {
      fake.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
      // Accepted, but the answer still shows the failure: the retry hasn't taken effect yet.
      fake.retry.mockResolvedValue(failed);
    });
    open("Controller software update status");
    const press = async (times: number) => {
      await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await waitFor(() => expect(update.retry).toHaveBeenCalledTimes(times));
    };
    await press(1);
    await press(2);
    // Status shows the retry running, then failing again: the next press is a new request.
    update.status.mockResolvedValue({ rollout: rollout({ phase: "migrating" }), servedByMaintenance: true });
    await vi.advanceTimersByTimeAsync(3_000);
    expect(await screen.findByText("Migrating the database")).toBeInTheDocument();
    update.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
    await vi.advanceTimersByTimeAsync(3_000);
    await press(3);
    const keys = update.retry.mock.calls.map(([, key]) => key);
    expect(keys[1]).toBe(keys[0]);
    expect(keys[2]).not.toBe(keys[1]);
    vi.useRealTimers();
  });

  it("uses a new retry key once the rollout changed, even if it never looked like it was running", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const failed = (updatedAt: number) =>
      rollout({ phase: "failed", ended: true, retryable: true, cancellable: false, updatedAt });
    const update = setup(staged({ step: "started" }), (fake) => {
      fake.status.mockResolvedValue({ rollout: failed(1_000), servedByMaintenance: true });
      // The answer to the first retry is lost.
      fake.retry.mockRejectedValueOnce(new Error("Failed to fetch")).mockResolvedValue(failed(2_000));
    });
    open("Controller software update status");
    const press = async (times: number) => {
      await waitFor(() => expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled());
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      await waitFor(() => expect(update.retry).toHaveBeenCalledTimes(times));
    };
    await press(1);
    // Frigate had accepted it, and the attempt failed again before the next status answer: the rollout looks
    // retryable throughout, and only its change time shows the key was used.
    update.status.mockResolvedValue({ rollout: failed(2_000), servedByMaintenance: true });
    await vi.advanceTimersByTimeAsync(3_000);
    await waitFor(() => expect(saved()[0].pendingAction).toBeUndefined());
    await press(2);
    // Nothing changed since the second retry, so a third press replays it.
    await press(3);
    const keys = update.retry.mock.calls.map(([, key]) => key);
    expect(keys[1]).not.toBe(keys[0]);
    expect(keys[2]).toBe(keys[1]);
    vi.useRealTimers();
  });

  it("leaves another tab's new update alone when a slow discard finishes", async () => {
    let finish = () => {};
    const update = setup(staged(), (fake) =>
      fake.discard.mockReturnValue(new Promise<void>((resolve) => (finish = resolve))),
    );
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(update.discard).toHaveBeenCalledTimes(1));
    // Meanwhile another tab discards the same package and begins copying a new one.
    localStorage.removeItem(prefix + "job");
    localStorage.setItem(prefix + "next", JSON.stringify(staged({ jobId: "next", step: "staging" })));
    act(() => void window.dispatchEvent(new StorageEvent("storage", { key: prefix + "next" })));
    await act(async () => finish());
    await waitFor(() => expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled());
    expect(saved().map((record) => record.jobId)).toEqual(["next"]);
  });

  it("shows a followed update with its dialog open while the app waits for Frigate", async () => {
    const failed = rollout({ phase: "failed", ended: true, retryable: true, cancellable: false });
    const update = setup(
      staged({ step: "started" }),
      (fake) => {
        fake.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
        fake.retry.mockImplementation(async () => {
          const installing = rollout({ phase: "installing" });
          fake.status.mockResolvedValue({ rollout: installing, servedByMaintenance: true });
          return installing;
        });
      },
      true,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(update.retry).toHaveBeenCalledTimes(1);
  });

  it("shows nothing while the app waits for Frigate unless an update is followed", async () => {
    const update = setup(staged(), undefined, true);
    await waitFor(() => expect(update.status).toHaveBeenCalledWith(undefined, expect.anything()));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("follows someone else's failed update past a copy that never started, and keeps the copy", async () => {
    const failed = rollout({ jobId: "fleet-job", phase: "failed", ended: true, retryable: true, cancellable: false });
    const update = setup(
      staged(),
      (fake) => {
        fake.status.mockResolvedValue({ rollout: failed, servedByMaintenance: true });
        fake.retry.mockResolvedValue(
          rollout({ jobId: "fleet-job", phase: "succeeded", ended: true, cancellable: false }),
        );
      },
      true,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    await waitFor(() => expect(update.retry).toHaveBeenCalledWith("fleet-job", expect.any(String), expect.anything()));
    expect(await screen.findByText("Update complete")).toBeInTheDocument();
    // Done releases the update that was followed. The copy is still this browser's to start or discard.
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(saved()).toEqual([expect.objectContaining({ jobId: "job", step: "staged" })]);
  });

  it("follows its own copy when status shows it started", async () => {
    setup(staged(), (fake) =>
      fake.status.mockResolvedValue({ rollout: rollout({ phase: "installing" }), servedByMaintenance: false }),
    );
    open("Controller software update status");
    expect(await screen.findByText("Installing")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ step: "started" })]);
  });

  it("keeps a copy that can't be discarded while someone else's update runs", async () => {
    const update = setup(staged(), (fake) => {
      fake.discard.mockRejectedValue(new FrigateUpdateRefusedError("UPDATE_ACTIVE_JOB", "another job is active"));
      fake.status
        .mockResolvedValueOnce({ servedByMaintenance: false })
        .mockImplementation(async (jobId) =>
          jobId === "job"
            ? { servedByMaintenance: false }
            : { rollout: rollout({ jobId: "fleet-job", phase: "installing" }), servedByMaintenance: false },
        );
    });
    await waitFor(() => expect(update.status).toHaveBeenCalledTimes(1));
    open("Controller software update status");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Another software update is running");
    expect(screen.getByText("Installing")).toBeInTheDocument();
    expect(saved()).toEqual([expect.objectContaining({ jobId: "job", step: "staged" })]);
  });

  it("offers to stop tracking a copy whose discard failed", async () => {
    setup(staged(), (fake) => fake.discard.mockRejectedValue(new Error("Controller ct1-b can't be reached.")));
    open("Controller software update status");
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stop tracking" }));
    expect(screen.getByText(/may stay on the controllers/)).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: "Stop tracking" }).slice(-1)[0]);
    expect(saved()).toEqual([]);
    expect(await screen.findByRole("button", { name: "Update controller software" })).toBeInTheDocument();
  });

  it("keeps a followed update on screen through an unanswered poll while the app waits for Frigate", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const update = setup(
      staged({ step: "started" }),
      (fake) => fake.status.mockRejectedValueOnce(new Error("Failed to fetch")),
      true,
    );
    expect(await screen.findByText(/The controller isn't answering/)).toBeInTheDocument();
    // Stopping here would leave no way back to Retry: the page can't be reloaded while Frigate is stopped.
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    update.status.mockResolvedValue({
      rollout: rollout({ phase: "failed", ended: true, retryable: true, cancellable: false }),
      servedByMaintenance: true,
    });
    await vi.advanceTimersByTimeAsync(3_000);
    expect(await screen.findByRole("button", { name: "Retry" })).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("stops looking for a rollout once the controller refuses the question", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const update = setup(undefined, (fake) =>
      fake.status.mockRejectedValue(new FrigateUpdateRefusedError("", "unimplemented")),
    );
    await waitFor(() => expect(update.status).toHaveBeenCalledTimes(1));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(update.status).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Update controller software" })).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("resumes a stopped copy with the same job and fingerprints, without starting it", async () => {
    const update = setup(undefined, (fake) => {
      fake.stage.mockImplementationOnce(async (_, _planned, onProgress, params) => {
        onProgress(50, { "ct1-a": "m-ct1-a" });
        await new Promise((_resolve, reject) =>
          params?.signal?.addEventListener("abort", () => reject(new Error("aborted"))),
        );
        return {};
      });
      fake.start.mockImplementation(async (record) => rollout({ jobId: record.jobId }));
    });
    open("Update controller software");
    choose();
    await waitFor(() => expect(screen.getByRole("button", { name: "Update" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Update" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stop copying" }));

    const again = await screen.findByRole("button", { name: "Copy again" });
    expect(update.start).not.toHaveBeenCalled();
    const [stopped] = saved();
    expect(stopped).toEqual(expect.objectContaining({ step: "staging", fingerprints: { "ct1-a": "m-ct1-a" } }));

    fireEvent.click(again);
    await waitFor(() => expect(update.stage).toHaveBeenCalledTimes(2));
    const [, resumed] = update.stage.mock.calls[1];
    expect(resumed).toEqual(expect.objectContaining({ jobId: stopped.jobId, fingerprints: { "ct1-a": "m-ct1-a" } }));
    await waitFor(() => expect(update.start).toHaveBeenCalledOnce());
    expect(update.start).toHaveBeenCalledWith(expect.objectContaining({ jobId: stopped.jobId }), expect.anything());
  });

  it("shows an update started elsewhere while the app waits for Frigate", async () => {
    setup(
      undefined,
      (fake) =>
        fake.status.mockResolvedValue({
          rollout: rollout({ jobId: "fleet-job", phase: "migrating" }),
          servedByMaintenance: true,
        }),
      true,
    );
    expect(await screen.findByText("Migrating the database")).toBeInTheDocument();
  });

  it("asks again for a rollout to follow when the first lookup gets no answer", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const update = setup(undefined, (fake) => fake.status.mockRejectedValueOnce(new Error("Failed to fetch")));
    await waitFor(() => expect(update.status).toHaveBeenCalledTimes(1));
    update.status.mockResolvedValue({ rollout: rollout({ jobId: "fleet-job" }), servedByMaintenance: false });
    await vi.advanceTimersByTimeAsync(3_000);
    expect(await screen.findByRole("button", { name: "Controller software update status" })).toBeInTheDocument();
    // One answer is enough: it doesn't keep polling with nothing to follow.
    expect(update.status.mock.calls.filter(([jobId]) => jobId === undefined)).toHaveLength(2);
    vi.useRealTimers();
  });
});
