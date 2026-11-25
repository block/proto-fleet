import type { BrowserContext } from "@playwright/test";
import { createHash } from "node:crypto";
import { clientA, mockClientWorkspace } from "./clientWorkspaceMocks";

export const reportingOriginalId = "55555555-5555-4555-8555-555555555555";
export const reportingCorrectionId = "66666666-6666-4666-8666-666666666666";
const period = {
  kind: "monthly",
  start: "2026-09-01T00:00:00Z",
  end: "2026-10-01T00:00:00Z",
  asOf: "2026-10-02T00:00:00Z",
  timezone: "UTC",
};
const original = {
  reportId: reportingOriginalId,
  accountId: clientA,
  accountName: "Customer A",
  seriesId: "monthly-september",
  number: "1",
  revision: "3",
  period,
  status: "issued",
  issuedAt: "2026-10-03T00:00:00Z",
  exception: "Contract definitions awaiting approval",
  result: {
    complete: false,
    metrics: {
      monthly_uptime: { status: "unavailable", unit: "%", causes: ["missing_G1"] },
      metered_energy_kwh: {
        status: "provisional",
        value: "100.25",
        exact: "401/4",
        unit: "kWh",
        causes: ["source_certification_pending"],
        sources: ["Reviewed meter worksheet"],
      },
    },
  },
};
const corrected = {
  ...original,
  reportId: reportingCorrectionId,
  number: "2",
  correctionOf: reportingOriginalId,
  correctionReason: "Corrected meter evidence",
  result: {
    ...original.result,
    metrics: {
      ...original.result.metrics,
      metered_energy_kwh: { ...original.result.metrics.metered_energy_kwh, value: "101.25", exact: "405/4" },
    },
  },
};

function envelope(flags: number, message: unknown) {
  const payload = Buffer.from(JSON.stringify(message));
  const header = Buffer.alloc(5);
  header.writeUInt8(flags, 0);
  header.writeUInt32BE(payload.length, 1);
  return Buffer.concat([header, payload]);
}

/** Complements real-database RPC tests with deterministic client/browser contracts. */
export async function mockReportingWorkspace(
  context: BrowserContext,
  preview: boolean,
  { administrator = false }: { administrator?: boolean } = {},
) {
  const workspace = await mockClientWorkspace(context, preview, {
    accountRole: administrator ? "ACCOUNT_ROLE_ADMINISTRATOR" : "ACCOUNT_ROLE_VIEWER",
  });
  let denied = false;
  let generated: Record<string, unknown> | undefined;
  const calls: { method: string; account: string | undefined; body: Record<string, unknown> }[] = [];
  await context.route("**/api-proxy/hosted.v1.HostedAccountService/*", async (route) => {
    const request = route.request();
    const method = new URL(request.url()).pathname.split("/").pop()!;
    if (
      ![
        "ListAccountReports",
        "GetAccountReport",
        "ExportAccountReport",
        "ListHostReportingAccounts",
        "ListAccountAnalyses",
        "GenerateAccountAnalysis",
        "GetAccountAnalysis",
        "ExportAccountAnalysis",
        "DeleteAccountAnalysis",
      ].includes(method)
    )
      return route.fallback();
    if (method === "ListHostReportingAccounts")
      return route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
    const raw = request.postDataBuffer()!;
    const body = (
      method.startsWith("Export")
        ? JSON.parse(raw.subarray(5, 5 + raw.readUInt32BE(1)).toString())
        : request.postDataJSON()
    ) as Record<string, unknown>;
    calls.push({ method, account: request.headers()["x-fleet-account-id"], body });
    if (denied)
      return route.fulfill({
        status: 403,
        contentType: "application/json",
        body: JSON.stringify({ code: "permission_denied", message: "Report access removed" }),
      });
    if (method === "DeleteAccountAnalysis") {
      generated = undefined;
      return route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
    }
    if (method === "ListAccountAnalyses")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ analyses: generated ? [generated] : [] }),
      });
    if (method === "GenerateAccountAnalysis") {
      generated = {
        analysisId: "77777777-7777-4777-8777-777777777777",
        accountId: clientA,
        ...body,
        createdAt: "2026-11-02T00:00:00Z",
        siteIds: ["1"],
        limitations: ["Earlier history is unavailable."],
        tables: [
          {
            key: "performance",
            title: "Historical performance",
            columns: ["Bucket start", "Hashrate (TH/s)", "Reporting miners"],
            rows: [{ cells: ["2026-10-01T00:00:00Z", "145.5", "2"] }],
          },
        ],
      };
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ analysis: generated }),
      });
    }
    if (method === "GetAccountAnalysis")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ analysis: generated }),
      });
    if (method === "ExportAccountAnalysis") {
      const csv = Buffer.from("Bucket start,Hashrate (TH/s),Reporting miners\n2026-10-01T00:00:00Z,145.5,2\n");
      return route.fulfill({
        status: 200,
        contentType: "application/connect+json",
        body: Buffer.concat([
          envelope(0, {
            chunk: csv.toString("base64"),
            filename: "analysis.csv",
            mediaType: "text/csv",
            sha256: createHash("sha256").update(csv).digest("hex"),
          }),
          envelope(2, {}),
        ]),
      });
    }
    if (method === "ListAccountReports")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ reports: [corrected] }),
      });
    if (method === "GetAccountReport")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ report: body.reportId === reportingOriginalId ? original : corrected }),
      });
    const html = Buffer.from("<!doctype html><html><body>Frozen issued report version 2</body></html>");
    return route.fulfill({
      status: 200,
      contentType: "application/connect+json",
      body: Buffer.concat([
        envelope(0, {
          chunk: html.toString("base64"),
          filename: "report.html",
          mediaType: "text/html",
          sha256: createHash("sha256").update(html).digest("hex"),
        }),
        envelope(2, {}),
      ]),
    });
  });
  return {
    workspace,
    calls,
    revoke: () => {
      denied = true;
    },
  };
}

