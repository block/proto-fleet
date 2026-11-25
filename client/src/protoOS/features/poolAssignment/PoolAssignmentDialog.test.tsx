import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { readPoolAttempts, savePoolAttempt } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import {
  type PoolAssignmentAttempt,
  type PoolAssignmentPlan,
  type PoolAssignmentService,
  type WorkerNameReview,
  WorkerNameUpdateRolledBack,
} from "@/protoOS/api/adapters/poolAssignment";
import { pushToast, updateToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: vi.fn(() => 42),
  updateToast: vi.fn(),
}));

const A = "aa:bb:cc:00:00:01",
  B = "aa:bb:cc:00:00:02";
const pool = {
  id: "1",
  name: "Primary",
  url: "stratum+tcp://pool:3333",
  username: "account.region.worker",
  priority: 0,
  enabled: true,
};
const saved = { id: "saved", name: "Saved pools", revision: "2", mining: true, pools: [pool], projectedBoards: 2 };
const capacity = { groupsUsed: 2, maxGroups: 5, maxBoards: 4, maxPools: 4 };
const target = { macs: [A, B] };
const boards = [
  { mac: A, location: "Tank 1 · Module 1 · Board 1" },
  { mac: B, location: "Tank 1 · Module 1 · Board 2" },
];
const attempt: PoolAssignmentAttempt = {
  id: "attempt",
  macs: [B],
  destinationId: "saved",
  destinationRef: "proto-os:pools:saved",
  pools: [pool],
  configurationConfirmed: true,
  operationIds: ["operation"],
  phase: "Moving selected hashboards",
};
const pendingStatus = {
  state: "pending",
  message: "Waiting for board stop",
  confirmed: 0,
  total: 1,
  blockers: [],
};
const fixture = () => {
  const service = {
    listConfigurations: vi.fn().mockResolvedValue([]),
    listConfigurationPage: vi.fn().mockResolvedValue({ configurations: [], total: 0, capacity }),
    deleteConfiguration: vi.fn().mockResolvedValue(undefined),
    inspect: vi.fn().mockResolvedValue({
      savedPools: [saved],
      mixedConfigurations: false,
      capacity,
    }),
    inspectWorkerNames: vi
      .fn<PoolAssignmentService["inspectWorkerNames"]>()
      .mockImplementation(async (selection) => [
        { configuration: saved, boards: boards.filter(({ mac }) => selection.macs.includes(mac)) },
      ]),
    reviewWorkerNames: vi.fn<PoolAssignmentService["reviewWorkerNames"]>(),
    applyWorkerNames: vi.fn<PoolAssignmentService["applyWorkerNames"]>(),
    review: vi.fn().mockImplementation(async (current) => ({
      id: "next-attempt",
      name: "Saved pools",
      target: current,
      boards: boards.filter((board) => current.macs.includes(board.mac)),
      stamp: "review",
      pools: [],
      savedPoolSet: saved,
      capacity: { ...capacity, destinationBoards: current.macs.length },
      mining: true,
      requiresActivation: false,
      destinationId: "saved",
      destinationRef: "proto-os:pools:saved",
      transfers: [{ source: "default", macs: current.macs }],
    })),
    apply: vi
      .fn()
      .mockImplementation(async (plan: PoolAssignmentPlan, track: (attempt: PoolAssignmentAttempt) => void) => {
        track({ ...attempt, id: plan.id });
        return {
          state: "saved",
          message: "Pool assignment saved.",
          confirmed: 1,
          total: 1,
          blockers: [],
        };
      }),
    status: vi.fn().mockResolvedValue({
      state: "saved",
      message: "Pool assignment saved.",
      confirmed: 1,
      total: 1,
      blockers: [],
    }),
    checkMiningRecovery: vi.fn().mockImplementation(async (current, _controllerId, track) => {
      track({ ...current, restoreLegacyMining: false });
      return { state: "unknown", message: "Check saved changes.", confirmed: 0, total: 0, blockers: [] };
    }),
  } satisfies PoolAssignmentService;
  return { service };
};
beforeEach(() => {
  const locks = new Map<string, Promise<void>>();
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: {
      request: (key: string, { signal }: { signal: AbortSignal }, callback: () => Promise<void>) => {
        const pending = (locks.get(key) ?? Promise.resolve()).then(() => {
          signal.throwIfAborted();
          return callback();
        });
        locks.set(
          key,
          pending.catch(() => {}),
        );
        return pending;
      },
    },
  });
  localStorage.clear();
  vi.mocked(pushToast).mockClear();
  vi.mocked(updateToast).mockClear();
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});

