import { act, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { parseDraft, toDraft, toRunInRequest, validateDefaults } from "./defaults";
import ModuleRunIn from "./ModuleRunIn";
import { useRunInDefaults } from "./useRunInDefaults";
import ContainerRunInDefaults from "@/protoOS/features/settings/components/General/ContainerRunInDefaults";

const state = vi.hoisted(() => ({ deviceId: "ct1-a", startModuleRunIn: vi.fn() }));
vi.mock("@/protoOS/store", () => ({ useSystemInfo: () => ({ identity: { deviceId: state.deviceId } }) }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: { startModuleRunIn: state.startModuleRunIn } }),
}));
const defaults = {
  durationMinutes: 30,
  sampleIntervalSeconds: 10,
  cooldownMinutes: 2,
  boardPowerTargetWatts: 1200,
  stopMiningOnFinish: true,
};

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  state.deviceId = "ct1-a";
  state.startModuleRunIn.mockReset().mockResolvedValue({ taskId: "task-1" });
});

test("defaults reject missing, nonfinite, fractional and out-of-range values", () => {
  expect(validateDefaults(defaults)).toBe(true);
  for (const invalid of [
    null,
    {},
    { ...defaults, durationMinutes: 0 },
    { ...defaults, cooldownMinutes: -1 },
    { ...defaults, boardPowerTargetWatts: Infinity },
    { ...defaults, sampleIntervalSeconds: 1801 },
    { ...defaults, durationMinutes: 0.5 },
    { ...defaults, boardPowerTargetWatts: 2 ** 32 },
  ]) {
    expect(validateDefaults(invalid)).toBe(false);
  }
  expect(() => parseDraft(toDraft(null))).toThrow();
  expect(() => parseDraft({ ...toDraft(defaults), cooldownMinutes: " " })).toThrow();
  expect(toRunInRequest(defaults)).toEqual({
    duration: "1800s",
    sampleInterval: "10s",
    cooldownDuration: "120s",
    boardPowerTargetWatts: 1200,
    stopMiningOnFinish: true,
  });
});

test("saved defaults synchronize consumers, survive remount and stay scoped to the container", () => {
  const first = renderHook(useRunInDefaults);
  const second = renderHook(useRunInDefaults);
  act(() => first.result.current.save(defaults));
  expect(second.result.current.defaults).toEqual(defaults);
  first.unmount();
  const reopened = renderHook(useRunInDefaults);
  expect(reopened.result.current.defaults).toEqual(defaults);
  state.deviceId = "ct1-b";
  reopened.rerender();
  expect(reopened.result.current.defaults).toBeNull();
  state.deviceId = "ct1-a";
  reopened.rerender();
  expect(reopened.result.current.defaults).toEqual(defaults);
  act(() => {
    localStorage.setItem("proto-os:run-in-defaults:v1:ct1-a", "broken JSON");
    window.dispatchEvent(new Event("storage"));
  });
  expect(reopened.result.current.defaults).toBeNull();
});

test("settings show dashes until saved and report storage failures without closing", () => {
  render(<ContainerRunInDefaults />);
  expect(screen.getAllByText("—")).toHaveLength(5);
  fireEvent.click(screen.getByRole("button", { name: "Edit run-in defaults" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.getByRole("alert")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Duration (minutes)"), { target: { value: "30" } });
  fireEvent.change(screen.getByLabelText("Power target per hashboard (W)"), { target: { value: "1200" } });
  const storage = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("quota");
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.getByRole("alert")).toHaveTextContent("Could not save defaults in this browser.");
  storage.mockRestore();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByText("1200 W")).toBeInTheDocument();
});

test("launch prefills saved defaults, blocks duplicate submissions and retries with the same task ID", async () => {
  const hook = renderHook(useRunInDefaults);
  act(() => hook.result.current.save(defaults));
  let reject!: (reason: Error) => void;
  state.startModuleRunIn.mockImplementationOnce(
    () =>
      new Promise((_, r) => {
        reject = r;
      }),
  );
  render(<ModuleRunIn moduleId="container:ct1-a:module:T1-M02" label="Tank 1 · Module 2" />);
  fireEvent.click(screen.getByRole("button", { name: "Start run-in" }));
  expect(screen.getByLabelText("Duration (minutes)")).toHaveValue("30");
  expect(screen.getByLabelText("Power target per hashboard (W)")).toHaveValue("1200");
  const dialog = within(screen.getByRole("dialog", { name: "Module run-in" }));
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  expect(state.startModuleRunIn).toHaveBeenCalledTimes(1);
  await act(async () => reject(new Error("response lost")));
  expect(screen.getByRole("alert")).toHaveTextContent("Try again");
  expect(screen.getByLabelText("Power target per hashboard (W)")).toBeDisabled();
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  await waitFor(() => expect(screen.getByText("Task task-1")).toBeInTheDocument());
  expect(state.startModuleRunIn).toHaveBeenCalledTimes(2);
  expect(state.startModuleRunIn.mock.calls[0]).toEqual(state.startModuleRunIn.mock.calls[1]);
  expect(state.startModuleRunIn.mock.calls[0]).toEqual([
    "container:ct1-a:module:T1-M02",
    { ...toRunInRequest(defaults), taskId: expect.any(String) },
  ]);
  expect(state.startModuleRunIn.mock.calls[0][1].taskId).toMatch(/^[0-9a-f]{32}$/);
});

test("a definite validation rejection allows correction and submission with a new task ID", async () => {
  const hook = renderHook(useRunInDefaults);
  act(() => hook.result.current.save(defaults));
  state.startModuleRunIn.mockRejectedValueOnce({
    status: 400,
    error: { code: 3, message: "Power target is outside board limits" },
  });
  render(<ModuleRunIn moduleId="container:ct1-a:module:T1-M02" label="Module 2" />);
  fireEvent.click(screen.getByRole("button", { name: "Start run-in" }));
  const dialog = within(screen.getByRole("dialog"));
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Power target is outside board limits");
  const input = screen.getByLabelText("Power target per hashboard (W)");
  expect(input).toBeEnabled();
  fireEvent.change(input, { target: { value: "1000" } });
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  expect(await screen.findByText("Task task-1")).toBeInTheDocument();
  const [first, second] = state.startModuleRunIn.mock.calls;
  expect(second[1].boardPowerTargetWatts).toBe(1000);
  expect(second[1].taskId).not.toBe(first[1].taskId);
});

test("a later rejection does not discard a task ID after an uncertain response", async () => {
  const hook = renderHook(useRunInDefaults);
  act(() => hook.result.current.save(defaults));
  state.startModuleRunIn
    .mockRejectedValueOnce({ status: 502 })
    .mockRejectedValueOnce({ status: 400, error: { code: 3, message: "Invalid settings" } });
  render(<ModuleRunIn moduleId="container:ct1-a:module:T1-M02" label="Module 2" />);
  fireEvent.click(screen.getByRole("button", { name: "Start run-in" }));
  const dialog = within(screen.getByRole("dialog"));
  for (let attempt = 0; attempt < 2; attempt++) {
    fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Try again to check the same request");
    expect(screen.getByLabelText("Power target per hashboard (W)")).toBeDisabled();
  }
  fireEvent.click(dialog.getByRole("button", { name: "Start run-in" }));
  expect(await screen.findByText("Task task-1")).toBeInTheDocument();
  const [first, second, third] = state.startModuleRunIn.mock.calls;
  expect(second).toEqual(first);
  expect(third).toEqual(first);
});
