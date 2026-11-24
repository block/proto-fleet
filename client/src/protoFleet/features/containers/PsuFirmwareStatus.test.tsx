import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { psuNeedsRecovery } from "./modulePsu";
import PsuFirmwareStatus from "./PsuFirmwareStatus";

const observedAt = () => new Date().toISOString();

describe("PsuFirmwareStatus", () => {
  it("shows programming progress and the controller that runs the job", () => {
    render(
      <PsuFirmwareStatus
        now={Date.now()}
        update={{
          state: "PSU_FIRMWARE_UPDATE_STATE_APPLYING",
          progressPercent: 64.6,
          serverId: "ct1-b",
          observedAt: observedAt(),
        }}
      />,
    );
    expect(screen.getByText("Updating (65%)")).toBeInTheDocument();
    expect(screen.getByText("ct1-b")).toBeInTheDocument();
    expect(screen.queryByText("Mining")).not.toBeInTheDocument();
  });

  it("names a PSU that needs recovery and how to recover it", () => {
    const update = {
      state: "PSU_FIRMWARE_UPDATE_STATE_FAILED",
      progressPercent: 0,
      recoveryRequired: true,
      miningHeld: true,
      message: "PSU firmware update failed in PSU_FW_PHASE_RECOVERY_REQUIRED: PSU_FW_ERROR_I2C",
      observedAt: observedAt(),
    };
    render(<PsuFirmwareStatus now={Date.now()} update={update} />);
    expect(psuNeedsRecovery(update)).toBe(true);
    expect(screen.getByText("Recovery required")).toBeInTheDocument();
    expect(screen.getByText(update.message)).toBeInTheDocument();
    expect(screen.getByText("Stopped. Send the same PSU firmware again to recover.")).toBeInTheDocument();
  });

  it("shows a failure that left the module mining, and labels stale evidence", () => {
    render(
      <PsuFirmwareStatus
        now={Date.now()}
        update={{
          state: "PSU_FIRMWARE_UPDATE_STATE_FAILED",
          message: "PSU firmware update failed in PSU_FW_PHASE_FAILED: PSU_FW_ERROR_TARGET_MISMATCH",
          observedAt: "2020-01-01T00:00:00Z",
        }}
      />,
    );
    expect(screen.getByText("Failed · Last observed")).toBeInTheDocument();
    expect(screen.getByText(/TARGET_MISMATCH/)).toBeInTheDocument();
    expect(screen.queryByText("Mining")).not.toBeInTheDocument();
  });

  it("reports a module the update left stopped without a recovery", () => {
    render(
      <PsuFirmwareStatus
        now={Date.now()}
        update={{ state: "PSU_FIRMWARE_UPDATE_STATE_FAILED", miningHeld: true, observedAt: observedAt() }}
      />,
    );
    expect(screen.getByText("Stopped by the update")).toBeInTheDocument();
  });

  it("renders nothing without a job or for a state it does not know", () => {
    const { container, rerender } = render(<PsuFirmwareStatus now={Date.now()} update={undefined} />);
    expect(container).toBeEmptyDOMElement();
    rerender(<PsuFirmwareStatus now={Date.now()} update={{ state: "PSU_FIRMWARE_UPDATE_STATE_NEW" }} />);
    expect(container).toBeEmptyDOMElement();
    expect(psuNeedsRecovery(undefined)).toBe(false);
  });
});