test.each([
  "during save",
  "during status polling",
  "after leaving the module",
  "when status reads fail",
  "when the transfer remains pending",
  "after a rejected copy rolls back",
] as const)("uses Fleet confirmations and reports worker name progress in a toast %s", async (completion) => {
  const { service } = fixture();
  const boardTarget = { macs: [A] };
  const username = "account.region.rig-02";
  const updatedPool = { ...pool, username };
  const review: WorkerNameReview = {
    groups: [
      {
        workerName: "rig-02",
        assignment: {
          id: "worker-review",
          name: "Saved pools",
          target: boardTarget,
          boards: [boards[0]],
          stamp: "review",
          originalPools: saved.pools,
          pools: [{ ...updatedPool, password: "", preservePassword: true }],
          capacity: { ...capacity, destinationBoards: 1 },
          mining: true,
          requiresActivation: false,
          destinationId: saved.id,
          destinationRef: "proto-os:pools:saved",
          transfers: [],
        },
      },
    ],
    unchangedCount: 0,
    capacity,
  };
  service.reviewWorkerNames.mockResolvedValue(review);
  let finishSave!: () => void;
  const saving = new Promise<void>((resolve) => {
    finishSave = resolve;
  });
  let writeSignal: AbortSignal | null | undefined;
  service.applyWorkerNames.mockImplementation(async (_review, callbacks, options) => {
    writeSignal = options?.signal;
    const completed = {
      ...attempt,
      id: "worker-review",
      macs: [A],
      destinationId: saved.id,
      pools: [updatedPool],
      unsubmittedWorkerNameBoards: 0,
    };
    callbacks.track(completed);
    await saving;
    if (completion === "after a rejected copy rolls back") throw new WorkerNameUpdateRolledBack(completed.id);
    if (completion !== "during save") return { ...pendingStatus, state: "pending" };
    callbacks.saved(completed);
    return { state: "saved", message: "Worker names saved.", confirmed: 1, total: 1, blockers: [] };
  });
  const dismiss = vi.fn();
  const dialog = (
    <PoolAssignmentDialog service={service} controllerId="ct1" target={boardTarget} workerNames onDismiss={dismiss} />
  );
  const view = render(dialog);
  const editor = await screen.findByRole("dialog", { name: "Update worker names" });
  const apply = within(editor).getByRole("button", { name: "Apply to 1 hashboard" });
  await waitFor(() => expect(apply).toBeEnabled());
  fireEvent.click(apply);
  const noChanges = await screen.findByRole("dialog", { name: "You haven't made any changes" });
  fireEvent.click(within(noChanges).getByRole("button", { name: "No, keep editing" }));
  expect(service.reviewWorkerNames).not.toHaveBeenCalled();
  fireEvent.click(await within(editor).findByRole("checkbox", { name: "Custom" }));
  fireEvent.click(within(editor).getByRole("button", { name: "Open Custom options" }));
  const options = await screen.findByRole("dialog", { name: "Options" });
  fireEvent.change(within(options).getByTestId("custom-property-prefix-input"), { target: { value: "rig-0" } });
  fireEvent.change(within(options).getByTestId("custom-property-counter-start-input"), { target: { value: "2" } });
  fireEvent.click(within(options).getByTestId("custom-property-options-save-button"));
  expect(await within(editor).findAllByText("rig-02")).not.toHaveLength(0);
  fireEvent.click(apply);
  const overwrite = await screen.findByRole("dialog", { name: "Overwrite existing worker names?" });
  expect(service.reviewWorkerNames).not.toHaveBeenCalled();
  fireEvent.click(within(overwrite).getByRole("button", { name: "Yes, continue" }));
  const confirmation = await screen.findByRole("dialog", { name: "Review worker names" });
  expect(confirmation).toHaveTextContent("account.region.worker");
  expect(confirmation).toHaveTextContent("account.region.rig-02");
  expect(confirmation).toHaveTextContent("rig-02");
  expect(screen.queryByLabelText("Pool password (optional)")).not.toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.reviewWorkerNames).toHaveBeenCalledWith(
    boardTarget,
    {
      namesByMac: { [A]: "rig-02" },
      sources: [{ configuration: saved, boards: [boards[0]] }],
    },
    expect.anything(),
  );
  expect(service.applyWorkerNames).not.toHaveBeenCalled();
  fireEvent.click(within(confirmation).getByRole("button", { name: "Save and update equipment" }));
  await waitFor(() => expect(readPoolAttempts("ct1")).toHaveLength(1));
  expect(pushToast).toHaveBeenCalledExactlyOnceWith({
    message: "Updating worker name",
    status: "loading",
    longRunning: true,
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(dismiss).not.toHaveBeenCalled();

  if (completion === "after leaving the module") {
    view.rerender(<p>Settings page</p>);
    expect(writeSignal?.aborted).toBe(false);
    await act(async () => finishSave());
    expect(service.status).toHaveBeenCalledOnce();
    expect(updateToast).toHaveBeenLastCalledWith(42, { message: "Worker names saved.", status: "success" });
    expect(dismiss).not.toHaveBeenCalled();
    expect(readPoolAttempts("ct1")).toEqual([]);
    return;
  }

  if (completion === "after a rejected copy rolls back") {
    await act(async () => finishSave());
    expect(readPoolAttempts("ct1")).toEqual([]);
    expect(dismiss).not.toHaveBeenCalled();
    const message = new WorkerNameUpdateRolledBack("worker-review").message;
    await waitFor(() => expect(screen.getByText(message)).toBeVisible());
    expect(updateToast).toHaveBeenLastCalledWith(42, { message, status: "error" });
    expect(service.status).not.toHaveBeenCalled();
    return;
  }

  if (completion === "when the transfer remains pending") {
    vi.useFakeTimers();
    try {
      service.status.mockResolvedValue({ ...pendingStatus, state: "pending" });
      await act(async () => finishSave());
      await act(() => vi.advanceTimersByTimeAsync(59_000));
      expect(service.status.mock.calls.length).toBeGreaterThan(1);
      expect(dismiss).not.toHaveBeenCalled();
      await act(() => vi.advanceTimersByTimeAsync(1_000));
      expect(updateToast).toHaveBeenLastCalledWith(42, {
        message: "The controller is still updating worker names. Check Pool assignment status in Settings → Pools.",
        status: "error",
      });
      expect(dismiss).toHaveBeenCalledOnce();
      expect(readPoolAttempts("ct1")).toMatchObject([{ id: "worker-review", operationIds: ["operation"] }]);
      const checked = service.status.mock.calls.length;
      await act(() => vi.advanceTimersByTimeAsync(60_000));
      expect(service.status).toHaveBeenCalledTimes(checked);
      expect(service.applyWorkerNames).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
    return;
  }

  if (completion === "when status reads fail") {
    vi.useFakeTimers();
    try {
      service.status.mockRejectedValue(new Error("Controller unavailable"));
      await act(async () => finishSave());
      expect(service.status).toHaveBeenCalledTimes(1);
      await act(() => vi.advanceTimersByTimeAsync(6_000));
      expect(service.status).toHaveBeenCalledTimes(2);
      expect(dismiss).not.toHaveBeenCalled();
      await act(() => vi.advanceTimersByTimeAsync(12_000));
      expect(updateToast).toHaveBeenLastCalledWith(42, {
        message: "Controller unavailable Check Pool assignment status in Settings → Pools.",
        status: "error",
      });
      expect(dismiss).toHaveBeenCalledOnce();
      expect(readPoolAttempts("ct1")).toHaveLength(1);
      await act(() => vi.advanceTimersByTimeAsync(60_000));
      expect(service.status).toHaveBeenCalledTimes(3);
      expect(pushToast).toHaveBeenCalledOnce();
      expect(service.applyWorkerNames).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
    return;
  }

  await act(async () => finishSave());
  await waitFor(() => expect(dismiss).toHaveBeenCalledOnce());
  expect(updateToast).toHaveBeenLastCalledWith(42, { message: "Worker names saved.", status: "success" });
  expect(pushToast).toHaveBeenCalledTimes(1);
  expect(service.status).toHaveBeenCalledTimes(completion === "during status polling" ? 1 : 0);
  expect(readPoolAttempts("ct1")).toEqual([]);
});

test("warns when two reported serials produce the same worker name", async () => {
  const { service } = fixture();
  service.inspectWorkerNames.mockResolvedValue([
    {
      configuration: saved,
      boards: [
        { ...boards[0], serialNumber: "A B" },
        { ...boards[1], serialNumber: "A-B" },
      ],
    },
  ]);
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} workerNames onDismiss={vi.fn()} />);
  const editor = await screen.findByRole("dialog", { name: "Update worker names" });
  const apply = within(editor).getByRole("button", { name: "Apply to 2 hashboards" });
  await waitFor(() => expect(apply).toBeEnabled());
  expect(within(editor).queryByRole("radio", { name: "Period ( . )" })).not.toBeInTheDocument();
  fireEvent.click(within(editor).getByRole("checkbox", { name: "Serial number" }));
  fireEvent.click(apply);
  expect(await screen.findByRole("dialog", { name: "Duplicate names" })).toBeInTheDocument();
  expect(service.reviewWorkerNames).not.toHaveBeenCalled();
});

test("reviews only the remaining boards after a partial transfer", async () => {
  const { service } = fixture();
  savePoolAttempt("ct1", { ...attempt, macs: [A, B] });
  service.status.mockResolvedValue({
    state: "partial",
    message: "One board moved.",
    confirmed: 1,
    total: 2,
    blockers: [],
    remainingMacs: [B],
  });
  const dialog = <PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />;
  const firstRender = render(dialog);
  fireEvent.click(await screen.findByRole("button", { name: "Review remaining boards" }));
  expect(await screen.findByRole("dialog", { name: "Assign pool configuration to 1 hashboard?" })).toBeInTheDocument();
  expect(service.review).toHaveBeenCalledWith({ macs: [B] }, { pools: [], savedPoolSetId: "saved" }, expect.anything());
  expect(service.apply).not.toHaveBeenCalled();
  expect(readPoolAttempts("ct1")).toEqual([{ ...attempt, macs: [A, B] }]);

  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  fireEvent.click(await screen.findByRole("button", { name: "Review remaining boards" }));
  await screen.findByRole("button", { name: "Assign" });
  firstRender.unmount();
  render(dialog);

  fireEvent.click(await screen.findByRole("button", { name: "Review remaining boards" }));
  service.apply.mockRejectedValueOnce(new Error("Pool assignments changed."));
  fireEvent.click(await screen.findByRole("button", { name: "Assign" }));
  expect(await screen.findByText("Pool assignments changed.")).toBeInTheDocument();
  expect(readPoolAttempts("ct1")).toEqual([{ ...attempt, macs: [A, B] }]);

  fireEvent.click(screen.getByRole("button", { name: "Review remaining boards" }));
  fireEvent.click(await screen.findByRole("button", { name: "Assign" }));
  await waitFor(() => expect(pushToast).toHaveBeenCalledWith({ message: "Pool assignment saved.", status: "success" }));
  expect(readPoolAttempts("ct1")).toEqual([]);
});

test("reviews the boards, credentials, and group capacity for a selected module", async () => {
  const { service } = fixture();
  service.inspect.mockResolvedValue({
    savedPools: [saved],
    mixedConfigurations: true,
    capacity,
  });
  const dismiss = vi.fn();
  const selection = { ...target, modules: [[A, B]] };
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={dismiss} />);
  const picker = within(await screen.findByRole("dialog", { name: "Edit pool" }));
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Your password")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Review assignment" })).toBeDisabled();
  const savedPool = await picker.findByRole("radio", { name: "Saved pools" });
  await waitFor(() => expect(picker.getByText("Mixed configurations")).toBeVisible());
  expect(picker.getAllByRole("columnheader").map((header) => header.textContent)).toEqual([
    "Name",
    "Primary pool",
    "Backup pool",
  ]);
  fireEvent.click(savedPool);
  fireEvent.click(screen.getByRole("button", { name: "Review assignment" }));
  const confirmation = within(
    await screen.findByRole("dialog", { name: "Assign pool configuration to 2 hashboards?" }),
  );
  await waitFor(() =>
    expect(confirmation.getByText("Assign Saved pools? Pool changes may briefly interrupt mining.")).toBeVisible(),
  );
  expect(confirmation.getByText(pool.url)).toBeVisible();
  expect(confirmation.getByText("Username: account.region.worker")).toBeVisible();
  expect(confirmation.getByText("Affected hashboards (2)")).toBeVisible();
  expect(confirmation.getByText("2 of 4 hashboard places will be used in this group.")).toBeVisible();
  expect(service.review).toHaveBeenCalledWith(
    selection,
    { name: "Saved pools", pools: [], savedPoolSetId: "saved" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(confirmation.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(dismiss).toHaveBeenCalledOnce());
  expect(service.apply).toHaveBeenCalledTimes(1);
});

