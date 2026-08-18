import { describe, expect, it, vi } from "vitest";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import { createMaintenanceWindow, listActiveAlertGroups } from "./alertsApi";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const listActiveAlertGroupsMock = vi.hoisted(() => vi.fn());
const createMaintenanceWindowMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/clients", () => ({
  alertChannelClient: {},
  alertHistoryClient: { listActiveAlertGroups: listActiveAlertGroupsMock },
  alertMaintenanceWindowClient: { createMaintenanceWindow: createMaintenanceWindowMock },
  alertRuleClient: {},
}));

describe("listActiveAlertGroups", () => {
  it("deadlines the request, so a stalled poll fails loudly instead of leaving the pill on an all-clear", async () => {
    listActiveAlertGroupsMock.mockResolvedValue({ groups: [], hasMore: false });

    await listActiveAlertGroups();

    expect(listActiveAlertGroupsMock).toHaveBeenCalledWith({}, { timeoutMs: POLL_INTERVAL_MS });
  });
});

describe("createMaintenanceWindow", () => {
  it("carries the rule/channel targets in the scope message and flattens them on the way back", async () => {
    createMaintenanceWindowMock.mockResolvedValue({
      maintenanceWindow: {
        id: "5",
        organizationId: 7n,
        scope: { kind: 0, ruleIds: ["rule-a"], channelIds: ["3"] },
        startsAt: timestampFromDate(new Date("2026-08-18T10:00:00Z")),
        endsAt: timestampFromDate(new Date("2026-08-18T12:00:00Z")),
        comment: "planned",
        createdBy: "alice@example.com",
        createdAt: timestampFromDate(new Date("2026-08-18T09:00:00Z")),
        active: true,
      },
    });

    const created = await createMaintenanceWindow({
      rule_ids: ["rule-a"],
      channel_ids: ["3"],
      starts_at: "2026-08-18T10:00:00.000Z",
      ends_at: "2026-08-18T12:00:00.000Z",
      comment: "planned",
    });

    expect(createMaintenanceWindowMock).toHaveBeenCalledWith(
      expect.objectContaining({ scope: { ruleIds: ["rule-a"], channelIds: ["3"] } }),
    );
    expect(created.rule_ids).toEqual(["rule-a"]);
    expect(created.channel_ids).toEqual(["3"]);
    expect(created.ends_at).toBe("2026-08-18T12:00:00.000Z");
  });
});
