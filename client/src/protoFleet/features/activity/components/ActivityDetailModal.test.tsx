import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ActivityDetailModal from "./ActivityDetailModal";
import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { GetCommandBatchDeviceResultsResponseSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";

const fetchBatchResultsMock = vi.hoisted(() => vi.fn());
const getBatchResultMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/useCommandBatchDeviceResults", () => ({
  useCommandBatchDeviceResults: () => ({
    fetch: fetchBatchResultsMock,
    getResult: getBatchResultMock,
  }),
}));

describe("ActivityDetailModal", () => {
  beforeEach(() => {
    fetchBatchResultsMock.mockReset();
    getBatchResultMock.mockReset();
  });

  it("renders command device issue rows with visible details", async () => {
    const batchData = create(GetCommandBatchDeviceResultsResponseSchema, {
      batchIdentifier: "batch-1",
      commandType: "set_power_target",
      status: "finished",
      totalCount: 2,
      successCount: 0,
      failureCount: 2,
      deviceResults: [
        {
          deviceIdentifier: "miner-1",
          status: "failed",
          deviceName: "Bitmain Antminer S17",
          macAddress: "02:42:27:15:62:E2",
          ipAddress: "192.168.2.7",
          errorMessage:
            "Internal: error getting miner connection info for deviceID: 19, failed to connect to miner: i/o timeout",
          updatedAt: { seconds: 1_781_000_000n },
        },
        {
          deviceIdentifier: "miner-2",
          status: "failed",
          deviceName: "Proto Rig",
          errorMessage: "context deadline exceeded while connecting to miner",
          updatedAt: { seconds: 1_781_000_001n },
        },
      ],
    });
    getBatchResultMock.mockReturnValue({
      data: batchData,
      isLoading: false,
      error: null,
    });

    const entry = create(ActivityEntrySchema, {
      eventId: "activity-1",
      eventType: "set_power_target.completed",
      eventCategory: "device_command",
      scopeType: "miner",
      scopeCount: 2,
      username: "scheduler",
      result: "failure",
      batchId: "batch-1",
      createdAt: { seconds: 1_781_000_000n },
      metadata: { success_count: 0, failure_count: 2 },
    });

    render(<ActivityDetailModal entry={entry} onDismiss={vi.fn()} />);

    await waitFor(() => {
      expect(fetchBatchResultsMock).toHaveBeenCalledWith("batch-1");
    });

    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.queryByText("Status")).not.toBeInTheDocument();
    expect(screen.queryByText("Message")).not.toBeInTheDocument();
    expect(screen.queryByText("Miner didn't respond.")).not.toBeInTheDocument();
    expect(screen.getByText("0/2 miners completed")).toBeInTheDocument();
    expect(screen.queryByText("Not completed")).not.toBeInTheDocument();
    expect(screen.queryByText("Couldn't complete")).not.toBeInTheDocument();

    const issueList = screen.getByText("Issues").nextElementSibling;
    if (!issueList) throw new Error("Expected issue list");

    expect(issueList).toHaveClass("divide-y", "border-y");
    expect(screen.getByText("Bitmain Antminer S17")).toBeInTheDocument();
    expect(screen.getByText("02:42:27:15:62:E2, 192.168.2.7")).toHaveClass("text-200", "break-words");
    expect(screen.queryByText("View details")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Bitmain Antminer S17/ })).not.toBeInTheDocument();
    expect(screen.getAllByText("Couldn't connect to miner. Connection timed out.")[0]).toHaveClass(
      "text-200",
      "break-words",
    );
  });

  it("shows In progress instead of completion counts while a batch is still running", async () => {
    const batchData = create(GetCommandBatchDeviceResultsResponseSchema, {
      batchIdentifier: "batch-1",
      commandType: "reboot",
      status: "processing",
      totalCount: 10,
      successCount: 3,
      failureCount: 0,
      deviceResults: [],
    });
    getBatchResultMock.mockReturnValue({
      data: batchData,
      isLoading: false,
      error: null,
    });

    const entry = create(ActivityEntrySchema, {
      eventId: "activity-1",
      eventType: "reboot",
      eventCategory: "device_command",
      scopeType: "miner",
      scopeCount: 10,
      username: "scheduler",
      result: "success",
      batchId: "batch-1",
      createdAt: { seconds: 1_781_000_000n },
    });

    render(<ActivityDetailModal entry={entry} onDismiss={vi.fn()} />);

    await waitFor(() => {
      expect(fetchBatchResultsMock).toHaveBeenCalledWith("batch-1");
    });

    expect(screen.getByText("In progress")).toBeInTheDocument();
    expect(screen.queryByText("3/10 miners completed")).not.toBeInTheDocument();
  });

  it("shows hidden issue and truncation messaging when failures are outside the returned slice", async () => {
    const batchData = create(GetCommandBatchDeviceResultsResponseSchema, {
      batchIdentifier: "batch-1",
      commandType: "set_power_target",
      status: "finished",
      totalCount: 6000,
      successCount: 5999,
      failureCount: 1,
      truncated: true,
      deviceResults: [
        {
          deviceIdentifier: "miner-1",
          status: "success",
          deviceName: "Bitmain Antminer S17",
        },
      ],
    });
    getBatchResultMock.mockReturnValue({
      data: batchData,
      isLoading: false,
      error: null,
    });

    const entry = create(ActivityEntrySchema, {
      eventId: "activity-1",
      eventType: "set_power_target.completed",
      eventCategory: "device_command",
      scopeType: "miner",
      scopeCount: 6000,
      username: "scheduler",
      result: "failure",
      batchId: "batch-1",
      createdAt: { seconds: 1_781_000_000n },
      metadata: { success_count: 5999, failure_count: 1 },
    });

    render(<ActivityDetailModal entry={entry} onDismiss={vi.fn()} />);

    await waitFor(() => {
      expect(fetchBatchResultsMock).toHaveBeenCalledWith("batch-1");
    });

    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.getByText("Issue details may be outside the results shown.")).toBeInTheDocument();
    expect(screen.getByText("Some miner details may not be shown.")).toBeInTheDocument();
  });

  it("renders cohort update metadata with friendly labels", () => {
    const entry = create(ActivityEntrySchema, {
      eventId: "event-1",
      eventCategory: "fleet_management",
      eventType: "cohort_updated",
      description: "Updated cohort",
      scopeType: "cohort",
      scopeLabel: "Test cohort",
      actorType: "user",
      username: "admin",
      result: "success",
      createdAt: { seconds: 1_767_225_600n },
      metadata: {
        cohort_id: 42,
        label: "Test cohort",
        update_kind: "firmware_target_updated",
        manufacturer: "Proto",
        model: "Rig",
        old_firmware_file_id: "fw-old",
        new_firmware_file_id: "fw-new",
        affected_member_count: 2,
        idempotency_key: "do-not-render",
        device_identifiers: ["miner-1", "miner-2"],
      },
    });

    render(<ActivityDetailModal entry={entry} onDismiss={vi.fn()} />);

    expect(screen.getByText("Cohort updated")).toBeInTheDocument();
    expect(screen.getByText("Update kind")).toBeInTheDocument();
    expect(screen.getByText("Firmware target updated")).toBeInTheDocument();
    expect(screen.getByText("Target")).toBeInTheDocument();
    expect(screen.getByText("Proto Rig")).toBeInTheDocument();
    expect(screen.getByText("Firmware before")).toBeInTheDocument();
    expect(screen.getByText("fw-old")).toBeInTheDocument();
    expect(screen.getByText("Firmware after")).toBeInTheDocument();
    expect(screen.getByText("fw-new")).toBeInTheDocument();
    expect(screen.getByText("Affected miners")).toBeInTheDocument();
    expect(screen.getByText("2 miners")).toBeInTheDocument();
    expect(screen.queryByText("do-not-render")).not.toBeInTheDocument();
    expect(screen.queryByText("miner-1")).not.toBeInTheDocument();
  });
});