test.each([
  { label: "one hashboard", selection: { macs: [A] } },
  { label: "one module with multiple hashboards", selection: { macs: [A, B], modules: [[A, B]] } },
])("requires review before assigning $label", async ({ selection }) => {
  const { service } = fixture();
  const dismiss = vi.fn();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={dismiss} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Review assignment" }));
  const confirmation = await screen.findByRole("dialog", { name: /Assign pool configuration to/ });
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(within(confirmation).getByRole("button", { name: "Assign" }));

  await waitFor(() => expect(dismiss).toHaveBeenCalledOnce());
  expect(pushToast).toHaveBeenCalledWith({ message: "Pool assignment saved.", status: "success" });
  expect(readPoolAttempts("ct1")).toEqual([]);
  expect(service.apply).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({ target: selection }),
    expect.any(Function),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(screen.queryByRole("dialog", { name: /Assign pool configuration to/ })).not.toBeInTheDocument();
});

test.each([
  { destination: "running", mining: true },
  { destination: "stopped", mining: false },
])("checks the mining intent when assigning one unassigned hashboard to a $destination pool", async ({ mining }) => {
  const { service } = fixture();
  const review = service.review.getMockImplementation()!;
  service.review.mockImplementationOnce(async (selection) => ({ ...(await review(selection)), mining }));
  const selection = { macs: [A], unassignedOnly: true };
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));

  if (mining) {
    const confirmation = within(
      await screen.findByRole("dialog", { name: "Assign pool configuration to 1 hashboard?" }),
    );
    await waitFor(() =>
      expect(
        confirmation.getByText("Assign Saved pools? This hashboard can start mining when assigned."),
      ).toBeVisible(),
    );
    expect(service.apply).not.toHaveBeenCalled();
    fireEvent.click(confirmation.getByRole("button", { name: "Assign" }));
  }

  await waitFor(() =>
    expect(service.apply).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ target: selection, mining }),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    ),
  );
});

