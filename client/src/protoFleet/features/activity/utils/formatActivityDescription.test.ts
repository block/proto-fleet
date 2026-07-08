import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  formatActivityDescription,
  formatActivityErrorMessage,
  formatActivityErrorSummary,
} from "./formatActivityDescription";
import { ActivityEntrySchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";

describe("formatActivityDescription", () => {
  it("uses plain language for auth activity", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "login_failed",
      description: "Login failed",
    });

    expect(formatActivityDescription(entry)).toBe("Couldn't log in");
  });

  it("uses metadata to avoid backend IDs in site and building descriptions", () => {
    const siteEntry = create(ActivityEntrySchema, {
      eventType: "site.created",
      description: 'Created site "Denver" (id=42)',
      metadata: { site_name: "Denver" },
    });
    const buildingEntry = create(ActivityEntrySchema, {
      eventType: "building.updated",
      description: 'Updated building "Building A" (id=7)',
      metadata: { building_name: "Building A" },
    });

    expect(formatActivityDescription(siteEntry)).toBe("Created site: Denver");
    expect(formatActivityDescription(buildingEntry)).toBe("Updated building: Building A");
  });

  it("keeps rack-clearing wording for the shared assign event type", () => {
    const clearEntry = create(ActivityEntrySchema, {
      eventType: "assign_devices_to_rack",
      description: "Cleared devices from rack",
    });
    const assignEntry = create(ActivityEntrySchema, {
      eventType: "assign_devices_to_rack",
      description: "Assigned devices to rack: Rack 7",
    });

    expect(formatActivityDescription(clearEntry)).toBe("Cleared miners from rack");
    expect(formatActivityDescription(assignEntry)).toBe("Assigned miners to rack: Rack 7");
  });

  it("uses readable action strings for rack position changes", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "set_rack_slot",
      description: "Set rack slot position",
      scopeLabel: "Rack 7",
    });

    expect(formatActivityDescription(entry)).toBe("Updated rack position: Rack 7");
  });

  it("uses in-progress wording for batch commands that are still running", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "reboot",
      description: "Reboot",
      batchId: "batch-1",
    });

    expect(formatActivityDescription(entry)).toBe("Rebooting miners");
  });

  it("falls back to the cleaned raw description for command events without a batch", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "reboot",
      description: "Reboot 3 device(s)",
    });

    expect(formatActivityDescription(entry)).toBe("Reboot 3 miners");
  });

  it("summarizes completed command counts as a ratio", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "reboot.completed",
      description: "Reboot completed: 2 succeeded, 1 failed",
      metadata: { success_count: 2, failure_count: 1 },
    });

    expect(formatActivityDescription(entry)).toBe("Rebooted miners: 2/3 miners completed");
  });

  it("uses singular miner wording for one completed command target", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "set_power_target.completed",
      description: "Set power target completed",
      metadata: { success_count: 1, failure_count: 0 },
    });

    expect(formatActivityDescription(entry)).toBe("Updated power target: 1/1 miner completed");
  });

  it("shows zero completed miners as a ratio when a command has failures", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "set_power_target.completed",
      description: "Set power target completed",
      metadata: { success_count: 0, failure_count: 9 },
    });

    expect(formatActivityDescription(entry)).toBe("Updated power target: 0/9 miners completed");
  });

  it("cleans fallback descriptions without changing backend values", () => {
    const entry = create(ActivityEntrySchema, {
      eventType: "future_event",
      description: "Future event failed on 2 device(s)",
    });

    expect(formatActivityDescription(entry)).toBe("Future event not completed on 2 miners");
  });
});

describe("formatActivityErrorMessage", () => {
  it("uses plain language for known auth errors", () => {
    expect(formatActivityErrorMessage("invalid credentials")).toBe("Credentials didn't match.");
  });

  it("summarizes noisy miner connection errors", () => {
    const message =
      "Internal: error getting miner connection info for deviceID: 19, failed to connect to miner: i/o timeout";

    expect(formatActivityErrorSummary(message)).toBe("Miner didn't respond.");
    expect(formatActivityErrorMessage(message)).toBe("Couldn't connect to miner. Connection timed out.");
  });

  it("uses miner addresses when shortening connection errors", () => {
    const message =
      "Internal: error getting miner connection info for deviceID: 19, not completed to connect to miner 019d53fc: not completed to create SDK miner: rpc error: code = Unknown desc = not completed to connect miner: not completed to verify miner communication: not completed to get miner status: not completed to get summary: not completed to connect to 192.168.2.7:4028: dial tcp 192.168.2.7:4028: i/o timeout";

    expect(formatActivityErrorMessage(message)).toBe(
      "Couldn't connect to miner at 192.168.2.7:4028. Connection timed out.",
    );
  });

  it("shortens deadline and refused connection errors", () => {
    expect(formatActivityErrorSummary("rpc error: code = DeadlineExceeded desc = miner did not respond")).toBe(
      "Miner didn't respond.",
    );
    expect(formatActivityErrorMessage("context deadline exceeded while connecting to miner")).toBe(
      "Couldn't connect to miner. Connection timed out.",
    );
    expect(
      formatActivityErrorMessage("failed to connect to miner: dial tcp 192.168.2.8:4028: connection refused"),
    ).toBe("Couldn't connect to miner at 192.168.2.8:4028. Connection was refused.");
  });
});
