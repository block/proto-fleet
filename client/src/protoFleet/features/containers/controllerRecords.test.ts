import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { fleetControllerRecords } from "./controllerRecords";
import {
  ControllerLogCoverage,
  ControllerLogSeverity,
  GetControllerLogsResponseSchema,
  ListControllerAuditEventsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";

const client = vi.hoisted(() => ({ getControllerLogs: vi.fn(), listControllerAuditEvents: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: client }));

const readAt = new Date("2026-11-14T10:00:05Z");
const signal = new AbortController().signal;

describe("fleetControllerRecords", () => {
  const handleAuthErrors = vi.fn();
  const source = fleetControllerRecords("ct1/a", handleAuthErrors);

  beforeEach(() => {
    handleAuthErrors.mockReset();
    client.getControllerLogs.mockReset();
    client.listControllerAuditEvents.mockReset();
  });

  it("reads logs for one controller and keeps coverage, identity and truncation", async () => {
    client.getControllerLogs.mockResolvedValue(
      create(GetControllerLogsResponseSchema, {
        entries: [
          {
            time: timestampFromDate(new Date("2026-11-14T10:00:00Z")),
            severity: ControllerLogSeverity.WARN,
            component: "plant",
            message: "pump slow",
          },
          { severity: ControllerLogSeverity.ERROR, component: "", message: "x", messageTruncated: true },
        ],
        limit: 1000,
        coverage: ControllerLogCoverage.INTERRUPTED,
        interruption: "the stream failed: reset",
        controllerId: "ct1-b",
        controllerIds: ["ct1-a", "ct1-b"],
        readAt: timestampFromDate(readAt),
      }),
    );
    const snapshot = await source.readLogs!({ limit: 1000, minSeverity: "warn", controllerId: "ct1-b" }, signal);

    expect(client.getControllerLogs).toHaveBeenCalledWith(
      {
        deviceIdentifier: "ct1/a",
        controllerId: "ct1-b",
        minSeverity: ControllerLogSeverity.WARN,
        component: "",
        limit: 1000,
      },
      { signal },
    );
    expect(snapshot).toEqual({
      entries: [
        {
          timestamp: "2026-11-14T10:00:00.000Z",
          severity: "warn",
          component: "plant",
          message: "pump slow",
          truncated: undefined,
        },
        { timestamp: undefined, severity: "error", component: "", message: "x", truncated: true },
      ],
      limit: 1000,
      coverage: { state: "interrupted", reason: "the stream failed: reset" },
      controllerId: "ct1-b",
      controllerIds: ["ct1-a", "ct1-b"],
      fetchedAt: readAt.toISOString(),
    });
  });

  it.each([
    [ControllerLogCoverage.RETAINED, { state: "retained" }],
    [ControllerLogCoverage.LIMIT_REACHED, { state: "limit-reached" }],
    [ControllerLogCoverage.NO_RESPONSE, { state: "no-response" }],
    [ControllerLogCoverage.UNSPECIFIED, { state: "interrupted", reason: "the read ended early" }],
  ])("maps coverage %s without overstating completeness", async (coverage, expected) => {
    client.getControllerLogs.mockResolvedValue(create(GetControllerLogsResponseSchema, { coverage, limit: 10 }));
    expect((await source.readLogs!({ limit: 10 }, signal)).coverage).toEqual(expected);
  });

  it("lists audit events in the requested window and keeps attribution", async () => {
    client.listControllerAuditEvents.mockResolvedValue(
      create(ListControllerAuditEventsResponseSchema, {
        events: [
          {
            id: "42",
            actor: "op@bridge",
            source: "grpc:10.0.0.2",
            node: "ct1-a",
            action: "SetBreaker",
            result: "error",
            error: "interlock",
            detailsJson: "{}",
            truncated: true,
          },
        ],
        limit: 1000,
        limitReached: true,
        controllerId: "ct1-a",
        controllerIds: ["ct1-a"],
        readAt: timestampFromDate(readAt),
      }),
    );
    const page = await source.listAuditEvents!(
      { start: "2026-11-13T10:00:00.000Z", end: "2026-11-14T10:00:00.000Z", limit: 1000, result: "error" },
      signal,
    );

    const [request] = client.listControllerAuditEvents.mock.calls[0];
    expect(request).toMatchObject({ deviceIdentifier: "ct1/a", controllerId: "", limit: 1000, result: "error" });
    expect(timestampDate(request.start).toISOString()).toBe("2026-11-13T10:00:00.000Z");
    expect(timestampDate(request.end).toISOString()).toBe("2026-11-14T10:00:00.000Z");
    expect(page.events[0]).toMatchObject({
      id: "42",
      actor: "op@bridge",
      node: "ct1-a",
      error: "interlock",
      details: "{}",
      truncated: true,
    });
    expect(page).toMatchObject({ limitReached: true, controllerId: "ct1-a", fetchedAt: readAt.toISOString() });
  });

  it("reports failures to the session handler and rethrows them", async () => {
    const error = new ConnectError("Upgrade this container's Fleet Node", Code.FailedPrecondition);
    client.getControllerLogs.mockRejectedValue(error);
    await expect(source.readLogs!({ limit: 1 }, signal)).rejects.toBe(error);
    expect(handleAuthErrors).toHaveBeenCalledWith({ error });
  });
});