/** A notification target is older than the queue's initial visible page. */
export async function mockReportingQueueTarget(context: BrowserContext) {
  const workspace = await mockClientWorkspace(context, true);
  const queueId = "88888888-8888-4888-8888-888888888888";
  const recent = Array.from({ length: 50 }, (_, index) => ({
    accountId: clientA,
    queueId: `recent-${index}`,
    accountName: `Recent client ${index}`,
    notificationStatus: "pending",
    deadlines:
      index < 2
        ? {
            contract: {
              date: index === 0 ? "2026-12-01" : "2026-12-02",
              status: index === 0 ? "provisional" : "authoritative",
            },
          }
        : undefined,
  }));
  const target = {
    queueId,
    accountId: clientA,
    accountName: "Older reporting obligation",
    generation: "9",
    canAcknowledge: true,
    canRetry: true,
    notificationStatus: "exhausted",
    period: {
      kind: "monthly",
      timezone: "UTC",
      start: "2026-08-01T00:00:00Z",
      end: "2026-09-01T00:00:00Z",
      asOf: "2026-09-02T00:00:00Z",
    },
  };
  const calls: { method: string; body: Record<string, unknown> }[] = [];
  await context.route("**/api-proxy/hosted.v1.HostedAccountService/*", async (route) => {
    const method = new URL(route.request().url()).pathname.split("/").pop()!;
    let result: unknown;
    const body = route.request().postDataJSON() as Record<string, unknown>;
    switch (method) {
      case "ListHostReportingAccounts":
        result = { accounts: [{ accountId: clientA, name: "Customer A", canCreate: true }] };
        break;
      case "ListHostReportQueue":
        result = body.pageToken ? { items: [target] } : { items: recent, nextPageToken: "older-page" };
        break;
      case "GetHostReportingSetup":
        result = {
          setup: {
            accountId: clientA,
            name: "Customer A",
            canCreate: true,
            owners: [{ userId: "operator", username: "Casey" }],
            input: {
              revision: "2",
              configuration: { timezone: "UTC", reportingYearMonth: 1, reportingYearDay: 1 },
            },
          },
        };
        break;
      case "ListHostReports":
        result = {};
        break;
      case "AcknowledgeHostReportQueue":
        result = {};
        break;
      default:
        return route.fallback();
    }
    calls.push({ method, body });
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(result) });
  });
  return { workspace, queueId, calls };
}

