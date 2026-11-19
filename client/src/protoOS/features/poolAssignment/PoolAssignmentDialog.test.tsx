import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { readPoolAttempts, savePoolAttempt } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import type {
  PoolAssignmentAttempt,
  PoolAssignmentPlan,
  PoolAssignmentService,
} from "@/protoOS/api/adapters/poolAssignment";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/features/toaster")>()),
  pushToast: vi.fn(),
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
const saved = { id: "saved", name: "Saved pools", revision: "2", mining: true, pools: [pool] };
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
    listConfigurationPage: vi.fn().mockResolvedValue({ configurations: [], total: 0 }),
    deleteConfiguration: vi.fn().mockResolvedValue(undefined),
    inspect: vi.fn().mockResolvedValue({
      boards,
      savedPools: [saved],
      mixedConfigurations: false,
      maxPools: 3,
      requiresActivation: false,
    }),
    review: vi.fn().mockImplementation(async (current) => ({
      id: "next-attempt",
      name: "Saved pools",
      target: current,
      boards: boards.filter((board) => current.macs.includes(board.mac)),
      stamp: "review",
      pools: [],
      savedPoolSet: saved,
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
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
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

test("keeps Fleet's short confirmation for multiple selected boards in one module", async () => {
  const { service } = fixture();
  service.inspect.mockResolvedValue({
    boards,
    savedPools: [saved],
    mixedConfigurations: true,
    maxPools: 3,
    requiresActivation: false,
  });
  const dismiss = vi.fn();
  const selection = { ...target, modules: [[A, B]] };
  render(
    <PoolAssignmentDialog
      service={service}
      controllerId="ct1"
      target={selection}
      selectedCount={2}
      onDismiss={dismiss}
    />,
  );
  const picker = within(await screen.findByRole("dialog", { name: "Edit pool" }));
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Your password")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
  const savedPool = await picker.findByRole("radio", { name: "Saved pools" });
  expect(picker.getByText("Mixed configurations")).toBeVisible();
  expect(picker.getAllByRole("columnheader").map((header) => header.textContent)).toEqual([
    "Name",
    "Primary pool",
    "Backup pool",
  ]);
  fireEvent.click(savedPool);
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));
  const confirmation = await screen.findByRole("dialog", { name: "Assign pool configuration to 2 hashboards?" });
  expect(
    within(confirmation).getByText("Assign Saved pools? Pool changes may briefly interrupt mining."),
  ).toBeVisible();
  expect(within(confirmation).queryByText(pool.url)).not.toBeInTheDocument();
  expect(within(confirmation).queryByText(/Affected hashboards/)).not.toBeInTheDocument();
  expect(service.review).toHaveBeenCalledWith(
    selection,
    { name: "Saved pools", pools: [], savedPoolSetId: "saved" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(within(confirmation).getByRole("button", { name: "Assign" }));
  await waitFor(() => expect(dismiss).toHaveBeenCalledOnce());
  expect(service.apply).toHaveBeenCalledTimes(1);
});

test.each([
  { label: "one hashboard", selection: { macs: [A] } },
  { label: "one module with multiple hashboards", selection: { macs: [A, B], modules: [[A, B]] } },
])("assigns $label after the first Assign click", async ({ selection }) => {
  const { service } = fixture();
  const dismiss = vi.fn();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={dismiss} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));

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

test("requires confirmation for a single assignment when the plan would pause container mining", async () => {
  const { service } = fixture();
  const review = service.review.getMockImplementation()!;
  service.review.mockImplementationOnce(async (selection) => ({
    ...(await review(selection)),
    requiresActivation: true,
  }));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={{ macs: [A] }} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));

  expect(await screen.findByText("Container mining must pause to enable pool assignments.")).toBeVisible();
  expect(service.apply).not.toHaveBeenCalled();
});

test("Edit pool checks and clears a saved attempt before loading the new selection", async () => {
  savePoolAttempt("ct1", attempt);
  const { service } = fixture();
  const selection = { macs: [A] };
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={selection} onDismiss={vi.fn()} />);

  expect(screen.getByRole("dialog", { name: "Edit pool" })).toBeInTheDocument();
  expect(await screen.findByRole("radio", { name: "Saved pools" })).toBeVisible();
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
    fireEvent.click(within(dialog).getByRole("button", { name: "Assign" }));
  }
  await waitFor(() => expect(service.review).toHaveBeenCalledTimes(2));
  expect(service.apply).toHaveBeenCalledTimes(1);
  rejectFirstWrite!(new Error("No changes were saved"));
  await waitFor(() => expect(service.apply).toHaveBeenCalledTimes(2));
});

test("failed inspection offers retry without a save action", async () => {
  const { service } = fixture();
  service.inspect.mockRejectedValueOnce(new Error("Controller unavailable"));
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  expect(await screen.findByText("Controller unavailable")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
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
        boards: [{ mac: A }, { mac: B }],
        savedPools: [saved, created],
        mixedConfigurations: false,
        maxPools: 3,
        requiresActivation: false,
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
      configuration={{ ...saved, assignedCount: assigned ? boards.length : 0, deletable: true, isDefault: false }}
      onConfigurationSaved={onSaved}
      onDismiss={vi.fn()}
    />,
  );
  const editor = await screen.findByRole("dialog", { name: "Edit pool configuration" });
  fireEvent.change(within(editor).getByLabelText("Name"), { target: { value: "Edited draft" } });
  fireEvent.click(within(editor).getByRole("button", { name: assigned ? "Review changes" : "Save configuration" }));
  if (assigned) {
    const review = await screen.findByRole("dialog", { name: "Review pool configuration changes" });
    expect(within(review).getByText(`Tank 1 · Module 1 · Board 1 · ${A}`)).toBeVisible();
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
