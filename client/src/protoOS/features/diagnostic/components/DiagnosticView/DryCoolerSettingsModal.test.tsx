import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import DryCoolerSettingsModal from "./DryCoolerSettingsModal";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { updateToast } from "@/shared/features/toaster";

const auth = vi.hoisted(() => ({ handleAuthErrors: vi.fn() }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ isFleetHosted: false }) }));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({ useAuthErrors: () => auth }));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => 1),
  updateToast: vi.fn(),
}));

const adapter = (overrides: Partial<MinerAdapter> = {}) =>
  ({
    setContainerControl: vi.fn().mockResolvedValue(undefined),
    setContainerSetpoint: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }) as MinerAdapter;

describe("DryCoolerSettingsModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.handleAuthErrors.mockImplementation(async ({ error, onSuccess, onError }) => {
      if (error.status === 401) await onSuccess();
      else onError(error);
    });
  });

  test.each(["target", "mode"])(
    "retries expired authentication during the %s write without replaying completed writes",
    async (phase) => {
      const miner = adapter();
      vi.mocked(phase === "target" ? miner.setContainerSetpoint! : miner.setContainerControl!).mockRejectedValueOnce({
        status: 401,
      });
      const onDismiss = vi.fn();
      render(
        <DryCoolerSettingsModal
          adapter={miner}
          initialMode="auto"
          initialTargetC={18}
          onSaved={vi.fn()}
          onDismiss={onDismiss}
        />,
      );
      fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
      await waitFor(() => expect(onDismiss).toHaveBeenCalledOnce());
      expect(auth.handleAuthErrors).toHaveBeenCalledOnce();
      expect(miner.setContainerSetpoint).toHaveBeenCalledTimes(phase === "target" ? 2 : 1);
      expect(miner.setContainerControl).toHaveBeenCalledTimes(phase === "mode" ? 2 : 1);
    },
  );

  test("stops the save sequence when the selected device changes", async () => {
    let finish!: () => void;
    const miner = adapter({
      setContainerSetpoint: vi.fn(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      ),
    });
    const onSaved = vi.fn();
    const onDismiss = vi.fn();
    render(
      <DryCoolerSettingsModal
        adapter={miner}
        initialMode="auto"
        initialTargetC={18}
        onSaved={onSaved}
        onDismiss={onDismiss}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
      finish();
    });
    expect(miner.setContainerControl).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
    expect(onDismiss).not.toHaveBeenCalled();
    expect(updateToast).toHaveBeenCalledWith(1, expect.objectContaining({ status: "error" }));
  });

  test("does not enable automatic mode if saving the target fails", async () => {
    const miner = adapter({ setContainerSetpoint: vi.fn().mockRejectedValue(new Error("Disconnected")) });
    const onSaved = vi.fn();
    const onDismiss = vi.fn();
    render(
      <DryCoolerSettingsModal
        adapter={miner}
        initialMode="auto"
        initialTargetC={18}
        onSaved={onSaved}
        onDismiss={onDismiss}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
    await waitFor(() =>
      expect(updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          message: "Could not confirm the target temperature was saved: Disconnected",
          status: "error",
        }),
      ),
    );
    expect(miner.setContainerControl).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getAllByRole("button", { name: "Save" })[0]).toBeEnabled();
  });

  test.each([false, true])("reports a partial save and refreshes state (refresh fails: %s)", async (refreshFails) => {
    const miner = adapter({ setContainerControl: vi.fn().mockRejectedValue(new Error("Disconnected")) });
    const onSaved = refreshFails
      ? vi.fn().mockRejectedValue(new Error("Offline"))
      : vi.fn().mockResolvedValue(undefined);
    const onDismiss = vi.fn();
    render(
      <DryCoolerSettingsModal
        adapter={miner}
        initialMode="auto"
        initialTargetC={18}
        onSaved={onSaved}
        onDismiss={onDismiss}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
    await waitFor(() =>
      expect(updateToast).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          message: `Target temperature saved, but automatic mode could not be confirmed: Disconnected${refreshFails ? ". Readings could not be refreshed." : ""}`,
          status: "error",
        }),
      ),
    );
    expect(onSaved).toHaveBeenCalledOnce();
    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getAllByRole("button", { name: "Save" })[0]).toBeEnabled();
  });

  test("closes after successful writes even when refreshing readings fails", async () => {
    const miner = adapter();
    const onDismiss = vi.fn();
    render(
      <DryCoolerSettingsModal
        adapter={miner}
        initialMode="auto"
        initialTargetC={18}
        onSaved={vi.fn().mockRejectedValue(new Error("Offline"))}
        onDismiss={onDismiss}
      />,
    );
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);
    await waitFor(() => expect(onDismiss).toHaveBeenCalledOnce());
    expect(updateToast).toHaveBeenCalledWith(
      1,
      expect.objectContaining({
        message: "Dry cooler settings saved, but readings could not be refreshed. No need to save again.",
        status: "error",
      }),
    );
    expect(miner.setContainerSetpoint).toHaveBeenCalledOnce();
    expect(miner.setContainerControl).toHaveBeenCalledOnce();
  });

  test("writes the automatic target before enabling automatic mode", async () => {
    const setControl = vi.fn().mockResolvedValue(undefined);
    const setSetpoint = vi.fn().mockResolvedValue(undefined);
    const onSaved = vi.fn().mockResolvedValue(undefined);
    const onDismiss = vi.fn();

    render(
      <DryCoolerSettingsModal
        adapter={adapter({ setContainerControl: setControl, setContainerSetpoint: setSetpoint })}
        initialMode="auto"
        initialTargetC={18.3}
        onSaved={onSaved}
        onDismiss={onDismiss}
      />,
    );

    fireEvent.change(screen.getByLabelText("Target °C"), { target: { value: "19.5" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
    expect(setSetpoint).toHaveBeenCalledWith("dry_cooler_auto_temp_c", 19.5, expect.anything());
    expect(setControl).toHaveBeenCalledWith("dry_cooler_auto", true, expect.anything());
    expect(setSetpoint.mock.invocationCallOrder[0]).toBeLessThan(setControl.mock.invocationCallOrder[0]);
    expect(onSaved).toHaveBeenCalled();
  });

  test("shows the supplied manual design without offering unsupported RPM writes", () => {
    render(
      <DryCoolerSettingsModal
        adapter={adapter()}
        initialMode="auto"
        initialTargetC={18.3}
        onSaved={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );

    fireEvent.mouseDown(screen.getByRole("button", { name: "Manual" }));

    expect(screen.getByLabelText("Pump speed (RPM)")).toBeDisabled();
    expect(screen.getByLabelText("Fan speed (RPM)")).toBeDisabled();
    screen.getAllByRole("button", { name: "Save" }).forEach((button) => expect(button).toBeDisabled());
    expect(screen.queryByText(/container API|not yet exposed/i)).not.toBeInTheDocument();
  });
});