export async function mockPublishedHostReport(context: BrowserContext) {
  const { workspace } = await mockReportingQueueTarget(context);
  const sources = JSON.stringify({
    schema: "hosted-report-source-inputs/v1",
    period,
    inputs: { meters: [{ id: "shared-meter", kWh: "200.5", allocationFraction: "1/2" }] },
  });
  const downloads: Record<string, unknown>[] = [];
  await context.route("**/api-proxy/hosted.v1.HostedAccountService/*", async (route) => {
    const method = new URL(route.request().url()).pathname.split("/").pop()!;
    if (method === "ExportHostReport") {
      const raw = route.request().postDataBuffer()!;
      downloads.push(JSON.parse(raw.subarray(5, 5 + raw.readUInt32BE(1)).toString()));
      return route.fulfill({
        status: 200,
        contentType: "application/connect+json",
        body: Buffer.concat([
          envelope(0, {
            chunk: Buffer.from(sources).toString("base64"),
            filename: "source-inputs.json",
            mediaType: "application/json",
            sha256: createHash("sha256").update(sources).digest("hex"),
          }),
          envelope(2, {}),
        ]),
      });
    }
    if (method === "GetHostReport")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ report: { report: corrected, inputRevision: "1" } }),
      });
    if (method === "ListHostReports")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ reports: [{ report: corrected }] }),
      });
    return route.fallback();
  });
  return { workspace, downloads, sources };
}

export async function mockReportingInputRegisters(context: BrowserContext) {
  const { workspace } = await mockReportingQueueTarget(context);
  let revision = 1;
  let loseResponse = false;
  const configuration = { timezone: "UTC", reportingYearMonth: 1, reportingYearDay: 1 };
  const facts = new Map<string, Record<string, unknown>>(
    Array.from({ length: 4 }, (_, i) => [
      `coverage-${i}`,
      {
        id: `coverage-${i}`,
        allocationIntervalId: "9",
        kind: 5,
        assetId: "11",
        siteId: "1",
        containerId: "11",
        orderId: "order",
        start: "2026-09-01T00:00:00Z",
        end: "2026-10-01T00:00:00Z",
        capacityThs: "100",
        sourceRef: "Reviewed original commercial evidence",
      },
    ]),
  );
  const calls: { method: string; body: Record<string, unknown> }[] = [];
  const metadata = () => ({
    revision: String(revision),
    configuration,
    registerCounts: [{ kind: "REPORTING_FACT_KIND_COVERAGE", count: String(facts.size) }],
  });
  await context.route("**/api-proxy/hosted.v1.HostedAccountService/*", async (route) => {
    const method = new URL(route.request().url()).pathname.split("/").pop()!;
    if (!["GetHostReportingSetup", "ListHostReportingFacts", "MutateHostReportingInputs"].includes(method))
      return route.fallback();
    const body = route.request().postDataJSON() as Record<string, unknown>;
    calls.push({ method, body });
    let result: unknown;
    if (method === "GetHostReportingSetup")
      result = { setup: { accountId: clientA, name: "Customer A", canCreate: true, input: metadata() } };
    else if (method === "ListHostReportingFacts") {
      const ordered = [...facts.keys()].sort();
      const after = body.cursor ? ordered.indexOf(String(body.cursor)) + 1 : 0;
      const keys = ordered.slice(after, after + 2);
      result = {
        revision: String(revision),
        facts: keys.map((id) => ({ coverage: facts.get(id) })),
        nextCursor: after + keys.length < ordered.length ? keys[keys.length - 1] : "",
      };
    } else {
      if (String(body.expectedRevision) !== String(revision))
        return route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({ code: "aborted", message: "Inputs changed; refresh before continuing" }),
        });
      for (const mutation of (body.mutations ?? []) as Record<string, unknown>[]) {
        if (mutation.delete === true) facts.delete(String(mutation.id));
        else {
          const payload = Object.values(mutation.upsert as Record<string, Record<string, unknown>>)[0];
          facts.set(String(mutation.id), payload);
        }
      }
      if (body.configuration) Object.assign(configuration, body.configuration);
      revision++;
      if (loseResponse) {
        loseResponse = false;
        return route.abort("failed");
      }
      result = { input: metadata() };
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(result) });
  });
  return {
    workspace,
    calls,
    advanceRevision: () => {
      revision++;
    },
    loseNextResponse: () => {
      loseResponse = true;
    },
  };
}
