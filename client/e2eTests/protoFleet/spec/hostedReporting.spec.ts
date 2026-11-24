import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { clientA } from "../helpers/clientWorkspaceMocks";
import {
  mockPublishedHostReport,
  mockReportingInputRegisters,
  mockReportingQueueTarget,
  mockReportingWorkspace,
  reportingCorrectionId,
  reportingOriginalId,
} from "../helpers/reportingWorkspaceMocks";
import { ReportingInputsPage, ReportingPage } from "../pages/reporting";

test.beforeEach(async ({ context }) => {
  await context.addInitScript(() => {
    Object.defineProperty(window, "showSaveFilePicker", { value: undefined, configurable: true });
  });
});

test.describe("Issued hosted reports", () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  for (const preview of [false, true]) {
    test(`${preview ? "read-only host preview" : "client"} inspects corrections and scoped frozen downloads`, async ({
      page,
      context,
    }) => {
      const mock = await mockReportingWorkspace(context, preview);
      const reports = new ReportingPage(page);
      await reports.open(clientA);
      await expect(reports.detail()).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Generate report", exact: true })).toHaveCount(0);
      const row = reports.row("Monthly service report");
      await expect(page.getByRole("columnheader", { name: "Report type", exact: true })).toBeVisible();
      await expect(row.getByRole("button")).toHaveCount(0);
      await expect(row.getByRole("link")).toHaveCount(0);
      await row.getByRole("cell", { name: "Official report", exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`report=${reportingCorrectionId}`));
      await expect(reports.detail().getByText(/Data is incomplete\./)).toBeVisible();
      await expect(reports.totals().getByText("101.25 kWh", { exact: true })).toBeVisible();
      await expect(reports.detail().getByRole("button", { name: "Report version", exact: true })).toHaveCount(0);
      await expect(
        reports.detail().getByText("Replacement reason: Corrected meter evidence", { exact: true }),
      ).toBeVisible();
      await expect(page.getByRole("button", { name: "Publish report", exact: true })).toHaveCount(0);
      await expect(page.getByText("Download source inputs", { exact: true })).toHaveCount(0);
      await test.info().attach("Issued report overview", {
        body: await page.screenshot({ path: test.info().outputPath("issued-report.png"), fullPage: true }),
        contentType: "image/png",
      });

      await reports.detail().getByRole("button", { name: "Evidence", exact: true }).click();
      await reports
        .detail()
        .getByRole("region", { name: "Report evidence", exact: true })
        .getByRole("row")
        .filter({ has: page.getByRole("cell", { name: "Monthly uptime", exact: true }) })
        .click();
      const evidence = page.getByRole("dialog", { name: "Evidence details", exact: true });
      await expect(
        evidence.getByText("Contract definitions and calculation methods have not been approved.", { exact: true }),
      ).toBeVisible();
      await evidence.getByRole("button", { name: "Close dialog", exact: true }).click();
      await reports.detail().getByRole("button", { name: "Results", exact: true }).click();

      await test.step("Original remains available and latest correction is explicit", async () => {
        await reports.previous();
        await expect(reports.detail().getByText("Replaced", { exact: true })).toBeVisible();
        await expect(reports.totals().getByText("100.25 kWh", { exact: true })).toBeVisible();
        await reports.latest();
        await expect(reports.totals().getByText("101.25 kWh", { exact: true })).toBeVisible();
      });

      await test.step("Download completes under the selected account", async () => {
        const download = await reports.downloadHTML();
        const file = test.info().outputPath("report.html");
        await download.saveAs(file);
        expect(await readFile(file, "utf8")).toContain("Frozen issued report version 2");
        expect(mock.calls.filter(({ method }) => method === "ExportAccountReport")).toMatchObject([
          { body: { reportId: reportingCorrectionId, name: "report.html" } },
        ]);
        const reads = mock.calls.filter(({ method }) => method === "GetAccountReport");
        expect(reads.map(({ body }) => body.reportId)).toEqual(
          expect.arrayContaining([reportingOriginalId, reportingCorrectionId]),
        );
        expect(mock.calls.every(({ account, body }) => account === clientA && body.accountId === clientA)).toBe(true);
      });

      await test.step("Revoked reads discard retained report content", async () => {
        mock.revoke();
        await reports.refresh();
        await expect(page.getByText("Report access removed", { exact: true })).toBeVisible();
        await expect(page.getByText("Replacement reason: Corrected meter evidence", { exact: true })).toHaveCount(0);
      });

      expect(mock.workspace.unexpected).toEqual([]);
    });
  }

  test("client generates an ad hoc report, reopens it from the shared library and downloads CSV", async ({
    page,
    context,
  }) => {
    const mock = await mockReportingWorkspace(context, false, { administrator: true });
    const reports = new ReportingPage(page);
    await reports.open(clientA);
    await page.getByRole("button", { name: "Generate report", exact: true }).click();
    const form = page.getByRole("dialog", { name: "Generate report", exact: true });
    await form.getByLabel("Report name", { exact: true }).fill("October performance");
    await form.getByLabel("Start date (UTC)", { exact: true }).fill("2026-10-01");
    await form.getByLabel("End date (UTC)", { exact: true }).fill("2026-10-01");
    await form.getByRole("button", { name: "Generate report", exact: true }).click();
    const detail = page.getByRole("region", { name: "Generated report", exact: true });
    await expect(detail.getByRole("heading", { name: "October performance", exact: true })).toBeVisible();
    await expect(detail.getByText("145.5", { exact: true })).toBeVisible();
    await expect(detail.getByText("Earlier history is unavailable.", { exact: true })).toBeVisible();
    await page
      .getByRole("navigation", { name: "Breadcrumb", exact: true })
      .getByRole("link", { name: "Reports", exact: true })
      .click();
    await expect(page).toHaveURL(new RegExp(`/accounts/${clientA}/reports$`));
    await page.getByRole("button", { name: "Report type", exact: true }).click();
    await page.getByRole("option", { name: "Miner performance", exact: true }).click();
    await expect(reports.row("Monthly service report")).toHaveCount(0);
    await page.getByRole("textbox", { name: "Search reports", exact: true }).fill("October performance");
    const analysis = reports.row("October performance");
    await expect(analysis.getByRole("cell", { name: "Ad hoc · Miner performance", exact: true })).toBeVisible();
    await expect(analysis.getByRole("button")).toHaveCount(0);
    await analysis.focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/analysis=77777777-7777-4777-8777-777777777777/);
    const downloading = page.waitForEvent("download");
    await detail.getByRole("button", { name: "Download CSV", exact: true }).click();
    const download = await downloading;
    const file = test.info().outputPath("analysis.csv");
    await download.saveAs(file);
    expect(await readFile(file, "utf8")).toContain("145.5,2");
    expect(mock.calls.find(({ method }) => method === "GenerateAccountAnalysis")?.body).toMatchObject({
      accountId: clientA,
      kind: "miner_performance",
      start: "2026-10-01T00:00:00Z",
      end: "2026-10-02T00:00:00Z",
    });

    await test.step("Administrator confirms permanent deletion", async () => {
      await detail.getByRole("button", { name: "Delete report", exact: true }).click();
      const confirmation = page.getByRole("dialog", { name: "Delete saved report?", exact: true });
      await expect(confirmation.getByText(/This cannot be undone/)).toBeVisible();
      await confirmation.getByRole("button", { name: "Cancel", exact: true }).click();
      expect(mock.calls.filter(({ method }) => method === "DeleteAccountAnalysis")).toHaveLength(0);
      await detail.getByRole("button", { name: "Delete report", exact: true }).click();
      await expect(confirmation).toHaveCSS("opacity", "1");
      await expect(confirmation.locator("..")).toHaveCSS("opacity", "1");
      await test.info().attach("Permanent analysis deletion", {
        body: await page.screenshot({ path: test.info().outputPath("delete-analysis.png"), fullPage: true }),
        contentType: "image/png",
      });
      await confirmation.getByRole("button", { name: "Permanently delete", exact: true }).click();
      await expect(detail).toHaveCount(0);
      await expect(page.getByRole("heading", { name: "Reports", exact: true })).toBeVisible();
      await reports.refresh();
      await expect(reports.row("October performance")).toHaveCount(0);
      await expect(reports.row("Monthly service report")).toBeVisible();
      expect(mock.calls.filter(({ method }) => method === "DeleteAccountAnalysis")).toMatchObject([
        { account: clientA, body: { accountId: clientA, analysisId: "77777777-7777-4777-8777-777777777777" } },
      ]);
    });

    expect(mock.workspace.unexpected).toEqual([]);
  });
});