test("offers a new configuration when no saved configuration can take the unassigned boards", async () => {
  const { service } = fixture();
  service.inspect.mockResolvedValue({
    savedPools: [],
    mixedConfigurations: false,
    capacity,
  });
  render(
    <PoolAssignmentDialog
      service={service}
      controllerId="ct1"
      target={{ ...target, unassignedOnly: true }}
      onDismiss={vi.fn()}
    />,
  );
  const picker = within(await screen.findByRole("dialog", { name: "Edit pool" }));
  const emptyMessage = await picker.findByText("No saved pool configurations are available for these hashboards.");
  await waitFor(() => expect(emptyMessage).toBeVisible());
  expect(picker.getByRole("button", { name: "Assign" })).toBeDisabled();
  fireEvent.click(picker.getByRole("button", { name: "New pool configuration" }));
  expect(await screen.findByRole("dialog", { name: "Create pool configuration" })).toBeInTheDocument();
});

test("requires confirmation for a single assignment when the plan would pause container mining", async () => {
  const { service } = fixture();
  const review = service.review.getMockImplementation()!;
  service.review.mockImplementationOnce(async (selection) => ({
    ...(await review(selection)),
    requiresActivation: true,
  }));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={{ macs: [A] }} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Review assignment" }));

  await waitFor(() =>
    expect(screen.getByText("Container mining must pause to enable pool assignments.")).toBeVisible(),
  );
  expect(service.apply).not.toHaveBeenCalled();
});

