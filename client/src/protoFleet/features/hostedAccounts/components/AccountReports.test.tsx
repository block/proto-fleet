import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { type AccountScope, AccountScopeContext } from "../AccountScope";
import AccountReports from "./AccountReports";
import { AccountAnalysisSchema } from "@/protoFleet/api/generated/hosted/v1/analysis_pb";
import { type Account, AccountRole, AccountSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { AccountReportSchema, ReportSummarySchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import * as utility from "@/shared/utils/utility";

afterEach(() => vi.restoreAllMocks());
let pollReports: () => Promise<void>;

const original = create(AccountReportSchema, {
  reportId: "original",
  seriesId: "series",
  number: 1n,
  status: "issued",
  period: { kind: "monthly", timezone: "UTC" },
  result: { metrics: { monthly_uptime: { status: "unavailable", causes: ["missing_G1"] } } },
});
const correction = create(AccountReportSchema, {
  ...original,
  reportId: "correction",
  number: 2n,
  correctionOf: "original",
  correctionReason: "Corrected meter evidence",
});
const savedAnalysis = create(AccountAnalysisSchema, {
  analysisId: "generated",
  name: "My performance",
  kind: "miner_performance",
  start: { seconds: 1790812800n },
  end: { seconds: 1790899200n },
  timezone: "UTC",
  createdAt: { seconds: 1791000000n },
  limitations: ["Earlier history unavailable"],
});
function mount(
  initialEntries = ["/?report=correction"],
  readOnly = false,
  withAnalysis = false,
  overrides: Partial<Pick<Account, "role" | "effectiveCapabilities" | "suspended">> = {},
) {
  const intervals = vi.spyOn(window, "setInterval");
  const cleared = vi.spyOn(window, "clearInterval");
  pollReports = async () => {
    const callbacks = intervals.mock.calls.flatMap(([handler, delay], index) => {
      const timer = intervals.mock.results[index];
      const active = timer?.type === "return" && !cleared.mock.calls.some(([id]) => id === timer.value);
      return delay === POLL_INTERVAL_MS && active && typeof handler === "function" ? [handler] : [];
    });
    await act(async () => {
      for (const poll of callbacks) poll();
    });
  };
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  const controller = new AbortController();
  const client = {
    listAccountAnalyses: vi.fn().mockResolvedValue({ analyses: withAnalysis ? [savedAnalysis] : [] }),
    deleteAccountAnalysis: vi.fn().mockResolvedValue({}),
    generateAccountAnalysis: vi.fn().mockResolvedValue({ analysis: { analysisId: "generated" } }),
    getAccountAnalysis: vi.fn().mockResolvedValue({
      analysis: {
        name: "My performance",
        requestId: expect.stringMatching(/^[0-9a-f-]{36}$/),
        kind: "miner_performance",
        start: { seconds: 1790812800n },
        end: { seconds: 1790899200n },
        timezone: "UTC",
        createdAt: { seconds: 1791000000n },
        limitations: ["Earlier history unavailable"],
        tables: [
          {
            key: "performance",
            title: "Performance history",
            columns: ["Time", "Hashrate"],
            rows: [{ cells: ["2026-10-01", "100 TH/s"] }],
          },
        ],
      },
    }),
    listAccountReports: vi.fn().mockResolvedValue({
      reports: [
        create(ReportSummarySchema, {
          reportId: "correction",
          seriesId: "series",
          number: 2n,
          period: original.period,
          status: "issued",
          correctionOf: "original",
        }),
      ],
    }),
    getAccountReport: vi.fn(async ({ reportId }: { reportId: string }) => ({
      report: reportId === "original" ? original : correction,
    })),
    exportAccountReport: vi.fn(async function* () {
      yield {
        chunk: new Uint8Array([1]),
        filename: "report.html",
        mediaType: "text/html",
        sha256: "4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a",
      };
    }),
  };
  const scope: AccountScope = {
    account: create(AccountSchema, {
      accountId: "client",
      role: AccountRole.ADMINISTRATOR,
      effectiveCapabilities: 33n,
      ...overrides,
    }),
    client: client as unknown as AccountScope["client"],
    signal: controller.signal,
    invalidateAccount: vi.fn(),
    refreshAccount: vi.fn(),
    readOnly,
  };
  const tree = () => (
    <MemoryRouter initialEntries={initialEntries}>
      <AccountScopeContext.Provider value={scope}>
        <AccountReports />
      </AccountScopeContext.Provider>
    </MemoryRouter>
  );
  const view = render(tree());
  return { client, scope, controller, view, rerenderScope: () => view.rerender(tree()) };
}
describe("issued account reports", () => {
  it("generates an ad hoc report with full UTC dates and opens its shared snapshot", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"]);
    await user.click(await screen.findByRole("button", { name: "Generate report" }));
    const form = within(screen.getByRole("dialog", { name: "Generate report" }));
    await user.clear(form.getByLabelText("Report name"));
    await user.type(form.getByLabelText("Report name"), "My performance");
    fireEvent.change(form.getByLabelText("Start date (UTC)"), { target: { value: "2026-10-01" } });
    fireEvent.change(form.getByLabelText("End date (UTC)"), { target: { value: "2026-10-01" } });
    client.listAccountAnalyses.mockResolvedValue({ analyses: [savedAnalysis] });
    await user.click(form.getByRole("button", { name: "Generate report" }));
    await screen.findByRole("region", { name: "Generated report" });
    expect(await screen.findByText("100 TH/s")).toBeInTheDocument();
    expect(screen.getByText("Earlier history unavailable")).toBeInTheDocument();
    expect(client.generateAccountAnalysis).toHaveBeenCalledWith(
      expect.objectContaining({
        accountId: "client",
        name: "My performance",
        requestId: expect.stringMatching(/^[0-9a-f-]{36}$/),
        kind: "miner_performance",
        timezone: "UTC",
        start: expect.objectContaining({ seconds: 1790812800n }),
        end: expect.objectContaining({ seconds: 1790899200n }),
      }),
      expect.anything(),
    );
    expect(screen.queryByRole("button", { name: "Publish report" })).not.toBeInTheDocument();
  });
  it("shows one shared error when both independent resources reject access", async () => {
    const { client } = mount(["/"]);
    await screen.findByRole("cell", { name: "Monthly service report" });
    const denied = new ConnectError("Report access removed", Code.PermissionDenied);
    client.listAccountReports.mockRejectedValue(denied);
    client.listAccountAnalyses.mockRejectedValue(denied);
    await pollReports();
    await screen.findByText("Report access removed");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
  });
  it("keeps official reports visible when the analysis library fails", async () => {
    const { client } = mount(["/"]);
    client.listAccountAnalyses.mockRejectedValue(new ConnectError("Analysis service unavailable", Code.Unavailable));
    await screen.findByRole("cell", { name: "Monthly service report" });
    await pollReports();
    await screen.findByText("Analysis service unavailable");
    expect(screen.getByRole("cell", { name: "Monthly service report" })).toBeInTheDocument();
  });
  it("does not claim a newly saved analysis was removed when its library refresh fails", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"]);
    await user.click(await screen.findByRole("button", { name: "Generate report" }));
    client.listAccountAnalyses.mockRejectedValue(new ConnectError("Analysis refresh unavailable", Code.Unavailable));
    await user.click(
      within(screen.getByRole("dialog", { name: "Generate report" })).getByRole("button", { name: "Generate report" }),
    );
    await screen.findByText("Analysis refresh unavailable");
    expect(screen.queryByText("This report is no longer available in your account library.")).not.toBeInTheDocument();
  });
  it("pages long analysis tables", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"], false, true);
    client.getAccountAnalysis.mockResolvedValue({
      analysis: {
        ...savedAnalysis,
        tables: [
          {
            key: "large",
            title: "Large table",
            columns: ["Record"],
            rows: Array.from({ length: 51 }, (_, index) => ({ cells: [`record-${index}`] })),
          },
        ],
      },
    });
    await user.click((await screen.findByText("My performance")).closest("tr")!);
    expect(await screen.findByText("record-0")).toBeInTheDocument();
    expect(screen.queryByText("record-50")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(screen.getByText("record-50")).toBeInTheDocument();
    expect(screen.queryByText("record-0")).not.toBeInTheDocument();
    await user.click(
      within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByRole("link", { name: "Reports" }),
    );
    expect(await screen.findByRole("cell", { name: "My performance" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Generated report" })).not.toBeInTheDocument();
  });
  it("loads additional analysis pages and refreshes without duplicating rows", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"]);
    client.listAccountAnalyses.mockImplementation(async (request: { pageToken?: string }) =>
      request.pageToken
        ? { analyses: [{ ...savedAnalysis, analysisId: "older", name: "Older performance" }], nextPageToken: "" }
        : { analyses: [savedAnalysis], nextPageToken: "older-page" },
    );
    await screen.findByRole("cell", { name: "Monthly service report" });
    await pollReports();
    await user.click(await screen.findByRole("button", { name: "Load more reports" }));
    await screen.findByText("Older performance");
    expect(client.listAccountAnalyses).toHaveBeenCalledWith(
      expect.objectContaining({ pageToken: "older-page", pageSize: 50 }),
      expect.anything(),
    );
    await pollReports();
    await waitFor(() => expect(screen.getAllByText("Older performance")).toHaveLength(1));
    expect(screen.getAllByText("My performance")).toHaveLength(1);
  });
  it("keeps one generation request identity across retries and bounds each call", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"]);
    await user.click(await screen.findByRole("button", { name: "Generate report" }));
    const form = within(screen.getByRole("dialog", { name: "Generate report" }));
    client.generateAccountAnalysis.mockRejectedValueOnce(new ConnectError("Try again", Code.Unavailable));
    await user.click(form.getByRole("button", { name: "Generate report" }));
    await screen.findByText("Try again");
    await user.click(form.getByRole("button", { name: "Generate report" }));
    await waitFor(() => expect(client.generateAccountAnalysis).toHaveBeenCalledTimes(2));
    const calls = client.generateAccountAnalysis.mock.calls;
    expect(calls[0][0].requestId).toBe(calls[1][0].requestId);
    expect(calls[0][1].timeoutMs).toBe(60_000);
  });
  it.each([
    ["viewer", AccountRole.VIEWER, 1n, false],
    ["pool operator", AccountRole.POOL_OPERATOR, 3n, false],
    ["ancestor access", AccountRole.UNSPECIFIED, 63n, false],
    ["administrator without generation permission", AccountRole.ADMINISTRATOR, 9n, false],
    ["administrator without view access", AccountRole.ADMINISTRATOR, 40n, false],
    ["read-only administrator", AccountRole.ADMINISTRATOR, 63n, true],
  ])("keeps generation out of %s access", async (_label, role, effectiveCapabilities, readOnly) => {
    mount(["/"], readOnly, false, { role, effectiveCapabilities });
    await screen.findByRole("cell", { name: "Monthly service report" });
    expect(screen.queryByRole("button", { name: "Generate report" })).not.toBeInTheDocument();
  });
  it.each([AccountRole.VIEWER, AccountRole.POOL_OPERATOR, AccountRole.UNSPECIFIED])(
    "keeps analysis reading and export for role %s without deletion",
    async (role) => {
      mount(["/?analysis=generated"], false, true, { role });
      await screen.findByText("100 TH/s");
      expect(screen.getByRole("button", { name: "Download CSV" })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Delete report" })).not.toBeInTheDocument();
    },
  );
  it("keeps analysis exports available in a read-only administrator preview without deletion", async () => {
    mount(["/?analysis=generated"], true, true);
    await screen.findByText("100 TH/s");
    expect(screen.getByRole("button", { name: "Download CSV" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete report" })).not.toBeInTheDocument();
  });
  it("keeps saved analysis reads, exports and admin deletion when generation is disabled", async () => {
    mount(["/?analysis=generated"], false, true, { effectiveCapabilities: 9n });
    await screen.findByText("100 TH/s");
    expect(screen.getByRole("button", { name: "Download CSV" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete report" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Generate report" })).not.toBeInTheDocument();
  });
  it("closes generation when the current account loses generation permission", async () => {
    const user = userEvent.setup();
    const { scope, rerenderScope, client } = mount(["/"]);
    await user.click(await screen.findByRole("button", { name: "Generate report" }));
    expect(screen.getByRole("dialog", { name: "Generate report" })).toBeInTheDocument();
    scope.account = create(AccountSchema, { ...scope.account, effectiveCapabilities: 9n });
    rerenderScope();
    expect(screen.queryByRole("dialog", { name: "Generate report" })).not.toBeInTheDocument();
    expect(client.generateAccountAnalysis).not.toHaveBeenCalled();
  });
  it("cancels permanent deletion without sending a mutation", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/?analysis=generated"], false, true);
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    const dialog = within(screen.getByRole("dialog", { name: "Delete saved report?" }));
    expect(dialog.getByText(/This cannot be undone/)).toHaveTextContent("The audit record is retained.");
    await user.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(client.deleteAccountAnalysis).not.toHaveBeenCalled();
    expect(screen.getByText("100 TH/s")).toBeInTheDocument();
  });
  it("permanently deletes a saved analysis and excludes stale library snapshots", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/?analysis=generated"], false, true);
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await screen.findByRole("heading", { name: "Reports", level: 1 });
    expect(client.deleteAccountAnalysis).toHaveBeenCalledWith(
      { accountId: "client", analysisId: "generated" },
      expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 30_000 }),
    );
    expect(screen.queryByText("My performance")).not.toBeInTheDocument();
    await pollReports();
    await waitFor(() => expect(client.listAccountAnalyses).toHaveBeenCalledTimes(3));
    expect(screen.queryByText("My performance")).not.toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Monthly service report" })).toBeInTheDocument();
  });
  it("keeps a failed deletion reviewable and permits an idempotent retry", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/?analysis=generated"], false, true);
    client.deleteAccountAnalysis.mockRejectedValueOnce(new ConnectError("Delete unavailable", Code.Unavailable));
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await screen.findByText("Delete unavailable");
    expect(screen.getByText("100 TH/s")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await screen.findByRole("heading", { name: "Reports", level: 1 });
    expect(client.deleteAccountAnalysis).toHaveBeenCalledTimes(2);
  });
  it("discards stale analysis content and blocks deletion after live permission denial", async () => {
    const user = userEvent.setup();
    const { client, scope } = mount(["/?analysis=generated"], false, true);
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    client.deleteAccountAnalysis.mockRejectedValue(new ConnectError("Deletion access removed", Code.PermissionDenied));
    client.getAccountAnalysis.mockRejectedValue(new ConnectError("Analysis access removed", Code.PermissionDenied));
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await screen.findByText("Deletion access removed");
    await waitFor(() => expect(screen.queryByText("100 TH/s")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Permanently delete" })).toBeDisabled();
    expect(scope.refreshAccount).toHaveBeenCalled();
  });
  it("aborts deletion when the current account role loses administrator authority", async () => {
    const user = userEvent.setup();
    const { client, scope, rerenderScope } = mount(["/?analysis=generated"], false, true);
    let finish!: () => void;
    client.deleteAccountAnalysis.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await waitFor(() => expect(finish).toBeDefined());
    scope.account = create(AccountSchema, { ...scope.account, role: AccountRole.VIEWER });
    rerenderScope();
    expect(client.deleteAccountAnalysis.mock.calls[0][1].signal.aborted).toBe(true);
    await act(async () => {
      finish();
    });
    expect(screen.getByText("100 TH/s")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete report" })).not.toBeInTheDocument();
    expect(client.listAccountAnalyses).toHaveBeenCalledTimes(1);
  });
  it("does not apply a late delete completion after the account scope is canceled", async () => {
    const user = userEvent.setup();
    const { client, controller } = mount(["/?analysis=generated"], false, true);
    let finish!: () => void;
    client.deleteAccountAnalysis.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    await user.click(await screen.findByRole("button", { name: "Delete report" }));
    await user.click(screen.getByRole("button", { name: "Permanently delete" }));
    await waitFor(() => expect(finish).toBeDefined());
    await act(async () => {
      controller.abort();
      finish();
    });
    expect(screen.queryByText("100 TH/s")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(client.listAccountAnalyses).toHaveBeenCalledTimes(1);
  });
  it("keeps ad hoc generation out of the read-only client preview", async () => {
    mount(["/"], true);
    await screen.findByRole("cell", { name: "Monthly service report" });
    expect(screen.queryByRole("button", { name: "Generate report" })).not.toBeInTheDocument();
  });
  it("clears an open analysis when the refreshed shared library removes its access", async () => {
    const { client } = mount(["/?analysis=generated"], false, true);
    await screen.findByText("100 TH/s");
    client.listAccountAnalyses.mockResolvedValue({ analyses: [] });
    await pollReports();
    await waitFor(() => expect(screen.queryByText("100 TH/s")).not.toBeInTheDocument());
    expect(screen.getByText("This report is no longer available in your account library.")).toBeInTheDocument();
  });
  it("opens on a searchable library and shows report detail only after selection", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/?preview=client"]);
    await screen.findByRole("cell", { name: "Monthly service report" });
    expect(client.getAccountReport).not.toHaveBeenCalled();
    expect(screen.queryByRole("region", { name: "Selected report" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Status" })).not.toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Search reports" }), "no matching report");
    expect(screen.queryByRole("cell", { name: "Monthly service report" })).not.toBeInTheDocument();
    await user.clear(screen.getByRole("textbox", { name: "Search reports" }));
    await user.click(await screen.findByRole("cell", { name: "Monthly service report" }));
    await screen.findByText("Corrected meter evidence");
    expect(screen.getByText(/Data is incomplete\./)).toBeInTheDocument();
    const library = within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByRole("link", {
      name: "Reports",
    });
    expect(library).toHaveAttribute("href", "/?preview=client");
    await user.click(library);
    expect(screen.queryByRole("region", { name: "Selected report" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Reports", level: 1 })).toBeInTheDocument();
  });
  it("shows report type in its own cell and opens a report from another cell in the row", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"], false, true);
    const name = await screen.findByRole("cell", { name: "Monthly service report" });
    const table = screen.getByRole("table");
    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((header) => header.textContent),
    ).toEqual(["Report", "Report type", "Reporting period", "Available since", "Data quality"]);
    expect(within(name).queryByText("Official report")).not.toBeInTheDocument();
    const row = name.closest("tr")!;
    expect(within(row).getByRole("cell", { name: "Official report" })).toBeInTheDocument();
    expect(within(table).queryByRole("button")).not.toBeInTheDocument();
    expect(within(table).queryByRole("link")).not.toBeInTheDocument();
    await user.click(within(row).getByRole("cell", { name: "Required information missing" }));
    await screen.findByText("Corrected meter evidence");
    expect(client.getAccountReport).toHaveBeenLastCalledWith(
      { accountId: "client", reportId: "correction" },
      expect.anything(),
    );
  });
  it.each(["{Enter}", " "])("opens a saved analysis with %s from its row", async (key) => {
    const user = userEvent.setup();
    const { client } = mount(["/"], false, true);
    const row = (await screen.findByRole("cell", { name: "My performance" })).closest("tr")!;
    expect(within(row).getByRole("cell", { name: "Ad hoc · Miner performance" })).toBeInTheDocument();
    row.focus();
    await user.keyboard(key);
    await screen.findByText("100 TH/s");
    expect(client.getAccountAnalysis).toHaveBeenLastCalledWith(
      { accountId: "client", analysisId: "generated" },
      expect.anything(),
    );
  });
  it("filters the shared library by report type and restores both kinds", async () => {
    const user = userEvent.setup();
    const { client } = mount(["/"], false, true);
    await screen.findByRole("cell", { name: "My performance" });
    await user.click(screen.getByRole("button", { name: "Report type" }));
    await user.click(screen.getByRole("option", { name: "Miner performance" }));
    await waitFor(() =>
      expect(client.listAccountAnalyses).toHaveBeenLastCalledWith(
        expect.objectContaining({ kind: "miner_performance" }),
        expect.anything(),
      ),
    );
    expect(screen.queryByRole("cell", { name: "Monthly service report" })).not.toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "My performance" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Report type" }));
    await user.click(screen.getByRole("option", { name: "Monthly service report" }));
    expect(await screen.findByRole("cell", { name: "Monthly service report" })).toBeInTheDocument();
    expect(screen.queryByRole("cell", { name: "My performance" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Report type" }));
    await user.click(screen.getByRole("option", { name: "All report types" }));
    expect(await screen.findByRole("cell", { name: "My performance" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Monthly service report" })).toBeInTheDocument();
  });
  it("opens the current publication and lazily loads its earlier publication without draft actions", async () => {
    const user = userEvent.setup();
    const { client } = mount();
    await screen.findByText("Corrected meter evidence");
    expect(client.getAccountReport).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("button", { name: "Publish report" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Report version" })).not.toBeInTheDocument();
    await user.click(screen.getByText("Publication history"));
    const history = within(screen.getByRole("region", { name: "Publication history" }));
    await user.click((await history.findByText("Replaced")).closest("tr")!);
    await waitFor(() =>
      expect(client.getAccountReport).toHaveBeenLastCalledWith(
        { accountId: "client", reportId: "original" },
        expect.anything(),
      ),
    );
    const selected = within(screen.getByRole("region", { name: "Selected report" }));
    expect(selected.queryByText("Replacement reason:")).not.toBeInTheDocument();
    await user.click(selected.getByText("Publication history"));
    await user.click(
      (
        await within(screen.getByRole("region", { name: "Publication history" })).findByText("Current publication")
      ).closest("tr")!,
    );
    expect(await screen.findByText("Replacement reason:")).toBeInTheDocument();
  });
  it("clears visible report data after live read denial", async () => {
    const { client, scope } = mount();
    await screen.findByText("Corrected meter evidence");
    client.listAccountReports.mockRejectedValue(new ConnectError("Access removed", Code.PermissionDenied));
    await pollReports();
    await screen.findByText("Access removed");
    expect(screen.queryByText("Corrected meter evidence")).not.toBeInTheDocument();
    expect(scope.refreshAccount).toHaveBeenCalled();
    expect(scope.invalidateAccount).not.toHaveBeenCalled();
  });
  it("never saves a partial download after scope cancellation", async () => {
    const user = userEvent.setup();
    const download = vi.spyOn(utility, "downloadBlob").mockImplementation(() => {});
    const { client, controller } = mount();
    let finish!: () => void;
    client.exportAccountReport.mockImplementation(async function* () {
      yield {
        chunk: new Uint8Array([1]),
        filename: "report.html",
        mediaType: "text/html",
        sha256: "4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a",
      };
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
    });
    await screen.findByText("Corrected meter evidence");
    expect(screen.queryByText("Download source inputs")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Download report HTML" }));
    await waitFor(() => expect(finish).toBeDefined());
    await act(async () => {
      controller.abort();
      finish();
    });
    expect(download).not.toHaveBeenCalled();
    download.mockRestore();
  });
});

it("loads the next official-report metadata page without losing the first page", async () => {
  const { client } = mount(["/"]);
  await screen.findByRole("cell", { name: "Monthly service report" });
  const first = create(ReportSummarySchema, {
    reportId: "first-page",
    status: "issued",
    period: { kind: "monthly", timezone: "UTC" },
  });
  const second = create(ReportSummarySchema, {
    reportId: "next-page",
    status: "issued",
    period: { kind: "annual", timezone: "UTC" },
  });
  client.listAccountReports.mockImplementation(async (request: { pageToken?: string }) =>
    request.pageToken ? { reports: [second] } : { reports: [first], nextPageToken: "official-cursor" },
  );
  await pollReports();
  await userEvent.click(await screen.findByRole("button", { name: "Load more reports" }));
  await screen.findByRole("cell", { name: "Annual performance report" });
  expect(screen.getByRole("cell", { name: "Monthly service report" })).toBeInTheDocument();
  expect(client.listAccountReports).toHaveBeenCalledWith(
    expect.objectContaining({ pageToken: "official-cursor", pageSize: 50 }),
    expect.anything(),
  );
});
