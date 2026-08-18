import { describe, expect, it, vi } from "vitest";

import { listActiveAlertGroups } from "./alertsApi";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

const listActiveAlertGroupsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/clients", () => ({
  alertChannelClient: {},
  alertHistoryClient: { listActiveAlertGroups: listActiveAlertGroupsMock },
  alertMaintenanceWindowClient: {},
  alertRuleClient: {},
}));

describe("listActiveAlertGroups", () => {
  it("deadlines the request, so a stalled poll fails loudly instead of leaving the pill on an all-clear", async () => {
    listActiveAlertGroupsMock.mockResolvedValue({ groups: [], hasMore: false });

    await listActiveAlertGroups();

    expect(listActiveAlertGroupsMock).toHaveBeenCalledWith({}, { timeoutMs: POLL_INTERVAL_MS });
  });
});
