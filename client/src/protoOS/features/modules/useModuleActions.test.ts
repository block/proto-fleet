import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { buildModuleRows } from "./moduleListModel";
import { useModuleActions } from "./useModuleActions";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ command: vi.fn(), telemetry: vi.fn() }));
const adapter = { runHashboardAction: mocks.command, getCurrentTelemetry: mocks.telemetry };
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter }) }));
vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry:
    () =>
    async ({
      request,
      onSuccess,
      onError,
      signal,
    }: {
      request: (params: unknown) => Promise<unknown>;
      onSuccess: (result: unknown) => Promise<void>;
      onError: () => void;
      signal: AbortSignal;
    }) => {
      try {
        await onSuccess(await request({ signal }));
      } catch {
        onError();
      }
    },
}));
let fixture: ReturnType<typeof tanksFixture>;
beforeEach(() => {
  vi.clearAllMocks();
  fixture = tanksFixture(2, 2);
  const state = useMinerStore.getState();
  state.resetDeviceData();
  state.hardware.replaceHardware(fixture.hardware);
  state.telemetry.mergeTelemetry(fixture.telemetry);
  mocks.command.mockResolvedValue({ accepted: 3, failures: [] });
  mocks.telemetry.mockResolvedValue(fixture.telemetry);
});
const setup = () => {
  const rows = buildModuleRows(fixture.topology, fixture.telemetry);
  return { rows, ...renderHook(() => useModuleActions(rows, fixture.topology)) };
};
afterEach(() => vi.useRealTimers());

test("a stalled command times out, releases the dialog, and ignores a late result", async () => {
  vi.useFakeTimers();
  let resolve!: (value: unknown) => void;
  mocks.command.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const { rows, result } = setup();
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let command!: Promise<void>;
  act(() => {
    command = result.current.confirm();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(30_000);
  });
  expect(result.current.pending).toBe(false);
  expect(result.current.outcome?.error).toContain("timed out");
  expect(result.current.outcome?.error).toContain("Check module status");
  expect(mocks.command.mock.calls[0][2].signal.aborted).toBe(true);
  await command;
  await act(async () => {
    resolve({ accepted: 3, failures: [] });
  });
  expect(result.current.outcome?.result).toBeUndefined();
  expect(mocks.telemetry).not.toHaveBeenCalled();
  expect(mocks.command).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
  act(() => result.current.dismiss());
  expect(result.current.request).toBeUndefined();
});

test("a stalled refresh preserves accepted results, releases the dialog, and ignores late telemetry", async () => {
  vi.useFakeTimers();
  let resolve!: (value: unknown) => void;
  mocks.telemetry.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const { rows, result } = setup();
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let command!: Promise<void>;
  await act(async () => {
    command = result.current.confirm();
  });
  expect(result.current.outcome?.result?.accepted).toBe(3);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15_000);
  });
  expect(result.current.pending).toBe(false);
  expect(result.current.outcome).toMatchObject({ result: { accepted: 3 }, refreshFailed: true });
  expect(mocks.telemetry.mock.calls[0][1].signal.aborted).toBe(true);
  await command;
  const telemetry = useMinerStore.getState().telemetry;
  await act(async () => {
    resolve(tanksFixture(1, 1).telemetry);
  });
  expect(useMinerStore.getState().telemetry).toBe(telemetry);
  expect(mocks.command).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
});

test("unmount cancels a stalled command and clears its deadline without waiting for transport", async () => {
  vi.useFakeTimers();
  mocks.command.mockImplementation(() => new Promise(() => {}));
  const { rows, result, unmount } = setup();
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let settled = false;
  act(() => {
    void result.current.confirm().then(() => {
      settled = true;
    });
  });
  unmount();
  await act(async () => {});
  expect(settled).toBe(true);
  expect(mocks.command.mock.calls[0][2].signal.aborted).toBe(true);
  expect(vi.getTimerCount()).toBe(0);
  expect(mocks.telemetry).not.toHaveBeenCalled();
});

