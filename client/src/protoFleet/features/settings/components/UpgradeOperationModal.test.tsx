import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  ReleaseInfoSchema,
  type UpgradeOperation,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import UpgradeOperationModal, {
  type UpgradeOperationModalProps,
} from "@/protoFleet/features/settings/components/UpgradeOperationModal";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    error: "error",
    success: "success",
  },
}));

vi.mock("@/shared/utils/utility", () => ({
  copyToClipboard: vi.fn(),
}));

const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const release = (version = "v1.3.0", prerelease = false) =>
  create(ReleaseInfoSchema, {
    version,
    prerelease,
  });

type OperationOverrides = Partial<
  Pick<UpgradeOperation, "error" | "hostLogPath" | "message" | "recoveryCommand" | "targetVersion">
>;

const operation = (phase: UpgradePhase, overrides?: OperationOverrides) =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
    ...overrides,
  });

const renderModal = (overrides: Partial<UpgradeOperationModalProps> = {}) => {
  const props: UpgradeOperationModalProps = {
    connectionLost: false,
    manualFallbackReady: false,
    onAcknowledge: vi.fn(),
    onDismiss: vi.fn(),
    onReload: vi.fn(),
    onUpgrade: vi.fn().mockResolvedValue(undefined),
    onUseManualFallback: vi.fn(),
    open: true,
    reconciling: false,
    reloadPending: false,
    release: release(),
    triggerError: null,
    triggering: false,
    ...overrides,
  };
  return { ...render(<UpgradeOperationModal {...props} />), props };
};

