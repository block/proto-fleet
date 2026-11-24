import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import ModuleVoltageSection, { VOLTAGE_WRITE_TIMEOUT_MS } from "./ModuleVoltageSection";
import { MODULE_VOLTAGE_OUTCOME_UNKNOWN, type ModuleVoltageSetting } from "@/protoOS/api/adapters/moduleVoltage";
import type { ModuleNode } from "@/protoOS/store/contract";

const mocks = vi.hoisted(() => ({
  pushToast: vi.fn(),
  isFleetHosted: false,
  /** Whether the token refresh that follows a 401 succeeds. */
  refreshSucceeds: false,
  adapter: {
    listModuleVoltageSettings: vi.fn(),
    setModuleFixedVoltage: vi.fn(),
    clearModuleFixedVoltage: vi.fn(),
  },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.isFleetHosted }),
}));
vi.mock("@/shared/features/toaster", () => ({ pushToast: mocks.pushToast }));
// The real useAuthRetry runs; only the token refresh is stubbed. As in useAuthErrors, only a 401 is refreshed.
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: ({
      error,
      onError,
      onSuccess,
    }: {
      error: { status?: number };
      onError?: (error: unknown) => void;
      onSuccess?: () => void;
    }) => (error?.status === 401 && mocks.refreshSucceeds ? onSuccess?.() : onError?.(error)),
  }),
}));

const WIRE_ID = "0011223344556677";
const module = (id = `container:ct1:module:${WIRE_ID}`) =>
  ({ id, hashboardIds: [], psuIds: [], controlBoardIds: [], fanIds: [] }) as unknown as ModuleNode;
const range = { minimumMillivolts: 26_000, maximumMillivolts: 30_000 };
const setting = (overrides: Partial<ModuleVoltageSetting> = {}): ModuleVoltageSetting => ({
  moduleId: WIRE_ID,
  supportedRange: range,
  boardCount: 3,
  readiness: "ready",
  ...overrides,
});
const applied = (count: number) =>
  Array.from({ length: count }, (_, index) => ({ macAddress: `aa:bb:cc:00:00:0${index + 1}`, applied: true }));

const section = () => screen.getByRole("heading", { name: "Voltage" }).closest("section")!;

const openFixedDialog = async () => {
  fireEvent.click(await screen.findByRole("button", { name: /fixed voltage/ }));
  return screen.findByRole("dialog", { name: "Set a fixed module voltage?" });
};
const enterVoltage = (dialog: HTMLElement, value: string) =>
  fireEvent.change(within(dialog).getByLabelText("Fixed voltage"), { target: { value } });

/** A confirmed change is announced as a success toast rather than in the section. */
const expectSuccessToast = (message: string) =>
  waitFor(() => expect(mocks.pushToast).toHaveBeenCalledWith(expect.objectContaining({ message, status: "success" })));

const setVoltage = async (value = "28000") => {
  const dialog = await openFixedDialog();
  enterVoltage(dialog, value);
  fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));
};
const unauthenticated = { status: 401, error: { message: "token expired" } };

beforeEach(() => {
  mocks.isFleetHosted = false;
  mocks.refreshSucceeds = false;
  mocks.pushToast.mockReset();
  Object.values(mocks.adapter).forEach((fn) => fn.mockReset());
});
afterEach(() => {
  vi.useRealTimers();
});