test("reviews the complete username for only the selected board before saving a copy", async () => {
  const { service } = fixture();
  const review = service.review.getMockImplementation()!;
  const username = "Fixed.Account..Region.Worker";
  service.review.mockImplementationOnce(async (selection, choice) => ({
    ...(await review(selection)),
    name: choice.name,
    pools: choice.pools,
    savedPoolSet: undefined,
    usernameSource: choice.usernameSource,
    capacity: { ...capacity, groupsUsed: 3, destinationBoards: 1 },
  }));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={{ macs: [A] }} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Edit usernames" }));
  const editor = within(await screen.findByRole("dialog", { name: "Edit pool usernames" }));
  expect(editor.getByLabelText("Pool URL")).toHaveAttribute("readonly");
  expect(editor.getByLabelText("Username")).toHaveValue(pool.username);
  fireEvent.change(editor.getByLabelText("Name"), { target: { value: "Board one pools" } });
  expect(editor.getByRole("button", { name: "Review assignment" })).toBeDisabled();
  fireEvent.change(editor.getByLabelText("Username"), { target: { value: username } });
  fireEvent.change(editor.getByLabelText("Pool password (optional)"), { target: { value: "new secret" } });
  fireEvent.click(editor.getByRole("button", { name: "Review assignment" }));

  const confirmation = within(await screen.findByRole("dialog", { name: /Assign pool configuration to/ }));
  await waitFor(() => expect(confirmation.getByText("Username: " + username)).toBeVisible());
  expect(confirmation.getByText(new RegExp(A))).toBeVisible();
  expect(confirmation.queryByText(new RegExp(B))).not.toBeInTheDocument();
  expect(confirmation.getByText(/3 of 5 mining groups/)).toBeVisible();
  expect(confirmation.getByText(/provider’s connection limits/)).toBeVisible();
  expect(confirmation.queryByText("new secret")).not.toBeInTheDocument();
  expect(service.review).toHaveBeenCalledWith(
    { macs: [A] },
    expect.objectContaining({
      usernameSource: expect.objectContaining({ id: saved.id, revision: saved.revision }),
      pools: [expect.objectContaining({ username })],
    }),
    expect.anything(),
  );
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(confirmation.getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(service.apply).toHaveBeenCalledTimes(1));
});

test("reports full groups before review and allows reuse of a compatible configuration", async () => {
  const { service } = fixture();
  service.inspect.mockResolvedValue({
    savedPools: [saved, { ...saved, id: "full", name: "Full destination", projectedBoards: 5 }],
    mixedConfigurations: true,
    capacity: { ...capacity, groupsUsed: capacity.maxGroups },
  });
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Full destination" }));
  await waitFor(() => expect(screen.getByText(/configuration would have 5 hashboards/)).toBeVisible());
  expect(screen.getByText(/All mining groups are in use/)).toBeVisible();
  expect(screen.getByRole("button", { name: "New pool configuration" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Edit usernames" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Review assignment" })).toBeDisabled();
  fireEvent.click(screen.getByRole("radio", { name: "Saved pools" }));
  expect(screen.getByRole("button", { name: "Review assignment" })).toBeEnabled();
  service.inspect.mockResolvedValueOnce({ savedPools: [saved], mixedConfigurations: false, capacity });
  fireEvent.click(screen.getByRole("button", { name: "Reload configurations" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "New pool configuration" })).toBeEnabled());
  expect(service.review).not.toHaveBeenCalled();
});

test("identifies which selected hashboards will move during review", async () => {
  const { service } = fixture();
  const review = service.review.getMockImplementation()!;
  service.review.mockImplementationOnce(async (selection) => ({
    ...(await review(selection)),
    transfers: [{ source: "default", macs: [B] }],
  }));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Review assignment" }));
  await waitFor(() => expect(screen.getByText(new RegExp(A + " · Already in this group"))).toBeVisible());
  expect(screen.getByText(new RegExp(B + " · Moves to this group"))).toBeVisible();
});

test("Edit pool checks and clears a saved attempt before loading the new selection", async () => {
  savePoolAttempt("ct1", attempt);
  const { service } = fixture();
  const selection = { macs: [A] };
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={vi.fn()} />);

  expect(screen.getByRole("dialog", { name: "Edit pool" })).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole("radio", { name: "Saved pools" })).toBeVisible());
  expect(screen.queryByRole("dialog", { name: "Pool assignment status" })).not.toBeInTheDocument();
  expect(service.status).toHaveBeenCalledWith(attempt, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(service.inspect).toHaveBeenCalledWith(selection, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(service.apply).not.toHaveBeenCalled();
  expect(readPoolAttempts("ct1")).toEqual([]);
});

test("opening assignment status after reload shows the result and never repeats the write", async () => {
  savePoolAttempt("ct1", attempt);
  const { service } = fixture();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} openStatus onDismiss={vi.fn()} />);
  expect(await screen.findByText("Pool assignment saved.")).toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.review).not.toHaveBeenCalled();
  expect(service.apply).not.toHaveBeenCalled();
  expect(service.status).toHaveBeenCalledWith(attempt, expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test("ends the status check when another tab already cleared the attempt", async () => {
  const { service } = fixture();
  const dismiss = vi.fn();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} openStatus onDismiss={dismiss} />);
  await waitFor(() => expect(screen.getByText("There is no pool assignment to check.")).toBeVisible());
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(dismiss).toHaveBeenCalledOnce();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.apply).not.toHaveBeenCalled();
});