test("host downloads frozen source inputs from the standard report actions menu", async ({ page, context }) => {
  const mock = await mockPublishedHostReport(context);
  const reports = new ReportingPage(page);
  await reports.openHost(clientA, reportingCorrectionId);
  const download = await reports.downloadSources();
  expect(download.suggestedFilename()).toBe("source-inputs.json");
  const file = test.info().outputPath("source-inputs.json");
  await download.saveAs(file);
  expect(await readFile(file, "utf8")).toBe(mock.sources);
  expect(mock.downloads).toEqual([{ accountId: clientA, reportId: reportingCorrectionId, name: "source-inputs.json" }]);
  expect(mock.workspace.unexpected).toEqual([]);
  await test.info().attach("Host published report", {
    body: await page.screenshot({ path: test.info().outputPath("host-report.png"), fullPage: true }),
    contentType: "image/png",
  });
});

test("notification links resolve older queue obligations without expanding the visible first page", async ({
  page,
  context,
}) => {
  const mock = await mockReportingQueueTarget(context);
  await page.goto(`/accounts/reports?account=${clientA}&queue=${mock.queueId}`);
  await page.getByText("Reporting activity", { exact: true }).click();
  await expect(page.getByRole("button", { name: "Retry notification", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Acknowledge", exact: true }).click();
  const acknowledgment = page.getByRole("dialog", { name: "Acknowledge reporting obligation", exact: true });
  await acknowledgment.getByLabel("Reason", { exact: true }).fill("Reporting owner assigned");
  await acknowledgment.getByRole("button", { name: "Acknowledge", exact: true }).click();
  await expect(acknowledgment).toHaveCount(0);
  expect(mock.calls.find(({ method }) => method === "AcknowledgeHostReportQueue")?.body).toMatchObject({
    accountId: clientA,
    queueId: mock.queueId,
    expectedGeneration: "9",
  });
  await page.getByRole("button", { name: "Create report", exact: true }).click();
  const preparation = page.getByRole("dialog", { name: "Create report", exact: true });
  await expect(preparation.getByLabel("Period start (UTC)", { exact: true })).toHaveValue("2026-08-01T00:00");
  await expect(preparation.getByLabel("Period end (exclusive) (UTC)", { exact: true })).toHaveValue("2026-09-01T00:00");
  await expect(preparation).toHaveCSS("opacity", "1");
  await expect(preparation.locator("..")).toHaveCSS("opacity", "1");
  await test.info().attach("Resolved notification period", {
    body: await page.screenshot({ path: test.info().outputPath("resolved-queue-period.png"), fullPage: true }),
    contentType: "image/png",
  });
  await preparation.getByRole("button", { name: "Close dialog", exact: true }).click();
  await page.getByRole("button", { name: "Work queue", exact: true }).click();
  await expect(page.getByRole("navigation", { name: "Breadcrumb", exact: true })).toHaveCount(0);
  await expect(page.getByRole("columnheader", { name: "Next action", exact: true })).toHaveCount(0);
  await expect(page.getByRole("row")).toHaveCount(51);
  await expect(page.getByRole("cell", { name: "2026-12-01", exact: true })).toHaveCount(0);
  await expect(page.getByRole("cell", { name: "2026-12-02", exact: true })).toBeVisible();
  await test.info().attach("Qualified queue due dates", {
    body: await page.screenshot({ path: test.info().outputPath("qualified-queue-dates.png") }),
    contentType: "image/png",
  });
  await expect(page.getByRole("cell", { name: "Older reporting obligation", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Load more obligations", exact: true }).click();
  await expect(page.getByRole("cell", { name: "Older reporting obligation", exact: true })).toBeVisible();
  const obligation = page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name: "Older reporting obligation", exact: true }) });
  await expect(obligation.getByRole("cell", { name: "Monthly · Aug 2026", exact: true })).toBeVisible();
  await expect(obligation.getByRole("button")).toHaveCount(0);
  await expect(obligation.getByRole("link")).toHaveCount(0);
  await obligation.getByRole("cell", { name: "Monthly · Aug 2026", exact: true }).click();
  await expect(page.getByRole("button", { name: "Create report", exact: true })).toBeVisible();
  expect(mock.workspace.unexpected).toEqual([]);
});

test.describe("Bounded reporting inputs", () => {
  test.use({ storageState: { cookies: [], origins: [] }, colorScheme: "dark" });

  test("host pages registers, edits one row and confirms a deletion", async ({ page, context }) => {
    const mock = await mockReportingInputRegisters(context);
    const inputs = new ReportingInputsPage(page);
    await inputs.open(clientA);
    await expect(inputs.facts().getByText("coverage-0", { exact: true })).toBeVisible();
    expect(mock.calls.filter((call) => call.method === "ListHostReportingFacts")).toMatchObject([
      { body: { kind: "REPORTING_FACT_KIND_COVERAGE", expectedRevision: "1", pageSize: 50 } },
    ]);
    await inputs.next();
    await expect(inputs.facts().getByText("coverage-2", { exact: true })).toBeVisible();
    const edit = await inputs.edit("coverage-2");
    await edit.getByLabel("Change evidence reference", { exact: true }).fill("Reviewed correction");
    await edit.getByRole("button", { name: "Save fact", exact: true }).click();
    await expect(edit).toHaveCount(0);
    expect(mock.calls.filter((call) => call.method === "MutateHostReportingInputs")).toMatchObject([
      {
        body: { expectedRevision: "1", mutations: [{ id: "coverage-2", upsert: { coverage: { id: "coverage-2" } } }] },
      },
    ]);
    await expect(inputs.facts().getByText("coverage-0", { exact: true })).toBeVisible();
    const confirmation = await inputs.remove("coverage-0");
    expect(mock.calls.filter((call) => call.method === "MutateHostReportingInputs")).toHaveLength(1);
    await confirmation.getByLabel("Change evidence reference", { exact: true }).fill("Duplicate removed");
    await confirmation.getByRole("button", { name: "Delete fact", exact: true }).click();
    await expect(confirmation).toHaveCount(0);
    await expect(inputs.facts().getByText("coverage-0", { exact: true })).toHaveCount(0);
    expect(mock.calls.filter((call) => call.method === "MutateHostReportingInputs")[1].body).toMatchObject({
      expectedRevision: "2",
      mutations: [{ id: "coverage-0", delete: true }],
    });
    await test.info().attach("Paged reporting register", {
      body: await page.screenshot({ path: test.info().outputPath("reporting-register.png"), fullPage: true }),
      contentType: "image/png",
    });
    expect(mock.workspace.unexpected).toEqual([]);
  });

  test("stale fact submission retains the draft for review without replaying it", async ({ page, context }) => {
    const mock = await mockReportingInputRegisters(context);
    const inputs = new ReportingInputsPage(page);
    await inputs.open(clientA);
    const edit = await inputs.edit("coverage-0");
    await edit.getByLabel("Change evidence reference", { exact: true }).fill("Keep my draft");
    mock.advanceRevision();
    await edit.getByRole("button", { name: "Save fact", exact: true }).click();
    await expect(edit.getByText(/Inputs changed; refresh before continuing/)).toBeVisible();
    await expect(edit.getByLabel("Change evidence reference", { exact: true })).toHaveValue("Keep my draft");
    await expect(edit.getByRole("button", { name: "Save fact", exact: true })).toBeDisabled();
    expect(mock.calls.filter((call) => call.method === "MutateHostReportingInputs")).toHaveLength(1);
    await test.info().attach("Stale reporting draft", {
      body: await page.screenshot({ path: test.info().outputPath("reporting-conflict.png"), fullPage: true }),
      contentType: "image/png",
    });
    expect(mock.workspace.unexpected).toEqual([]);
  });

  test("lost import response remains unknown and retains only the submitted batch for review", async ({
    page,
    context,
  }) => {
    const mock = await mockReportingInputRegisters(context);
    const inputs = new ReportingInputsPage(page);
    await inputs.open(clientA);
    mock.loseNextResponse();
    const form = await inputs.importRows(201);
    await expect(form.getByText(/The last submitted batch has an unknown outcome/)).toBeVisible();
    await expect(form.getByRole("status")).toContainText("0 rows confirmed committed");
    expect(mock.calls.filter((call) => call.method === "MutateHostReportingInputs")).toHaveLength(1);
    await form.getByRole("button", { name: "Done", exact: true }).click();
    await page.reload();
    await expect(inputs.facts().getByText(/The last submitted batch has an unknown outcome/)).toBeVisible();
    const downloading = page.waitForEvent("download");
    await inputs.facts().getByRole("button", { name: "Download submitted rows for review", exact: true }).click();
    const download = await downloading;
    const file = test.info().outputPath("submitted.json");
    await download.saveAs(file);
    expect(JSON.parse(await readFile(file, "utf8"))).toHaveLength(200);
    await test.info().attach("Unknown import outcome", {
      body: await page.screenshot({ path: test.info().outputPath("reporting-import-unknown.png"), fullPage: true }),
      contentType: "image/png",
    });
    expect(mock.workspace.unexpected).toEqual([]);
  });
});