describe("ModuleVoltageSection", () => {
  test("shows the saved mode and range before offering a change", async () => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([
      setting({ moduleId: "ffffffffffffffff", fixedMillivolts: 27_000 }),
      setting({ updatedBy: "proto-os", updatedAt: new Date("2026-11-16T10:00:00Z") }),
    ]);
    render(<ModuleVoltageSection module={module()} />);
    expect(await within(section()).findByText("Automatic")).toBeInTheDocument();
    expect(within(section()).getByText("26,000–30,000 mV")).toBeInTheDocument();
    expect(within(section()).getByText(/by proto-os/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Set fixed voltage" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Use automatic voltage" })).not.toBeInTheDocument();
  });

  test("confirms, sends the module identity once and confirms by readback", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);

    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    await expectSuccessToast("Fixed voltage of 28,000 mV saved and applied to all 3 hashboards.");
    expect(within(section()).getByText("Fixed at 28,000 mV")).toBeInTheDocument();
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledExactlyOnceWith(
      `container:ct1:module:${WIRE_ID}`,
      28_000,
      expect.anything(),
    );
    expect(mocks.adapter.listModuleVoltageSettings).toHaveBeenCalledTimes(2);
  });

  test("confirms a valid value with Enter and ignores Enter on an invalid one", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 29_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);

    const dialog = await openFixedDialog();
    const input = within(dialog).getByLabelText("Fixed voltage");
    enterVoltage(dialog, "31000");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mocks.adapter.setModuleFixedVoltage).not.toHaveBeenCalled();

    enterVoltage(dialog, "29000");
    fireEvent.keyDown(input, { key: "Enter" });
    await expectSuccessToast("Fixed voltage of 29,000 mV saved and applied to all 3 hashboards.");
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledExactlyOnceWith(
      `container:ct1:module:${WIRE_ID}`,
      29_000,
      expect.anything(),
    );
  });

  test.each([
    ["25999", "Enter a value from 26,000–30,000 mV."],
    ["30001", "Enter a value from 26,000–30,000 mV."],
    ["28000.5", "Enter a whole number of millivolts."],
    ["1e5", "Enter a whole number of millivolts."],
    ["", "Enter a whole number of millivolts."],
  ])("rejects %j before sending", async (value, error) => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "27000");
    enterVoltage(dialog, value);
    expect(within(dialog).getByText(error)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Set voltage" })).toBeDisabled();
    expect(mocks.adapter.setModuleFixedVoltage).not.toHaveBeenCalled();
  });

  test.each([
    ["limitsUnavailable", "Firmware has not reported voltage limits for every hashboard in this module."],
    ["noCommonRange", "The hashboards in this module have no supported voltage range in common."],
  ] as const)("keeps an unavailable range unavailable (%s)", async (readiness, note) => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([
      setting({ readiness, supportedRange: undefined, fixedMillivolts: 28_000 }),
    ]);
    render(<ModuleVoltageSection module={module()} />);
    expect(await within(section()).findByText("Unavailable")).toBeInTheDocument();
    expect(within(section()).getByText(note)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change fixed voltage" })).toBeDisabled();
    // Returning to automatic needs no range.
    expect(screen.getByRole("button", { name: "Use automatic voltage" })).toBeEnabled();
  });

  test("clears a fixed voltage and confirms automatic mode by readback", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting({ fixedMillivolts: 28_000 })])
      .mockResolvedValue([setting()]);
    mocks.adapter.clearModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);

    fireEvent.click(await screen.findByRole("button", { name: "Use automatic voltage" }));
    const dialog = await screen.findByRole("dialog", { name: "Return to automatic voltage?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Use automatic voltage" }));

    await expectSuccessToast("Automatic voltage restored and applied to all 3 hashboards.");
    expect(within(section()).getByText("Automatic")).toBeInTheDocument();
    expect(mocks.adapter.clearModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("reports boards that have not applied the saved setting", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({
      boardsUpdated: 2,
      boards: [
        { macAddress: "aa:bb:cc:00:00:01", applied: true },
        { macAddress: "aa:bb:cc:00:00:02", applied: true, message: "recorded for the board's owner" },
        { macAddress: "aa:bb:cc:00:00:03", applied: false, message: "board is reserved" },
      ],
    });
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "Fixed voltage of 28,000 mV saved, but 1 of 3 hashboards has not applied it yet. The controller keeps retrying.",
    );
    expect(alert).toHaveTextContent("aa:bb:cc:00:00:03: Not applied yet: board is reserved");
    expect(alert).toHaveTextContent("aa:bb:cc:00:00:02: recorded for the board's owner");
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("shows a controller refusal without replaying the write", async () => {
    const refusal = "The controller refused the request: this node is not the container control primary.";
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.clearModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error(refusal), { writeOutcome: "refused" }),
    );
    render(<ModuleVoltageSection module={module()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Use automatic voltage" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Use automatic voltage" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(refusal);
    expect(within(section()).getByText("Fixed at 28,000 mV")).toBeInTheDocument();
    expect(mocks.adapter.clearModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test.each([
    [
      "matches the request",
      [setting({ fixedMillivolts: 28_000 })],
      "The controller did not answer, but it now reports a fixed voltage of 28,000 mV.",
    ],
    ["differs from the request", [setting()], `${MODULE_VOLTAGE_OUTCOME_UNKNOWN} The current setting is shown above.`],
  ])("resolves an unknown outcome only by re-reading when the readback %s", async (_name, reread, message) => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValueOnce([setting()]).mockResolvedValue(reread);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error(MODULE_VOLTAGE_OUTCOME_UNKNOWN), { writeOutcome: "unknown" }),
    );
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("treats a stalled write as an unknown outcome without replaying it", async () => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    vi.useFakeTimers();
    mocks.adapter.setModuleFixedVoltage.mockImplementation(() => new Promise(() => {}));
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));
    await act(async () => {});

    await act(() => vi.advanceTimersByTimeAsync(VOLTAGE_WRITE_TIMEOUT_MS));
    expect(screen.getByRole("alert")).toHaveTextContent(MODULE_VOLTAGE_OUTCOME_UNKNOWN);
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("does not claim success when the controller reports a different setting after the write", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 27_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The controller now reports a fixed voltage of 27,000 mV, not a fixed voltage of 28,000 mV.",
    );
  });

  test("reports a replaced module when the controller no longer lists it", async () => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValueOnce([setting()]).mockResolvedValue([]);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error("The controller refused the request: module not found."), { writeOutcome: "refused" }),
    );
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    expect(await screen.findByText(/does not list this module/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /fixed voltage/ })).not.toBeInTheDocument();
  });

  test("keeps the saved write when the setting cannot be re-read", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockRejectedValue(new Error("Failed to fetch"));
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);
    const dialog = await openFixedDialog();
    enterVoltage(dialog, "28000");
    fireEvent.click(within(dialog).getByRole("button", { name: "Set voltage" }));

    expect(
      await screen.findByText(
        "The controller saved the change, but the current setting could not be re-read. Reload before changing it again.",
      ),
    ).toBeVisible();
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting({ fixedMillivolts: 28000 })]);
    fireEvent.click(screen.getByRole("button", { name: "Reload" }));

    expect(await screen.findByText("Fixed at 28,000 mV")).toBeVisible();
    expect(screen.queryByText(/could not be re-read/)).not.toBeInTheDocument();
  });

  test("re-sends a write once after the token refresh that follows a 401", async () => {
    mocks.refreshSucceeds = true;
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.setModuleFixedVoltage
      .mockRejectedValueOnce(unauthenticated)
      .mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();

    await expectSuccessToast("Fixed voltage of 28,000 mV saved and applied to all 3 hashboards.");
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledTimes(2);
  });

  test("asks for a new sign-in when the write is rejected again after the refresh", async () => {
    mocks.refreshSucceeds = true;
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(unauthenticated);
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Sign in again before trying again. The voltage setting was not changed.",
    );
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledTimes(2);
  });

  test("does not re-send a write whose outcome is unknown, even when the token can be refreshed", async () => {
    mocks.refreshSucceeds = true;
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error(MODULE_VOLTAGE_OUTCOME_UNKNOWN), { writeOutcome: "unknown" }),
    );
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();

    expect(await screen.findByRole("alert")).toHaveTextContent(MODULE_VOLTAGE_OUTCOME_UNKNOWN);
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("offers another read next to an outcome that is not a confirmed change", async () => {
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error(MODULE_VOLTAGE_OUTCOME_UNKNOWN), { writeOutcome: "unknown" }),
    );
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();
    await screen.findByRole("alert");

    // The save lands after the first re-read.
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));

    expect(await within(section()).findByText("Fixed at 28,000 mV")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(mocks.adapter.setModuleFixedVoltage).toHaveBeenCalledOnce();
  });

  test("announces a confirmed change as a toast and keeps the section clear", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockResolvedValue({ boardsUpdated: 3, boards: applied(3) });
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();

    await expectSuccessToast("Fixed voltage of 28,000 mV saved and applied to all 3 hashboards.");
    expect(within(section()).getByText("Fixed at 28,000 mV")).toBeInTheDocument();
    expect(within(section()).queryByText(/saved and applied/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Check again" })).not.toBeInTheDocument();
  });

  test("does not say nothing changed when a refused write is followed by a different setting", async () => {
    mocks.adapter.listModuleVoltageSettings
      .mockResolvedValueOnce([setting()])
      .mockResolvedValue([setting({ fixedMillivolts: 28_000 })]);
    mocks.adapter.setModuleFixedVoltage.mockRejectedValue(
      Object.assign(new Error("The controller refused the request. The voltage setting was not changed."), {
        writeOutcome: "refused",
      }),
    );
    render(<ModuleVoltageSection module={module()} />);
    await setVoltage();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "The controller refused the request, but it now reports a fixed voltage of 28,000 mV.",
    );
    expect(alert).not.toHaveTextContent("was not changed");
  });

  test("reports a write in flight until its re-read settles", async () => {
    let answer!: (value: unknown) => void;
    mocks.adapter.listModuleVoltageSettings.mockResolvedValue([setting()]);
    mocks.adapter.setModuleFixedVoltage.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const onWritingChange = vi.fn();
    render(<ModuleVoltageSection module={module()} onWritingChange={onWritingChange} />);
    await setVoltage();
    expect(onWritingChange).toHaveBeenLastCalledWith(true);

    await act(async () => answer({ boardsUpdated: 3, boards: applied(3) }));
    await waitFor(() => expect(onWritingChange).toHaveBeenLastCalledWith(false));
  });

  test("never names a module by its slot position", () => {
    render(<ModuleVoltageSection module={module("container:ct1:tank:2:module:5")} />);
    expect(screen.getByText(/has not reported an identity for this module/)).toBeInTheDocument();
    expect(mocks.adapter.listModuleVoltageSettings).not.toHaveBeenCalled();
  });

  test("stays hidden when the miner is hosted in Fleet", () => {
    mocks.isFleetHosted = true;
    const { container } = render(<ModuleVoltageSection module={module()} />);
    expect(container).toBeEmptyDOMElement();
    expect(mocks.adapter.listModuleVoltageSettings).not.toHaveBeenCalled();
  });
});