test("worker name recovery tells the operator to review unsubmitted boards again", async () => {
  const { service } = fixture();
  savePoolAttempt("ct1", { ...attempt, unsubmittedWorkerNameBoards: 2 });
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} openStatus onDismiss={vi.fn()} />);

  const recovery = await screen.findByRole("dialog", { name: "Worker name update status" });
  expect(
    within(recovery).getByText("Review the other 2 selected hashboards again after this update finishes."),
  ).toBeInTheDocument();
  expect(service.applyWorkerNames).not.toHaveBeenCalled();
});

test.each([
  { state: "partial", confirmed: 1, remainingMacs: [B], message: "One board moved." },
  { state: "unknown", confirmed: 2, message: "Board assignments match, but pool settings could not be confirmed." },
  { state: "failed", confirmed: 0, message: "A pool transfer failed." },
  {
    state: "unknown",
    confirmed: 0,
    message: "The final assignment is not confirmed. Some changes may be saved.",
    attemptOverrides: {
      destinationId: undefined,
      destinationRef: "proto-os:pools:attempt",
      configurationConfirmed: false,
      operationIds: [],
      phase: "Creating the selected pool set",
    },
  },
])(
  "Done reloads pools after a $state result with $confirmed confirmed boards",
  async ({ attemptOverrides, ...result }) => {
    savePoolAttempt("ct1", { ...attempt, ...attemptOverrides, macs: [A, B], unassignedOnly: true });
    const { service } = fixture();
    const onAssignmentsChanged = vi.fn();
    const onDismiss = vi.fn();
    service.status.mockResolvedValue({ ...result, total: 2, blockers: [] });
    render(
      <PoolAssignmentDialog
        service={service}
        controllerId="ct1"
        target={target}
        openStatus
        onAssignmentsChanged={onAssignmentsChanged}
        onDismiss={onDismiss}
      />,
    );

    const message = await screen.findByText(result.message);
    await waitFor(() => expect(message).toBeVisible());
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onAssignmentsChanged).toHaveBeenCalledOnce();
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(readPoolAttempts("ct1")).toHaveLength(0);
    expect(pushToast).not.toHaveBeenCalled();
  },
);