test("confirmation preserves the exact row target and reports partial results", async () => {
  const { rows, result } = setup();
  act(() =>
    result.current
      .actionsFor([rows[0].id])
      .find((a) => a.testId === "module-action-stop")!
      .onClick(),
  );
  mocks.command.mockResolvedValue({ accepted: 2, failures: [{ identifier: "board", message: "disconnected" }] });
  await act(() => result.current.confirm());
  expect(mocks.command).toHaveBeenCalledExactlyOnceWith(
    "stop",
    rows[0].module.hashboardIds.map((id) => fixture.topology.hashboards.get(id)!.identity.value).sort(),
    expect.anything(),
  );
  expect(result.current.outcome?.result?.accepted).toBe(2);
  expect(result.current.outcome?.result?.failures).toHaveLength(1);
});

test("changed topology rejects a stale confirmation", async () => {
  const { rows, result } = setup();
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  fixture.topology.hashboards.delete(rows[0].module.hashboardIds[0]);
  act(() => useMinerStore.getState().hardware.replaceHardware(fixture.hardware));
  await act(() => result.current.confirm());
  expect(mocks.command).not.toHaveBeenCalled();
  expect(result.current.outcome?.error).toContain("changed");
});

test("start and stop are disabled when a selected module does not support start-stop commands", () => {
  fixture = tanksFixture(2, 2);
  const module = [...fixture.topology.modules.values()][0];
  module.capabilities = {
    commands: { ...module.capabilities.commands, startStop: "unsupported" },
  };
  act(() => useMinerStore.getState().hardware.replaceHardware(fixture.hardware));
  const { rows, result } = setup();

  const actions = result.current.actionsFor([rows[0].id]);

  expect(actions.find((action) => action.testId === "module-action-start")?.disabled).toBe(true);
  expect(actions.find((action) => action.testId === "module-action-stop")?.disabled).toBe(true);
  expect(actions.find((action) => action.testId === "module-action-identify")?.disabled).toBe(false);
});

test("capability changes after confirmation opens prevent the command", async () => {
  const { rows, result } = setup();
  act(() =>
    result.current
      .actionsFor([rows[0].id])
      .find((action) => action.testId === "module-action-start")!
      .onClick(),
  );
  const updated = tanksFixture(2, 2);
  const updatedModule = updated.topology.modules.get(rows[0].module.id)!;
  updatedModule.capabilities = {
    commands: { ...updatedModule.capabilities.commands, startStop: "unsupported" },
  };
  act(() => useMinerStore.getState().hardware.replaceHardware(updated.hardware));

  await act(() => result.current.confirm());

  expect(mocks.command).not.toHaveBeenCalled();
  expect(result.current.outcome?.error).toContain("changed");
});

test("duplicate confirmation cannot replay a pending command; failed refresh does not replay it", async () => {
  const { rows, result } = setup();
  let resolve!: (value: unknown) => void;
  mocks.command.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  mocks.telemetry.mockRejectedValue(new Error("refresh failed"));
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.confirm();
  });
  await act(() => result.current.confirm());
  await act(async () => {
    resolve({ accepted: 3, failures: [] });
    await pending;
  });
  expect(mocks.command).toHaveBeenCalledTimes(1);
  expect(result.current.outcome).toMatchObject({ result: { accepted: 3 }, refreshFailed: true });
});

test("device changes abort pending work and discard its result", async () => {
  const { rows, result } = setup();
  let resolve!: (value: unknown) => void;
  mocks.command.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let pending!: Promise<void>;
  act(() => {
    pending = result.current.confirm();
  });
  act(() => useMinerStore.getState().resetDeviceData());
  await act(async () => {
    resolve({ accepted: 3, failures: [] });
    await pending;
  });
  expect(mocks.command.mock.calls[0][2].signal.aborted).toBe(true);
  expect(mocks.telemetry).not.toHaveBeenCalled();
  expect(result.current.request).toBeUndefined();
});
