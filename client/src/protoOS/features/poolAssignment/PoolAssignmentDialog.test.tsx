import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { readPoolAttempts, savePoolAttempt } from "./attemptStorage";
import PoolAssignmentDialog from "./PoolAssignmentDialog";
import type {
  PoolAssignmentAttempt,
  PoolAssignmentPlan,
  PoolAssignmentService,
} from "@/protoOS/api/adapters/poolAssignment";

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
const fixture = () => {
  const service = {
    listConfigurations: vi.fn().mockResolvedValue([]),
    listConfigurationPage: vi.fn().mockResolvedValue({ configurations: [], total: 0 }),
    deleteConfiguration: vi.fn().mockResolvedValue(undefined),
    inspect: vi.fn().mockResolvedValue({
      boards,
      savedPools: [saved],
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
          message: "Pool settings and assignments are saved.",
          confirmed: 1,
          total: 1,
          blockers: [],
        };
      }),
    status: vi.fn().mockResolvedValue({
      state: "saved",
      message: "Pool settings and assignments are saved.",
      confirmed: 1,
      total: 1,
      blockers: [],
    }),
    restoreMining: vi.fn().mockImplementation(async (current, track) => {
      track({ ...current, restoreLegacyMining: false });
      return { state: "unknown", message: "Check saved changes.", confirmed: 0, total: 0, blockers: [] };
    }),
  } satisfies PoolAssignmentService;
  return { service };
};
beforeEach(() => {
  localStorage.clear();
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
  expect(await screen.findByText("Pool settings and assignments are saved.")).toBeInTheDocument();
  expect(readPoolAttempts("ct1")).toEqual([{ ...attempt, id: "next-attempt" }]);
});

test("uses Fleet's configuration picker and confirms the captured equipment selection", async () => {
  const { service } = fixture();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  expect(await screen.findByRole("dialog", { name: "Edit pool" })).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Your password")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Assign" })).toBeDisabled();
  fireEvent.click(await screen.findByRole("radio", { name: "Saved pools" }));
  fireEvent.click(screen.getByRole("button", { name: "Assign" }));
  const confirmation = await screen.findByRole("dialog", { name: "Assign pool configuration to 2 hashboards?" });
  expect(service.review).toHaveBeenCalledWith(
    { macs: [A, B] },
    { name: "Saved pools", pools: [], savedPoolSetId: "saved" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  expect(service.apply).not.toHaveBeenCalled();
  fireEvent.click(within(confirmation).getByRole("button", { name: "Assign" }));
  expect(await screen.findByText("Pool settings and assignments are saved.")).toBeInTheDocument();
  expect(service.apply).toHaveBeenCalledTimes(1);
});

test("reload recovers status and never repeats the write", async () => {
  savePoolAttempt("ct1", attempt);
  const { service } = fixture();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  expect(await screen.findByText("Pool settings and assignments are saved.")).toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(service.review).not.toHaveBeenCalled();
  expect(service.apply).not.toHaveBeenCalled();
  expect(service.status).toHaveBeenCalledWith(attempt, expect.objectContaining({ signal: expect.any(AbortSignal) }));
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

test("offers to restore mining after an interrupted activation", async () => {
  savePoolAttempt("ct1", { ...attempt, restoreLegacyMining: true });
  const { service } = fixture();
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Start mining" }));
  await waitFor(() => expect(service.restoreMining).toHaveBeenCalledTimes(1));
  expect(readPoolAttempts("ct1")[0].restoreLegacyMining).toBe(false);
});

test("can keep mining stopped after activation", async () => {
  const { service } = fixture();
  const onDismiss = vi.fn();
  savePoolAttempt("ct1", { ...attempt, restoreLegacyMining: true });
  render(<PoolAssignmentDialog service={service} controllerId="ct1" target={target} onDismiss={onDismiss} />);

  fireEvent.click(await screen.findByRole("button", { name: "Keep stopped" }));

  expect(service.restoreMining).not.toHaveBeenCalled();
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

test("saves an assigned configuration from the editor without another review screen", async () => {
  const { service } = fixture();
  service.inspect.mockRejectedValue(new Error("Unrelated configuration changed"));
  service.review.mockImplementation(async (_current, choice) => ({
    id: "edit",
    name: choice.name,
    configurationOnly: true,
    editingRevision: saved.revision,
    target,
    boards,
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
      configuration={saved}
      onConfigurationSaved={onSaved}
      onDismiss={vi.fn()}
    />,
  );
  const editor = await screen.findByRole("dialog", { name: "Edit pool configuration" });
  fireEvent.change(within(editor).getByLabelText("Name"), { target: { value: "Edited draft" } });
  fireEvent.click(within(editor).getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(screen.queryByText("Unrelated configuration changed")).not.toBeInTheDocument();
  expect(service.inspect).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog", { name: "Review pool configuration changes" })).not.toBeInTheDocument();
  expect(service.review).toHaveBeenCalledWith(
    { macs: [] },
    expect.objectContaining({ editPoolSetId: saved.id, expectedRevision: saved.revision }),
    expect.anything(),
  );
  expect(service.apply).toHaveBeenCalledTimes(1);
});