describe("UpgradeOperationModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCopyToClipboard.mockResolvedValue(undefined);
  });

  it("confirms the exact stable target before starting the upgrade", async () => {
    const onUpgrade = vi.fn().mockResolvedValue(undefined);
    renderModal({ onUpgrade });

    const confirmButton = screen.getByRole("button", { name: "Update now" });
    expect(screen.getByTestId("upgrade-dialog-icon")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument();
    expect(confirmButton).toBeInTheDocument();
    fireEvent.click(confirmButton);

    await waitFor(() => expect(onUpgrade).toHaveBeenCalledWith("v1.3.0"));
  });

  it("renders the release-candidate warning as supporting text", () => {
    renderModal({ release: release("v1.3.0-rc.2", true) });

    expect(screen.getByText("You can't return to an earlier release after this update.")).toHaveClass(
      "text-text-primary-70",
    );
    expect(screen.queryByTestId("callout")).not.toBeInTheDocument();
  });

  it("keeps a long-running upgrade dismissible", () => {
    const onDismiss = vi.fn();
    renderModal({
      onDismiss,
      operation: operation(UpgradePhase.PREFLIGHT, { message: "Validating the new stack" }),
    });

    expect(screen.getByTestId("upgrade-operation-modal")).toHaveTextContent("Updating Fleet to v1.3.0");
    expect(screen.getByRole("status")).toHaveTextContent("Checking update…");
    expect(screen.getByRole("status")).toHaveTextContent(
      "This may take a few minutes. You can close this dialog while it runs.",
    );
    expect(screen.getByTestId("upgrade-operation-modal")).not.toHaveTextContent("Validating the new stack");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("keeps active update details available while reconciling", () => {
    renderModal({
      operation: operation(UpgradePhase.PREFLIGHT, { message: "Validating the new stack" }),
      reconciling: true,
    });

    expect(screen.getByTestId("upgrade-operation-modal")).toHaveTextContent("Checking update to v1.3.0");
    expect(screen.getByRole("status")).toHaveTextContent("Checking update…");
    expect(screen.getByRole("status")).toHaveTextContent(
      "This may take a few minutes. You can close this dialog while it runs.",
    );
  });

  it("announces the expected reconnect state", () => {
    renderModal({
      connectionLost: true,
      operation: operation(UpgradePhase.ACTIVATING),
    });

    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveTextContent("Restarting Fleet…");
    expect(status).toHaveTextContent(/will reconnect when the update finishes/);
  });

  it("requires explicit host confirmation before unlocking a manual fallback", () => {
    const onUseManualFallback = vi.fn();
    renderModal({
      connectionLost: true,
      manualFallbackReady: true,
      onUseManualFallback,
      reconciling: true,
      triggerError: "Host updater did not confirm the update",
    });

    expect(screen.getByText(/We couldn't confirm whether the update started/i)).toBeInTheDocument();
    expect(screen.queryByText("Host updater did not confirm the update")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Unlock manual install" }));
    expect(onUseManualFallback).toHaveBeenCalledOnce();
  });

  it("shows failed-operation details and copies the recovery command", async () => {
    const recoveryCommand = "cd /opt/proto-fleet/deployment && ./run-fleet.sh --non-interactive --skip-build";
    renderModal({
      operation: operation(UpgradePhase.FAILED, {
        message: "Update couldn't complete",
        error: "new stack failed to start",
        hostLogPath: "/var/lib/proto-fleet-updater/logs/operation-1.log",
        recoveryCommand,
      }),
    });

    const alert = screen.getByRole("alert");
    expect(screen.getByText("Update needs recovery")).toBeInTheDocument();
    expect(alert).toHaveTextContent("Run this command on the Fleet host to continue the update.");
    expect(alert).toHaveTextContent("new stack failed to start");
    expect(alert).toHaveTextContent("operation-1.log");
    expect(alert).toHaveTextContent("Mark this update resolved only after you no longer need these recovery details.");
    fireEvent.click(screen.getByRole("button", { name: "Copy recovery command" }));

    await waitFor(() => expect(mockCopyToClipboard).toHaveBeenCalledWith(recoveryCommand));
    expect(mockPushToast).toHaveBeenCalledWith({
      message: "Recovery command copied to clipboard",
      status: STATUSES.success,
    });
  });

  it("reports a recovery-command copy failure", async () => {
    mockCopyToClipboard.mockRejectedValue(new Error("copy failed"));
    renderModal({
      operation: operation(UpgradePhase.FAILED, {
        recoveryCommand: "./run-fleet.sh --non-interactive --skip-build",
      }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Copy recovery command" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Couldn't copy recovery command",
        status: STATUSES.error,
      }),
    );
  });

  it("does not render an empty recovery fallback", () => {
    renderModal({
      operation: operation(UpgradePhase.FAILED, {
        error: "preflight failed",
        recoveryCommand: "   ",
      }),
    });

    expect(screen.queryByText("Recovery command")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy recovery command" })).not.toBeInTheDocument();
  });

  it("acknowledges a failure only through the explicit resolved action", () => {
    const onAcknowledge = vi.fn();
    const onDismiss = vi.fn();
    renderModal({
      onAcknowledge,
      onDismiss,
      operation: operation(UpgradePhase.FAILED),
    });

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(onAcknowledge).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Mark resolved" }));
    expect(onAcknowledge).toHaveBeenCalledOnce();
  });

  it("reloads Fleet after a successful upgrade", () => {
    const onReload = vi.fn();
    renderModal({
      onReload,
      operation: operation(UpgradePhase.SUCCEEDED, { message: "Fleet v1.3.0 is running" }),
    });

    expect(screen.getByText("Relaunch Fleet to finish updating to v1.3.0.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Relaunch" }));
    expect(onReload).toHaveBeenCalledOnce();
  });

  it("announces a trigger error and allows retry after reconciliation", async () => {
    const onUpgrade = vi.fn().mockResolvedValue(undefined);
    renderModal({
      onUpgrade,
      triggerError: "Host updater did not answer",
    });

    expect(screen.getByRole("alert")).toHaveTextContent("Host updater did not answer");
    fireEvent.click(screen.getByRole("button", { name: "Update now" }));
    await waitFor(() => expect(onUpgrade).toHaveBeenCalledWith("v1.3.0"));
  });

  it("keeps a trigger error visible when there is no longer an eligible release", () => {
    const onDismiss = vi.fn();
    renderModal({
      release: undefined,
      triggerError: "The eligible release changed before the request completed",
      onDismiss,
    });

    expect(screen.getByRole("alert")).toHaveTextContent("The eligible release changed before the request completed");
    expect(screen.queryByRole("button", { name: "Update now" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