test("backs off failed status reads up to one minute and restores the normal interval after recovery", async () => {
  vi.useFakeTimers();
  try {
    savePoolAttempt("ct1", attempt);
    const { service } = fixture();
    service.status.mockRejectedValue(new Error("Controller unavailable"));
    render(
      <PoolAssignmentDialog service={service} controllerId="ct1" target={target} openStatus onDismiss={vi.fn()} />,
    );
    await act(() => vi.advanceTimersByTimeAsync(0));

    expect(screen.getByText("Controller unavailable")).toBeInTheDocument();
    expect(service.status).toHaveBeenCalledTimes(1);
    let calls = 1;
    for (const delay of [6_000, 12_000, 24_000, 48_000, 60_000]) {
      await act(() => vi.advanceTimersByTimeAsync(delay - 1));
      expect(service.status).toHaveBeenCalledTimes(calls);
      await act(() => vi.advanceTimersByTimeAsync(1));
      calls += 1;
      expect(service.status).toHaveBeenCalledTimes(calls);
    }

    service.status.mockResolvedValueOnce(pendingStatus).mockResolvedValueOnce({
      ...pendingStatus,
      state: "saved",
      message: "Pool assignment saved.",
      confirmed: 1,
    });
    await act(() => vi.advanceTimersByTimeAsync(59_999));
    expect(service.status).toHaveBeenCalledTimes(calls);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(screen.getByText(pendingStatus.message)).toBeInTheDocument();
    expect(screen.queryByText("Controller unavailable")).not.toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(2_999));
    expect(service.status).toHaveBeenCalledTimes(calls + 1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(screen.getByText("Pool assignment saved.")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(service.status).toHaveBeenCalledTimes(calls + 2);
    expect(service.apply).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});

test("checks status immediately during backoff and cancels the next automatic check on close", async () => {
  vi.useFakeTimers();
  try {
    savePoolAttempt("ct1", attempt);
    const { service } = fixture();
    service.status.mockRejectedValueOnce(new Error("Controller unavailable")).mockResolvedValue(pendingStatus);
    const view = render(
      <PoolAssignmentDialog service={service} controllerId="ct1" target={target} openStatus onDismiss={vi.fn()} />,
    );
    await act(() => vi.advanceTimersByTimeAsync(0));

    fireEvent.click(screen.getByRole("button", { name: "Check status" }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(service.status).toHaveBeenCalledTimes(2);
    expect(screen.getByText(pendingStatus.message)).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(3_000));
    expect(service.status).toHaveBeenCalledTimes(3);

    view.unmount();
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(service.status).toHaveBeenCalledTimes(3);
    expect(service.apply).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});

test("refreshes the configuration library after a recovered configuration save", async () => {
  savePoolAttempt("ct1", { ...attempt, configurationOnly: true, macs: [] });
  const { service } = fixture();
  const onSaved = vi.fn();
  render(
    <PoolAssignmentDialog
      service={service}
      controllerId="ct1"
      target={{ macs: [] }}
      onConfigurationSaved={onSaved}
      onDismiss={vi.fn()}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Done" }));
  expect(onSaved).toHaveBeenCalledTimes(1);
});

test("checks mining after an interrupted activation", async () => {
  savePoolAttempt("ct1", { ...attempt, restoreLegacyMining: true });
  const { service } = fixture();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Check mining" }));
  await waitFor(() => expect(service.checkMiningRecovery).toHaveBeenCalledTimes(1));
  expect(readPoolAttempts("ct1")[0].restoreLegacyMining).toBe(false);
});

test("can keep mining stopped after activation", async () => {
  const { service } = fixture();
  const onDismiss = vi.fn();
  savePoolAttempt("ct1", { ...attempt, restoreLegacyMining: true });
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={onDismiss} />);

  fireEvent.click(await screen.findByRole("button", { name: "Keep stopped" }));

  expect(service.checkMiningRecovery).not.toHaveBeenCalled();
  expect(readPoolAttempts("ct1")).toEqual([]);
  expect(onDismiss).toHaveBeenCalledOnce();
});

test("recovery records omit credentials even if a caller passes extra fields", () => {
  const extraFields = { ...attempt, password: "secret", pools: [{ ...pool, password: "hidden" }] };
  savePoolAttempt("ct1", extraFields);
  const serialized = JSON.stringify(Object.values(localStorage));
  expect(serialized).not.toContain("secret");
  expect(serialized).not.toContain("hidden");
  expect(serialized).not.toContain("password");
  expect(readPoolAttempts("another-controller")).toEqual([]);
});

test("redacts private pool URLs in new and existing recovery records", () => {
  const url = "stratum+tcp://private-user:private-pass@pool.example:3333/path?token=private-token";
  const record = { ...attempt, pools: [{ ...pool, url }] };
  const key = "proto-os:pool-assignment:ct1:" + record.id;
  const expectRedaction = () => {
    const serialized = localStorage.getItem(key);
    expect(serialized).not.toContain("private");
    expect(JSON.parse(serialized!)).toMatchObject({
      poolFingerprint: expect.stringMatching(/^[0-9a-f]{64}$/),
      pools: [{ ...pool, url: "stratum+tcp://pool.example:3333" }],
    });
  };
  savePoolAttempt("ct1", record);
  expectRedaction();
  localStorage.setItem(key, JSON.stringify(record));
  expect(readPoolAttempts("ct1")).toHaveLength(1);
  expectRedaction();
});

test("serializes controller writes across separate dialogs", async () => {
  const { service } = fixture();
  let rejectFirstWrite: (reason: Error) => void;
  service.apply.mockImplementationOnce(
    () =>
      new Promise<never>((_, reject) => {
        rejectFirstWrite = reject;
      }),
  );
  const props = { service, controllerId: "ct1", target: { macs: [A] }, onDismiss: vi.fn() };
  render(
    <>
      <PoolAssignmentDialog {...props} />
      <PoolAssignmentDialog {...props} />
    </>,
  );
  const [first, second] = await screen.findAllByRole("dialog", { name: "Edit pool" });
  for (const dialog of [first, second]) {
    fireEvent.click(await within(dialog).findByRole("radio", { name: "Saved pools" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Review assignment" }));
  }
  await waitFor(() => expect(service.review).toHaveBeenCalledTimes(2));
  const confirmations = await waitFor(() => {
    const dialogs = screen.getAllByRole("dialog", { name: "Assign pool configuration to 1 hashboard?" });
    expect(dialogs).toHaveLength(2);
    return dialogs;
  });
  for (const confirmation of confirmations)
    fireEvent.click(within(confirmation).getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(service.apply).toHaveBeenCalledTimes(1));
  expect(service.apply).toHaveBeenCalledTimes(1);
  rejectFirstWrite!(new Error("No changes were saved"));
  await waitFor(() => expect(service.apply).toHaveBeenCalledTimes(2));
});

test("failed inspection offers retry without a save action", async () => {
  const { service } = fixture();
  service.inspect.mockRejectedValueOnce(new Error("Controller unavailable"));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  expect(await screen.findByText("Controller unavailable")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Review assignment" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByRole("radio", { name: "Saved pools" })).toBeInTheDocument();
  expect(service.apply).not.toHaveBeenCalled();
});

test("pending transfers show individual blockers and keep their recovery record", async () => {
  savePoolAttempt("ct1", attempt);
  const { service } = fixture();
  service.status.mockResolvedValue({
    state: "pending",
    message: "Waiting for board stop",
    confirmed: 0,
    total: 1,
    blockers: [{ mac: B, reason: "Previous owner unavailable" }],
  });
  const view = render(
    <PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />,
  );
  expect(await screen.findByText(`${B}: Previous owner unavailable`)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Done" })).not.toBeInTheDocument();
  view.unmount();
  expect(readPoolAttempts("ct1")).toHaveLength(1);
  await waitFor(() => expect(service.apply).not.toHaveBeenCalled());
});

test("new configurations use Fleet's primary and backup form before returning to the picker", async () => {
  const { service } = fixture();
  service.review.mockImplementation(async (current, choice) => ({
    id: "create",
    name: choice.name,
    configurationOnly: true,
    target: current,
    boards: boards.filter((board) => current.macs.includes(board.mac)),
    stamp: "review",
    pools: choice.pools,
    capacity: { ...capacity, destinationBoards: 0 },
    mining: true,
    requiresActivation: false,
    destinationRef: "proto-os:pools:create",
    transfers: [],
  }));
  service.apply.mockImplementation(
    async (plan: PoolAssignmentPlan, track: (attempt: PoolAssignmentAttempt) => void) => {
      const created = {
        ...saved,
        id: "created",
        name: plan.name,
        pools: plan.pools.map((pool, index) => ({ ...pool, id: String(index + 1), enabled: true, priority: index })),
      };
      track({ ...attempt, id: plan.id, configurationOnly: true, destinationId: "created", pools: created.pools });
      service.inspect.mockResolvedValue({
        savedPools: [saved, created],
        mixedConfigurations: false,
        capacity,
      });
      return { state: "saved", message: "Pool configuration saved.", confirmed: 0, total: 0, blockers: [] };
    },
  );
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "New pool configuration" }));
  const editor = await screen.findByRole("dialog", { name: "Create pool configuration" });
  fireEvent.change(within(editor).getByLabelText("Name"), { target: { value: "New named pools" } });
  const primary = within(editor).getByRole("group", { name: "Primary pool" });
  fireEvent.change(within(primary).getByLabelText("Pool URL"), { target: { value: "stratum+tcp://primary:3333" } });
  fireEvent.change(within(primary).getByLabelText("Username"), { target: { value: "full.account.worker" } });
  fireEvent.click(within(editor).getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByRole("radio", { name: "New named pools" })).toBeChecked();
  expect(service.apply).toHaveBeenCalledWith(
    expect.objectContaining({ configurationOnly: true, name: "New named pools" }),
    expect.any(Function),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(readPoolAttempts("ct1")).toEqual([]);
});

test("recovers standalone configuration writes with no selected equipment", () => {
  savePoolAttempt("ct1", { ...attempt, name: "Production", configurationOnly: true, macs: [] });
  expect(readPoolAttempts("ct1")[0]).toMatchObject({ name: "Production", configurationOnly: true, macs: [] });
});

test.each([false, true])("reviews edits only if hashboards use the configuration (%s)", async (assigned) => {
  const { service } = fixture();
  service.inspect.mockRejectedValue(new Error("Unrelated configuration changed"));
  service.review.mockImplementation(async (_current, choice) => ({
    id: "edit",
    name: choice.name,
    configurationOnly: true,
    editingRevision: saved.revision,
    target: assigned ? target : { macs: [] },
    boards: assigned ? boards : [],
    stamp: "review",
    pools: choice.pools,
    capacity: { ...capacity, destinationBoards: 0 },
    mining: true,
    requiresActivation: false,
    destinationId: saved.id,
    destinationRef: "proto-os:pools:saved",
    transfers: [],
  }));
  service.apply.mockImplementation(async (plan, track) => {
    track({ ...attempt, id: plan.id, configurationOnly: true });
    return { state: "saved", message: "Pool configuration saved.", confirmed: 0, total: 0, blockers: [] };
  });
  const onSaved = vi.fn();
  render(
    <PoolAssignmentDialog
      service={service}
      controllerId="ct1"
      target={{ macs: [] }}
      configuration={{
        ...saved,
        assignedCount: assigned ? boards.length : 0,
        assignedMacs: assigned ? boards.map(({ mac }) => mac) : [],
        deletable: true,
        isDefault: false,
      }}
      configurationCapacity={capacity}
      onConfigurationSaved={onSaved}
      onDismiss={vi.fn()}
    />,
  );
  const editor = await screen.findByRole("dialog", { name: "Edit pool configuration" });
  fireEvent.change(within(editor).getByLabelText("Name"), { target: { value: "Edited draft" } });
  fireEvent.click(within(editor).getByRole("button", { name: assigned ? "Review changes" : "Save configuration" }));
  if (assigned) {
    const review = await screen.findByRole("dialog", { name: "Review pool configuration changes" });
    await waitFor(() => expect(within(review).getByText(`Tank 1 · Module 1 · Board 1 · ${A}`)).toBeVisible());
    expect(within(review).getByText("Edited draft", { exact: false })).toBeVisible();
    expect(service.apply).not.toHaveBeenCalled();
    fireEvent.click(within(review).getByRole("button", { name: "Cancel" }));
    const restored = await screen.findByRole("dialog", { name: "Edit pool configuration" });
    expect(within(restored).getByLabelText("Name")).toHaveValue("Edited draft");
    fireEvent.click(within(restored).getByRole("button", { name: "Review changes" }));
    fireEvent.click(await screen.findByRole("button", { name: "Save and update equipment" }));
  }
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(screen.queryByText("Unrelated configuration changed")).not.toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.review).toHaveBeenCalledWith(
    { macs: [] },
    expect.objectContaining({ editPoolSetId: saved.id, expectedRevision: saved.revision }),
    expect.anything(),
  );
  expect(service.apply).toHaveBeenCalledTimes(1);
});
