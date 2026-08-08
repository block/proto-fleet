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

    const confirmButton = screen.getByRole("button", { name: "Confirm upgrade to v1.3.0" });
    expect(confirmButton).toBeInTheDocument();
    fireEvent.click(confirmButton);

    await waitFor(() => expect(onUpgrade).toHaveBeenCalledWith("v1.3.0"));
  });

  it("gives release candidates a strong forward-only migration warning", () => {
    renderModal({ release: release("v1.3.0-rc.2", true) });

    expect(screen.getByText(/This is a release candidate/)).toBeInTheDocument();
    expect(screen.getByText(/forward-only database migrations/)).toBeInTheDocument();
    expect(screen.getByText(/cannot downgrade this instance afterward/)).toBeInTheDocument();
  });

  it("keeps a long-running upgrade dismissible", () => {
    const onDismiss = vi.fn();
    renderModal({
      onDismiss,
      operation: operation(UpgradePhase.PREFLIGHT, { message: "Validating the new stack" }),
    });

    expect(screen.getByRole("status")).toHaveTextContent("Phase: Preflight");
    fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it("announces the expected reconnect state", () => {
    renderModal({
      connectionLost: true,
      operation: operation(UpgradePhase.ACTIVATING),
    });

    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveTextContent(/disconnect is expected while services restart/);
  });

  it("warns against manual installation while reconciling an ambiguous trigger", () => {
    renderModal({ reconciling: true, triggerError: "The request timed out" });

    expect(screen.getByRole("status")).toHaveTextContent(/Checking upgrade status/);
    expect(screen.getByRole("status")).toHaveTextContent(/Do not run the manual install command yet/);
    expect(screen.queryByRole("button", { name: /Confirm upgrade/ })).not.toBeInTheDocument();
  });

  it("requires explicit host confirmation before unlocking a manual fallback", () => {
    const onUseManualFallback = vi.fn();
    renderModal({
      connectionLost: true,
      manualFallbackReady: true,
      onUseManualFallback,
      reconciling: true,
      triggerError: "Host updater did not confirm the upgrade",
    });

    expect(screen.getByRole("status")).toHaveTextContent(/checking the host and confirming no upgrade is running/i);
    fireEvent.click(screen.getByRole("button", { name: "I confirmed — unlock manual install" }));
    expect(onUseManualFallback).toHaveBeenCalledOnce();
  });

  it("labels reconciliation with its tracked target rather than a newer offer", () => {
    renderModal({
      reconciling: true,
      release: release("v1.4.0"),
      targetVersion: "v1.3.0",
      triggerError: "The v1.3.0 request has an unknown outcome",
    });

    expect(screen.getByTestId("upgrade-operation-modal")).toHaveTextContent("Upgrade Fleet to v1.3.0");
    expect(screen.getByTestId("upgrade-operation-modal")).not.toHaveTextContent("Upgrade Fleet to v1.4.0");
  });

  it("shows failed-operation details and copies the recovery command", async () => {
    const recoveryCommand = "cd /opt/proto-fleet/deployment && ./run-fleet.sh --non-interactive --skip-build";
    renderModal({
      operation: operation(UpgradePhase.FAILED, {
        message: "Upgrade failed",
        error: "new stack failed to start",
        hostLogPath: "/var/lib/proto-fleet-updater/logs/operation-1.log",
        recoveryCommand,
      }),
    });

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("new stack failed to start");
    expect(alert).toHaveTextContent("operation-1.log");
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
        message: "Failed to copy recovery command",
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

  it("acknowledges a failure separately from hiding its details", () => {
    const onAcknowledge = vi.fn();
    const onDismiss = vi.fn();
    renderModal({
      onAcknowledge,
      onDismiss,
      operation: operation(UpgradePhase.FAILED),
    });

    fireEvent.click(screen.getByRole("button", { name: "Dismiss failure" }));
    expect(onAcknowledge).toHaveBeenCalledOnce();
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("reloads Fleet after a successful upgrade", () => {
    const onReload = vi.fn();
    renderModal({
      onReload,
      operation: operation(UpgradePhase.SUCCEEDED, { message: "Fleet v1.3.0 is running" }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Reload Fleet" }));
    expect(onReload).toHaveBeenCalledOnce();
  });

  it("announces a trigger error and allows retry after reconciliation", async () => {
    const onUpgrade = vi.fn().mockResolvedValue(undefined);
    renderModal({
      onUpgrade,
      triggerError: "Host updater did not answer",
    });

    expect(screen.getByRole("alert")).toHaveTextContent("Host updater did not answer");
    fireEvent.click(screen.getByRole("button", { name: "Confirm upgrade to v1.3.0" }));
    await waitFor(() => expect(onUpgrade).toHaveBeenCalledWith("v1.3.0"));
  });

  it("keeps a trigger error visible when there is no longer an eligible release", () => {
    renderModal({
      release: undefined,
      triggerError: "The eligible release changed before the request completed",
    });

    expect(screen.getByRole("alert")).toHaveTextContent("The eligible release changed before the request completed");
    expect(screen.queryByRole("button", { name: /Confirm upgrade/ })).not.toBeInTheDocument();
  });
});
