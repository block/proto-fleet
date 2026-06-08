import { fromJsonString } from "@bufbuild/protobuf";
import { expect, test } from "../fixtures/pageFixtures";
import { createServerLogEntry, fulfillServerLogs, parseServerLogsRequest } from "../helpers/serverLogsMocks";
import { ListServerLogsRequestSchema, LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

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
