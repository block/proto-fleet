import { create, fromJsonString, toJsonString } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { type Route } from "@playwright/test";
import { expect, test } from "../fixtures/pageFixtures";
import {
  ListServerLogsRequestSchema,
  ListServerLogsResponseSchema,
  LogEntrySchema,
  LogLevel,
} from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

const serverLogsRpcPattern = /ServerLogService\/ListServerLogs/;
const loadErrorMessage = "Polling failed for test";
const exportErrorMessage = "Export failed for test";

const initialEntries = [
  createServerLogEntry({
    id: 1n,
    level: LogLevel.INFO,
    message: "server booted",
    source: "fleetd",
    time: new Date("2026-01-01T12:00:00Z"),
  }),
  createServerLogEntry({
    id: 2n,
    level: LogLevel.WARN,
    message: "request completed",
    source: "http",
    time: new Date("2026-01-01T12:00:05Z"),
    attrs: [{ key: "request_id", value: "req-123" }],
  }),
];

const appendedEntry = createServerLogEntry({
  id: 3n,
  level: LogLevel.ERROR,
  message: "background sweep failed",
  source: "scheduler",
  time: new Date("2026-01-01T12:00:10Z"),
  attrs: [{ key: "job", value: "retention" }],
});

test.describe("Proto Fleet - Server Logs", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Page loads, polling appends new rows, and export starts a CSV download", async ({
    commonSteps,
    page,
    serverLogsPage,
  }) => {
    const pollSinceIds: bigint[] = [];

    await page.route(serverLogsRpcPattern, async (route) => {
      const request = parseServerLogsRequest(route);

      if (request.limit === 5000) {
        return fulfillServerLogs(route, [...initialEntries, appendedEntry], 3n);
      }

      pollSinceIds.push(request.sinceId);

      if (request.sinceId === 0n) {
        return fulfillServerLogs(route, initialEntries, 2n);
      }

      if (request.sinceId === 2n) {
        return fulfillServerLogs(route, [appendedEntry], 3n);
      }

      return fulfillServerLogs(route, [], 3n);
    });

    await commonSteps.loginAsAdmin();

    await test.step("Open Server Logs and validate the initial render", async () => {
      await serverLogsPage.navigateToServerLogsSettings();
      await serverLogsPage.validateServerLogsPageOpened();
      await serverLogsPage.waitForLogRowCount(2);
      await serverLogsPage.validateLogRowVisible("fleetd server booted");
      await serverLogsPage.validateLogRowVisible("http request completed request_id=req-123");
      expect(pollSinceIds[0]).toBe(0n);
    });

    await test.step("Wait for the next poll to append a new log row", async () => {
      await serverLogsPage.waitForLogRowCount(3);
      await serverLogsPage.validateLogRowVisible("scheduler background sweep failed job=retention");
      expect(pollSinceIds.slice(0, 2)).toEqual([0n, 2n]);
    });

    await test.step("Export the buffered logs and validate the download starts", async () => {
      const exportRequestPromise = page.waitForRequest((request) => {
        if (!request.url().match(serverLogsRpcPattern)) {
          return false;
        }

        const payload = fromJsonString(ListServerLogsRequestSchema, request.postData() ?? "{}");
        return payload.limit === 5000 && payload.sinceId === 0n;
      });
      const downloadPromise = page.waitForEvent("download");

      await serverLogsPage.clickExport();

      await exportRequestPromise;
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toMatch(/server-logs.*\.csv$/i);
    });
  });

  test("Load failures surface the server logs error callout", async ({ commonSteps, page, serverLogsPage }) => {
    await page.route(serverLogsRpcPattern, async (route) => {
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ code: "unavailable", message: loadErrorMessage }),
      });
    });

    await commonSteps.loginAsAdmin();

    await serverLogsPage.navigateToServerLogsSettings();
    await serverLogsPage.validateServerLogsPageOpened();
    await serverLogsPage.validateFetchErrorCallout(loadErrorMessage);
  });

  test("Export failures surface the export error callout", async ({ commonSteps, page, serverLogsPage }) => {
    await page.route(serverLogsRpcPattern, async (route) => {
      const request = parseServerLogsRequest(route);

      if (request.limit === 5000) {
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ code: "unavailable", message: exportErrorMessage }),
        });
      }

      return fulfillServerLogs(route, initialEntries, 2n);
    });

    await commonSteps.loginAsAdmin();

    await serverLogsPage.navigateToServerLogsSettings();
    await serverLogsPage.validateServerLogsPageOpened();
    await serverLogsPage.waitForLogRowCount(2);

    await serverLogsPage.clickExport();
    await serverLogsPage.validateExportErrorCallout(exportErrorMessage);
  });
});

function createServerLogEntry({
  id,
  level,
  message,
  source,
  time,
  attrs = [],
}: {
  id: bigint;
  level: LogLevel;
  message: string;
  source: string;
  time: Date;
  attrs?: Array<{ key: string; value: string }>;
}) {
  return create(LogEntrySchema, {
    id,
    level,
    message,
    source,
    attrs,
    time: create(TimestampSchema, {
      seconds: BigInt(Math.floor(time.getTime() / 1000)),
      nanos: 0,
    }),
  });
}

function parseServerLogsRequest(route: Route) {
  return fromJsonString(ListServerLogsRequestSchema, route.request().postData() ?? "{}");
}

function fulfillServerLogs(route: Route, entries: typeof initialEntries, latestId: bigint) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: toJsonString(
      ListServerLogsResponseSchema,
      create(ListServerLogsResponseSchema, {
        entries,
        latestId,
        bufferSize: entries.length,
        truncated: false,
      }),
    ),
  });
}
