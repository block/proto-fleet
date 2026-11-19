import { MemoryRouter, useNavigate } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import userEvent from "@testing-library/user-event";

import { hostedAccountsClient } from "../api";
import ReportingQueue from "./ReportingQueue";
import {
  AcknowledgeHostReportQueueResponseSchema,
  ExportHostReportResponseSchema,
  GetHostReportingSetupResponseSchema,
  GetHostReportResponseSchema,
  type HostReportingSetup,
  HostReportingSetupSchema,
  type HostReportQueueItem,
  type HostReportRequest,
  HostReportRequestSchema,
  HostReportSchema,
  HostReportSummarySchema,
  ListHostReportingAccountsResponseSchema,
  type ListHostReportQueueResponse,
  ListHostReportQueueResponseSchema,
  ListHostReportRequestsResponseSchema,
  ListHostReportsResponseSchema,
  PrepareHostReportResponseSchema,
  PublishHostReportResponseSchema,
  RecordHostReportDeliveryResponseSchema,
  RecordHostReportRequestFulfillmentResponseSchema,
  ReportingPeriodSchema,
  SetHostReportingAuthorityResponseSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";

afterEach(() => vi.restoreAllMocks());
function mount(
  canPublish = true,
  status = "draft",
  overrides: Partial<HostReportingSetup> = {},
  requests: HostReportRequest[] = [],
  entry = "/accounts/reports?account=client&report=report",
  queueItems: HostReportQueueItem[] = [],
  nextPageToken = "",
) {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  const report = create(HostReportSchema, {
    report: {
      reportId: "report",
      revision: 4n,
      status,
      number: 1n,
      result: { complete: false, metrics: { monthly_uptime: { status: "unavailable", causes: ["missing_G1"] } } },
    },
    canPublish,
  });
  const setup = create(HostReportingSetupSchema, {
    accountId: "client",
    name: "Northstar",
    canCreate: true,
    canReadRequests: true,
    canCreateRequests: true,
    owners: [{ userId: "operator", username: "Casey" }],
    input: {
      revision: 2n,
      configuration: { timezone: "UTC", reportingYearMonth: 1, reportingYearDay: 1 },
    },
    authorities: [{ userId: "operator", username: "Casey", canCreate: true }],
  });
  Object.assign(setup, overrides);
  vi.spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
    create(ListHostReportingAccountsResponseSchema, {
      accounts: [{ accountId: "client", name: "Northstar", canCreate: true }],
    }),
  );
  vi.spyOn(hostedAccountsClient, "listHostReportQueue").mockResolvedValue(
    create(ListHostReportQueueResponseSchema, { items: queueItems, nextPageToken }),
  );
  vi.spyOn(hostedAccountsClient, "getHostReportingSetup").mockResolvedValue(
    create(GetHostReportingSetupResponseSchema, { setup }),
  );
  vi.spyOn(hostedAccountsClient, "listHostReports").mockResolvedValue(
    create(ListHostReportsResponseSchema, {
      reports: [
        create(HostReportSummarySchema, {
          report: { reportId: "report", revision: 4n, status, number: 1n, complete: false },
          canPublish,
        }),
      ],
    }),
  );
  vi.spyOn(hostedAccountsClient, "listHostReportRequests").mockResolvedValue(
    create(ListHostReportRequestsResponseSchema, { requests }),
  );
  const get = vi
    .spyOn(hostedAccountsClient, "getHostReport")
    .mockResolvedValue(create(GetHostReportResponseSchema, { report }));
  const publish = vi
    .spyOn(hostedAccountsClient, "publishHostReport")
    .mockResolvedValue(create(PublishHostReportResponseSchema));
  let navigate!: ReturnType<typeof useNavigate>;
  const RoutedQueue = () => {
    navigate = useNavigate();
    return <ReportingQueue />;
  };
  render(
    <MemoryRouter initialEntries={[entry]}>
      <RoutedQueue />
    </MemoryRouter>,
  );
  return { report, get, publish, setup, navigate: (url: string) => navigate(url) };
}
describe("host report publication", () => {
  it("downloads frozen source inputs from the shared report actions menu", async () => {
    const user = userEvent.setup();
    const download = vi.spyOn(hostedAccountsClient, "exportHostReport").mockImplementation(async function* () {
      yield create(ExportHostReportResponseSchema, {
        chunk: new Uint8Array([1]),
        filename: "source-inputs.json",
        mediaType: "application/json",
        sha256: "4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7cce23c7785459a",
      });
    });
    mount(false, "issued");
    await user.click(await screen.findByRole("button", { name: "Report actions" }));
    await user.click(screen.getByText("Download source inputs"));
    await waitFor(() =>
      expect(download).toHaveBeenCalledWith(
        { accountId: "client", reportId: "report", name: "source-inputs.json" },
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
  });

  it("offers direct publication without review or submission actions", async () => {
    mount();
    expect(await screen.findByRole("button", { name: "Publish report" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /submit|convert to draft|approve/i })).not.toBeInTheDocument();
  });

  it("keeps earlier publications in secondary history and excludes unrelated report series", async () => {
    const user = userEvent.setup();
    const { report, get } = mount(false, "issued");
    report.report!.seriesId = "period-series";
    report.report!.number = 2n;
    report.report!.correctionOf = "original";
    get.mockImplementation(async ({ reportId }) =>
      create(GetHostReportResponseSchema, {
        report:
          reportId === "original"
            ? { report: { ...report.report!, reportId: "original", number: 1n, correctionOf: "" } }
            : report,
      }),
    );
    vi.mocked(hostedAccountsClient.listHostReports).mockResolvedValue(
      create(ListHostReportsResponseSchema, {
        reports: [
          {
            report: {
              reportId: "report",
              seriesId: "period-series",
              number: 2n,
              status: "issued",
              correctionOf: "original",
            },
          },
          { report: { reportId: "original", seriesId: "period-series", number: 1n, status: "issued" } },
          { report: { reportId: "unrelated", seriesId: "another-period", number: 99n, status: "issued" } },
        ],
      }),
    );
    await user.click(await screen.findByText("Publication history"));
    const history = within(screen.getByRole("region", { name: "Publication history" }));
    await user.click((await history.findByText("Replaced")).closest("tr")!);
    await waitFor(() =>
      expect(get).toHaveBeenLastCalledWith({ accountId: "client", reportId: "original" }, expect.anything()),
    );
    expect(screen.queryByRole("button", { name: "Report version" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Report actions" }));
    expect(screen.getByText("Download source inputs")).toBeInTheDocument();
    expect(screen.queryByText("Replace report")).not.toBeInTheDocument();
    expect(screen.queryByText(/Version 99/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Refresh report" })).not.toBeInTheDocument();
  });

  it("prepares a replacement from the overflow menu with the original period and a reason", async () => {
    const user = userEvent.setup();
    const { report } = mount(false, "issued");
    report.report!.period = create(ReportingPeriodSchema, {
      kind: "monthly",
      timezone: "UTC",
      start: { seconds: 1790812800n },
      end: { seconds: 1793491200n },
      asOf: { seconds: 1793491200n },
    });
    const prepare = vi
      .spyOn(hostedAccountsClient, "prepareHostReport")
      .mockResolvedValue(create(PrepareHostReportResponseSchema));
    await screen.findByRole("heading", { name: "Monthly service report" });
    expect(screen.queryByText("Replace report")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Report actions" }));
    await user.click(screen.getByText("Replace report"));
    const form = within(screen.getByRole("dialog", { name: "Replace report" }));
    expect(form.queryByLabelText("Period start")).not.toBeInTheDocument();
    expect(form.getByRole("button", { name: "Create draft" })).toBeDisabled();
    await user.type(form.getByLabelText("Replacement reason (visible to client)"), "Reviewed late meter evidence");
    await user.click(form.getByRole("button", { name: "Accountable reporting owner" }));
    await user.click(await screen.findByRole("option", { name: "Casey" }));
    await user.click(form.getByRole("button", { name: "Create draft" }));
    await waitFor(() =>
      expect(prepare).toHaveBeenCalledWith(
        expect.objectContaining({
          correctionOf: "report",
          correctionReason: "Reviewed late meter evidence",
          period: expect.objectContaining({
            kind: "monthly",
            start: report.report!.period!.start,
            end: report.report!.period!.end,
          }),
        }),
        expect.anything(),
      ),
    );
  });

  it("opens the shared draft replacement without preparing another draft", async () => {
    const user = userEvent.setup();
    const { report, get } = mount(false, "issued");
    report.report!.seriesId = "series";
    const replacement = create(HostReportSchema, {
      report: {
        ...report.report!,
        reportId: "shared",
        number: 2n,
        status: "draft",
        correctionOf: "report",
        correctionReason: "Reviewed late evidence",
      },
    });
    vi.mocked(hostedAccountsClient.listHostReports).mockResolvedValue(
      create(ListHostReportsResponseSchema, {
        reports: [report, replacement].map(({ report: item }) => ({
          report: {
            reportId: item!.reportId,
            seriesId: item!.seriesId,
            number: item!.number,
            status: item!.status,
            correctionOf: item!.correctionOf,
          },
        })),
      }),
    );
    get.mockImplementation(async ({ reportId }) =>
      create(GetHostReportResponseSchema, { report: reportId === "shared" ? replacement : report }),
    );
    const prepare = vi.spyOn(hostedAccountsClient, "prepareHostReport");
    await user.click(await screen.findByRole("button", { name: "Report actions" }));
    await user.click(screen.getByText("Open replacement draft"));
    await screen.findByText("Reviewed late evidence");
    expect(screen.getByText("Draft")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Replace report" })).not.toBeInTheDocument();
    expect(prepare).not.toHaveBeenCalled();
  });

  it("lists the current publication per series and keeps pending drafts", async () => {
    const user = userEvent.setup();
    const { get } = mount(false, "issued", {}, [], "/accounts/reports?account=client&view=reports");
    vi.mocked(hostedAccountsClient.listHostReports).mockResolvedValue(
      create(ListHostReportsResponseSchema, {
        reports: [
          { report: { reportId: "old", seriesId: "series", number: 1n, status: "issued", deliveryStatus: "failed" } },
          {
            report: {
              reportId: "current",
              seriesId: "series",
              number: 2n,
              status: "issued",
              deliveryStatus: "confirmed",
            },
          },
          { report: { reportId: "pending", seriesId: "series", number: 3n, status: "draft", correctionOf: "current" } },
          {
            report: {
              reportId: "other",
              seriesId: "other-series",
              number: 1n,
              status: "issued",
              deliveryStatus: "unconfirmed",
            },
          },
        ],
      }),
    );
    const library = within(await screen.findByRole("region", { name: "Prepared reports" }));
    expect(library.getAllByRole("row")).toHaveLength(4);
    expect(library.getByText("Draft")).toBeInTheDocument();
    expect(library.getByText("Unconfirmed")).toBeInTheDocument();
    expect(library.queryByText("Failed")).not.toBeInTheDocument();
    await user.click(library.getByText("Confirmed"));
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith({ accountId: "client", reportId: "current" }, expect.anything()),
    );
  });

  it("starts with the work queue without opening client settings or a report", async () => {
    const { get } = mount(true, "draft", {}, [], "/accounts/reports");
    await screen.findByRole("heading", { name: "Reports", level: 1 });
    await screen.findByRole("button", { name: "Client reports" });
    expect(get).not.toHaveBeenCalled();
    expect(screen.queryByRole("region", { name: "Reporting setup" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Prepared reports" })).not.toBeInTheDocument();
    expect(hostedAccountsClient.listHostReportQueue).toHaveBeenCalledTimes(1);
  });
  it("keeps an account-wide requested report delivery action out of restricted scope", async () => {
    const { report, get } = mount(false, "issued", { canReadRequests: false, canCreateRequests: false });
    report.report!.period = create(ReportingPeriodSchema, { kind: "ad_hoc" });
    get.mockResolvedValue(create(GetHostReportResponseSchema, { report }));
    await screen.findByRole("navigation", { name: "Report sheets" });
    expect(screen.queryByRole("button", { name: "Record report delivery" })).not.toBeInTheDocument();
  });

  it("requires a named incomplete exception and sends the observed revision", async () => {
    const user = userEvent.setup();
    const { publish } = mount();
    await user.click(await screen.findByRole("button", { name: "Publish report" }));
    const form = within(screen.getByRole("dialog", { name: "Publish report" }));
    expect(form.getByRole("button", { name: "Publish report" })).toBeDisabled();
    await user.type(form.getByLabelText("Incomplete-report explanation"), "Operations approved missing G1 disclosure");
    await user.click(form.getByRole("button", { name: "Publish report" }));
    await waitFor(() =>
      expect(publish).toHaveBeenCalledWith(
        {
          accountId: "client",
          reportId: "report",
          expectedRevision: 4n,
          exception: "Operations approved missing G1 disclosure",
        },
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
  });
  it("does not offer publication when the server denies that exact action", async () => {
    mount(false);
    await screen.findByRole("navigation", { name: "Report sheets" });
    expect(screen.queryByRole("button", { name: "Publish report" })).not.toBeInTheDocument();
  });
  it("refreshes a revision conflict and requires a fresh confirmation", async () => {
    const user = userEvent.setup();
    const { publish, get } = mount();
    publish.mockRejectedValue(new ConnectError("Report changed", Code.Aborted));
    await user.click(await screen.findByRole("button", { name: "Publish report" }));
    const form = within(screen.getByRole("dialog", { name: "Publish report" }));
    await user.type(form.getByLabelText("Incomplete-report explanation"), "Named reviewer exception");
    await user.click(form.getByRole("button", { name: "Publish report" }));
    await screen.findByText(/Close this form and review the refreshed data/);
    expect(form.getByRole("button", { name: "Publish report" })).toBeDisabled();
    expect(get.mock.calls.length).toBeGreaterThan(1);
    expect(publish).toHaveBeenCalledTimes(1);
  });
  it("keeps publication bound to the version opened even if polling returns a newer revision", async () => {
    const intervals = vi.spyOn(window, "setInterval");
    const user = userEvent.setup();
    const { report, publish, get } = mount();
    await user.click(await screen.findByRole("button", { name: "Publish report" }));
    const form = within(screen.getByRole("dialog", { name: "Publish report" }));
    await user.type(form.getByLabelText("Incomplete-report explanation"), "Reviewed this version");
    const latest = create(HostReportSchema, { ...report, report: { ...report.report!, revision: 5n } });
    get.mockResolvedValue(create(GetHostReportResponseSchema, { report: latest }));
    await act(async () => {
      for (const [callback, interval] of intervals.mock.calls)
        if (interval === POLL_INTERVAL_MS && typeof callback === "function") callback();
    });
    await waitFor(() => expect(get.mock.calls.length).toBeGreaterThan(1));
    await user.click(form.getByRole("button", { name: "Publish report" }));
    expect(publish).toHaveBeenCalledWith(expect.objectContaining({ expectedRevision: 4n }), expect.anything());
  });
  it("records failed delivery separately and gives a later attempt a new operation identity", async () => {
    const user = userEvent.setup();
    mount(false, "issued");
    const record = vi
      .spyOn(hostedAccountsClient, "recordHostReportDelivery")
      .mockResolvedValue(create(RecordHostReportDeliveryResponseSchema));
    for (let index = 0; index < 2; index++) {
      await user.click(await screen.findByRole("button", { name: "Record report delivery" }));
      const form = within(screen.getByRole("dialog", { name: "Record report delivery" }));
      await user.click(form.getByRole("button", { name: "Delivery outcome" }));
      await user.click(await screen.findByRole("option", { name: "Failed" }));
      await user.type(form.getByLabelText("Actual delivery channel"), "Agreed email");
      await user.type(form.getByLabelText("Delivery evidence reference"), `Attempt ${index + 1} log`);
      await user.click(form.getByRole("button", { name: "Record delivery" }));
      await waitFor(() => expect(record).toHaveBeenCalledTimes(index + 1));
      await waitFor(() =>
        expect(screen.queryByRole("dialog", { name: "Record report delivery" })).not.toBeInTheDocument(),
      );
    }
    expect(record.mock.calls[0][0]).toMatchObject({ status: "failed", failureCategory: "unconfirmed" });
    expect(record.mock.calls[0][0].requestId).not.toBe(record.mock.calls[1][0].requestId);
  });
});

describe("reporting access and recorded requests", () => {
  it("lets an administrator assign the first grant without reading reports or requests", async () => {
    const user = userEvent.setup();
    mount(false, "draft", {
      canAdminister: true,
      canCreate: false,
      canReadRequests: false,
      canCreateRequests: false,
      authorities: [],
    });
    const reports = vi
      .mocked(hostedAccountsClient.listHostReports)
      .mockRejectedValue(new ConnectError("No reporting grant", Code.PermissionDenied));
    const requests = vi
      .mocked(hostedAccountsClient.listHostReportRequests)
      .mockRejectedValue(new ConnectError("No request access", Code.PermissionDenied));
    const save = vi
      .spyOn(hostedAccountsClient, "setHostReportingAuthority")
      .mockResolvedValue(create(SetHostReportingAuthorityResponseSchema));
    await user.click(await screen.findByRole("button", { name: "Reporting settings" }));
    await user.click(await screen.findByRole("button", { name: "Reporting permissions" }));
    const form = within(screen.getByRole("dialog", { name: "Reporting permissions" }));
    await user.click(form.getByRole("button", { name: "Operator" }));
    await user.click(await screen.findByRole("option", { name: "Casey" }));
    // An owner without a stored grant is a manager with implicit all-site authority.
    expect(form.getByLabelText("Create reports")).toBeChecked();
    expect(form.getByRole("radio", { name: "All account sites" })).toBeChecked();
    await user.click(form.getByRole("radio", { name: "Specific sites" }));
    expect(form.getByText("Site choices are unavailable.")).toBeInTheDocument();
    await user.click(form.getByRole("radio", { name: "All account sites" }));
    expect(form.getByRole("radio", { name: "Specific sites" })).not.toBeChecked();
    expect(form.queryByText("Site choices are unavailable.")).not.toBeInTheDocument();
    await user.click(form.getByRole("button", { name: "Save reporting permissions" }));
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        expect.objectContaining({
          authority: expect.objectContaining({ userId: "operator", canCreate: true, allSites: true }),
        }),
        expect.anything(),
      ),
    );
    expect(reports).not.toHaveBeenCalled();
    expect(requests).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Create report" })).not.toBeInTheDocument();
  });

  it("keeps normal preparation available without account-wide request access", async () => {
    const user = userEvent.setup();
    mount(false, "draft", { canReadRequests: false, canCreateRequests: false });
    await user.click(await screen.findByRole("link", { name: "Client reports" }));
    await user.click(await screen.findByRole("button", { name: "Create report" }));
    expect(hostedAccountsClient.listHostReportRequests).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Record ad hoc request" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create requested report" })).not.toBeInTheDocument();
    const form = within(screen.getByRole("dialog", { name: "Create report" }));
    await user.click(form.getByRole("button", { name: "Report type" }));
    expect(screen.queryByRole("option", { name: "Ad hoc" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("option", { name: "Annual" }));
    expect(form.queryByLabelText("Reporting year start (UTC)")).not.toBeInTheDocument();
  });

  it("loads request history one page at a time", async () => {
    mount(false, "draft", { canReadRequests: true }, []);
    vi.mocked(hostedAccountsClient.listHostReportRequests).mockImplementation(async ({ pageToken }) =>
      create(
        ListHostReportRequestsResponseSchema,
        pageToken
          ? { requests: [create(HostReportRequestSchema, { requestId: "older", requester: "Earlier lead" })] }
          : {
              requests: [create(HostReportRequestSchema, { requestId: "newer", requester: "Client lead" })],
              nextPageToken: "next",
            },
      ),
    );
    await userEvent.click(await screen.findByRole("button", { name: "Report requests" }));
    await screen.findByText(/Client lead · Received/);
    expect(screen.queryByText(/Earlier lead · Received/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Load more requests" }));
    await screen.findByText(/Earlier lead · Received/);
    expect(screen.getByText(/Client lead · Received/)).toBeInTheDocument();
    expect(hostedAccountsClient.listHostReportRequests).toHaveBeenCalledWith(
      expect.objectContaining({ pageSize: 50, pageToken: "next" }),
      expect.anything(),
    );
    await waitFor(() => expect(screen.queryByRole("button", { name: "Load more requests" })).not.toBeInTheDocument());
  });

  it("uses the separate request-creation flag even when request reading is allowed", async () => {
    mount(false, "draft", { canReadRequests: true, canCreateRequests: false }, [
      create(HostReportRequestSchema, { requestId: "recorded", requester: "Client lead" }),
    ]);
    await userEvent.click(await screen.findByRole("button", { name: "Report requests" }));
    await screen.findByText(/Client lead · Received/);
    expect(hostedAccountsClient.listHostReportRequests).toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Record ad hoc request" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Prepared reports" })).not.toBeInTheDocument();
  });

  it("prepares an ad hoc report with the canonical recorded request and immutable receipt", async () => {
    const user = userEvent.setup();
    const request = create(HostReportRequestSchema, {
      requestId: "a36b08fe-e8f5-4d5c-8f35-64dc6c639054",
      requester: "Client lead",
      scope: "October evidence",
      sourceRef: "Client email / October",
      receivedAt: { seconds: 1793505600n, nanos: 123000000 },
    });
    mount(false, "draft", {}, [request], "/accounts/reports", [
      create(ListHostReportQueueResponseSchema, {
        items: [{ queueId: "request-work", accountId: "client", accountName: "Northstar", request }],
      }).items[0],
    ]);
    const prepare = vi
      .spyOn(hostedAccountsClient, "prepareHostReport")
      .mockResolvedValue(create(PrepareHostReportResponseSchema));
    await user.click(await screen.findByRole("cell", { name: "Northstar" }));
    await user.click(await screen.findByText(/Client lead · Received/));
    await user.click(screen.getByRole("button", { name: "Create requested report" }));
    const form = within(screen.getByRole("dialog", { name: "Create report" }));
    expect(form.queryByLabelText("Request evidence reference")).not.toBeInTheDocument();
    expect(form.queryByLabelText("Request received (UTC)")).not.toBeInTheDocument();
    expect(form.getByText("Request evidence: Client email / October")).toBeInTheDocument();
    await user.click(form.getByRole("button", { name: "Report type" }));
    expect(screen.queryByRole("option", { name: "Monthly" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("option", { name: "Ad hoc" }));
    fireEvent.change(form.getByLabelText("Period start (UTC)"), { target: { value: "2026-10-01T00:00" } });
    fireEvent.change(form.getByLabelText("Period end (exclusive) (UTC)"), { target: { value: "2026-11-01T00:00" } });
    await user.click(form.getByRole("button", { name: "Accountable reporting owner" }));
    await user.click(await screen.findByRole("option", { name: "Casey" }));
    await user.click(form.getByRole("button", { name: "Create draft" }));
    await waitFor(() =>
      expect(prepare).toHaveBeenCalledWith(
        expect.objectContaining({
          period: expect.objectContaining({
            kind: "ad_hoc",
            requestRef: request.requestId,
            requestReceivedAt: request.receivedAt,
          }),
        }),
        expect.anything(),
      ),
    );
  });

  it("shows server-confirmed fulfillment and retains original delivery evidence", async () => {
    const request = create(HostReportRequestSchema, {
      requestId: "request-id",
      requester: "Client lead",
      sourceRef: "Original request",
      fulfilledAt: { seconds: 1793552400n, nanos: 0 },
      fulfillmentRef: "Actual package receipt",
      deliveryRef: "Historic manual delivery note",
    });
    mount(false, "issued", {}, [request]);
    await userEvent.click(await screen.findByRole("button", { name: "Report requests" }));
    await userEvent.click(await screen.findByText(/Client lead · Received/));
    expect(await screen.findByText("Fulfilled · delivery confirmed")).toBeInTheDocument();
    expect(screen.getByText("Recorded delivery evidence: Historic manual delivery note")).toBeInTheDocument();
  });

  it("records external fulfillment against the received request without sending a package", async () => {
    const record = vi
      .spyOn(hostedAccountsClient, "recordHostReportRequestFulfillment")
      .mockResolvedValue(create(RecordHostReportRequestFulfillmentResponseSchema));
    mount(false, "issued", {}, [
      create(HostReportRequestSchema, { requestId: "canonical-request", requester: "Client lead" }),
    ]);
    await userEvent.click(await screen.findByRole("button", { name: "Report requests" }));
    await userEvent.click(await screen.findByText(/Client lead · Received/));
    await userEvent.click(screen.getByRole("button", { name: "Record external fulfillment" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Actual delivery time (UTC)"), {
      target: { value: "2026-11-01T12:00" },
    });
    await userEvent.type(within(dialog).getByLabelText("Delivery evidence reference"), "package receipt");
    await userEvent.type(
      within(dialog).getByLabelText("Delivered package and confirmation evidence"),
      "Client accepted the reviewed package",
    );
    await userEvent.click(within(dialog).getByRole("button", { name: "Record fulfillment" }));
    await waitFor(() =>
      expect(record).toHaveBeenCalledWith(
        expect.objectContaining({
          accountId: "client",
          requestId: "canonical-request",
          deliveryRef: "package receipt",
          evidence: "Client accepted the reviewed package",
        }),
        expect.anything(),
      ),
    );
  });

  it("does not treat a manual evidence reference as confirmed report delivery", async () => {
    mount(false, "issued", {}, [
      create(HostReportRequestSchema, {
        requestId: "request-id",
        requester: "Client lead",
        deliveryRef: "Manual note",
      }),
    ]);
    await userEvent.click(await screen.findByRole("button", { name: "Report requests" }));
    await userEvent.click(await screen.findByText(/Client lead · Received/));
    expect(screen.getByText("Delivery unconfirmed")).toBeInTheDocument();
    expect(screen.queryByText("Fulfilled · delivery confirmed")).not.toBeInTheDocument();
  });
});

it.each(["reports", "requests"])("shows an explicit inaccessible state for a %s view", async (view) => {
  mount(true, "draft", {}, [], `/accounts/reports?account=unavailable&view=${view}`);
  expect(await screen.findByRole("status")).toHaveTextContent(
    "This reporting view is unavailable for the selected account.",
  );
});
it.each([
  ["queue", "draft"],
  ["queue", "draft"],
  ["reports", "draft"],
  ["reports", "draft"],
])("opens an existing %s / %s from its row without offering preparation", async (view, status) => {
  const user = userEvent.setup();
  const { get } = mount(
    false,
    status,
    {},
    [],
    `/accounts/reports?account=client&view=${view}`,
    create(ListHostReportQueueResponseSchema, {
      items: [
        {
          accountId: "client",
          accountName: "Northstar",
          ownerUsername: "Casey",
          queueId: "queue",
          report: { report: { reportId: "report", status } },
        },
      ],
    }).items,
  );
  await screen.findByRole("cell", { name: view === "queue" ? "Casey" : "Required information missing" });
  expect(screen.queryByRole("columnheader", { name: "Next action" })).not.toBeInTheDocument();
  for (const row of screen.getAllByRole("row")) {
    expect(within(row).queryByRole("button")).not.toBeInTheDocument();
    expect(within(row).queryByRole("link")).not.toBeInTheDocument();
  }
  await user.click(
    await screen.findByRole("cell", { name: view === "queue" ? "Casey" : "Required information missing" }),
  );
  await screen.findByRole("navigation", { name: "Report sheets" });
  expect(get).toHaveBeenCalledWith({ accountId: "client", reportId: "report" }, expect.anything());
  expect(screen.queryByRole("button", { name: "Create report" })).not.toBeInTheDocument();
});

it("keeps completed and superseded obligations out of the work queue", async () => {
  vi.spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
    create(ListHostReportingAccountsResponseSchema),
  );
  vi.spyOn(hostedAccountsClient, "listHostReportQueue").mockResolvedValue(
    create(ListHostReportQueueResponseSchema, {
      items: [
        { accountName: "Completed client", queueId: "complete", notificationStatus: "quiet" },
        { accountName: "Superseded client", queueId: "superseded", notificationStatus: "superseded" },
      ],
    }),
  );
  render(
    <MemoryRouter>
      <ReportingQueue />
    </MemoryRouter>,
  );
  await screen.findByRole("textbox", { name: "Search clients or reports" });
  expect(screen.queryByText("Completed client")).not.toBeInTheDocument();
  expect(screen.queryByText("Superseded client")).not.toBeInTheDocument();
});
it.each([
  ["provisional", false],
  ["authoritative", true],
  ["authoritative", false],
])("renders prepared report due dates for %s / reasonable-efforts %s", async (status, reasonableEfforts) => {
  const user = userEvent.setup();
  mount(true, "draft", {}, [], "/accounts/reports?account=client&view=settings");
  vi.mocked(hostedAccountsClient.listHostReports).mockResolvedValue(
    create(ListHostReportsResponseSchema, {
      reports: [
        { report: { reportId: "report", deadlines: { contract: { date: "2026-12-01", status, reasonableEfforts } } } },
      ],
    }),
  );
  await user.click(await screen.findByRole("button", { name: "Client reports" }));
  const region = await screen.findByRole("region", { name: "Prepared reports" });
  if (status === "authoritative" && !reasonableEfforts)
    expect(within(region).getByText("2026-12-01")).toBeInTheDocument();
  else expect(within(region).queryByText("2026-12-01")).not.toBeInTheDocument();
});

it("can continue past an empty admitted queue page", async () => {
  const user = userEvent.setup();
  vi.spyOn(hostedAccountsClient, "listHostReportingAccounts").mockResolvedValue(
    create(ListHostReportingAccountsResponseSchema),
  );
  const list = vi.spyOn(hostedAccountsClient, "listHostReportQueue").mockImplementation(async (request) => ({
    ...create(ListHostReportQueueResponseSchema),
    items: request.pageToken
      ? create(ListHostReportQueueResponseSchema, {
          items: [{ queueId: "older", accountName: "Older active client", notificationStatus: "pending" }],
        }).items
      : [],
    nextPageToken: request.pageToken ? "" : "next-page",
  }));
  render(
    <MemoryRouter>
      <ReportingQueue />
    </MemoryRouter>,
  );
  await user.click(await screen.findByRole("button", { name: "Load more obligations" }));
  expect(await screen.findByText("Older active client")).toBeInTheDocument();
  expect(list).toHaveBeenCalledWith(
    expect.objectContaining({ pageToken: "next-page", pageSize: 50 }),
    expect.anything(),
  );
});

it("prepares official calendar periods in the approved reporting timezone", async () => {
  const user = userEvent.setup();
  mount(
    true,
    "draft",
    {
      input: create(HostReportingSetupSchema, {
        input: { revision: 2n, configuration: { timezone: "Africa/Nairobi" } },
      }).input,
    },
    [],
    "/accounts/reports?account=client&view=reports",
  );
  const prepare = vi
    .spyOn(hostedAccountsClient, "prepareHostReport")
    .mockResolvedValue(create(PrepareHostReportResponseSchema));
  await user.click(await screen.findByRole("button", { name: "Create report" }));
  const form = within(screen.getByRole("dialog", { name: "Create report" }));
  fireEvent.change(form.getByLabelText("Period start (Africa/Nairobi)"), { target: { value: "2026-10-01T00:00" } });
  fireEvent.change(form.getByLabelText("Period end (exclusive) (Africa/Nairobi)"), {
    target: { value: "2026-11-01T00:00" },
  });
  await user.click(form.getByRole("button", { name: "Accountable reporting owner" }));
  await user.click(await screen.findByRole("option", { name: "Casey" }));
  await user.click(form.getByRole("button", { name: "Create draft" }));
  await waitFor(() =>
    expect(prepare).toHaveBeenCalledWith(
      expect.objectContaining({
        period: expect.objectContaining({
          timezone: "Africa/Nairobi",
          start: expect.objectContaining({ seconds: 1790802000n }),
          end: expect.objectContaining({ seconds: 1793480400n }),
        }),
      }),
      expect.anything(),
    ),
  );
});

const olderQueueItem = () =>
  create(ListHostReportQueueResponseSchema, {
    items: [
      {
        queueId: "older-obligation",
        accountId: "client",
        accountName: "Older obligation",
        generation: 7n,
        canAcknowledge: true,
        canRetry: true,
        notificationStatus: "exhausted",
        period: {
          kind: "monthly",
          timezone: "UTC",
          start: { seconds: 1785542400n },
          end: { seconds: 1788220800n },
          asOf: { seconds: 1788307200n },
        },
      },
    ],
  }).items[0];
it("resolves a notification target beyond the first 50 rows without expanding visible pagination", async () => {
  const user = userEvent.setup();
  const recent = create(ListHostReportQueueResponseSchema, {
    items: Array.from({ length: 50 }, (_, index) => ({
      accountId: "client",
      queueId: `recent-${index}`,
      accountName: `Recent client ${index}`,
      notificationStatus: "pending",
    })),
  }).items;
  const target = olderQueueItem();
  mount(true, "draft", {}, [], "/accounts/reports?account=client&queue=older-obligation", recent, "older-page");
  const list = vi.mocked(hostedAccountsClient.listHostReportQueue).mockImplementation(async (request) =>
    create(ListHostReportQueueResponseSchema, {
      items: request.pageToken ? [target] : recent,
      nextPageToken: request.pageToken ? "" : "older-page",
    }),
  );
  const acknowledge = vi
    .spyOn(hostedAccountsClient, "acknowledgeHostReportQueue")
    .mockResolvedValue(create(AcknowledgeHostReportQueueResponseSchema));
  await user.click(
    within(await screen.findByLabelText("Reporting activity for Older obligation")).getByText("Reporting activity"),
  );
  expect(screen.getByRole("button", { name: "Retry notification" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Acknowledge" }));
  const acknowledgement = within(screen.getByRole("dialog", { name: "Acknowledge reporting obligation" }));
  await user.type(acknowledgement.getByLabelText("Reason"), "Assigned to reporting owner");
  await user.click(acknowledgement.getByRole("button", { name: "Acknowledge" }));
  await waitFor(() =>
    expect(acknowledge).toHaveBeenCalledWith(
      {
        accountId: "client",
        queueId: "older-obligation",
        expectedGeneration: 7n,
        reason: "Assigned to reporting owner",
      },
      expect.anything(),
    ),
  );
  await user.click(await screen.findByRole("button", { name: "Create report" }));
  const preparation = within(screen.getByRole("dialog", { name: "Create report" }));
  expect(preparation.getByLabelText("Period start (UTC)")).toHaveValue("2026-08-01T00:00");
  expect(preparation.getByLabelText("Period end (exclusive) (UTC)")).toHaveValue("2026-09-01T00:00");
  await user.click(preparation.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Work queue" }));
  expect(screen.getAllByRole("row")).toHaveLength(51);
  expect(screen.queryByRole("cell", { name: "Older obligation" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Load more obligations" }));
  expect(await screen.findByRole("cell", { name: "Older obligation" })).toBeInTheDocument();
  expect(list).toHaveBeenCalledWith(expect.objectContaining({ pageToken: "older-page" }), expect.anything());
});
it("rejects queue IDs belonging to a different URL account without falling back to a report", async () => {
  const foreign = { ...olderQueueItem(), accountId: "different-client" };
  const { get } = mount(
    true,
    "draft",
    {},
    [],
    "/accounts/reports?account=client&queue=older-obligation&report=report",
    [foreign],
  );
  expect(await screen.findByRole("status")).toHaveTextContent(
    "This reporting obligation is no longer available for the selected account.",
  );
  expect(screen.queryByLabelText("Reporting activity for Older obligation")).not.toBeInTheDocument();
  expect(hostedAccountsClient.getHostReportingSetup).not.toHaveBeenCalled();
  expect(get).not.toHaveBeenCalled();
});
it.each([
  ["account=client&queue=missing", "This reporting obligation is no longer available for the selected account."],
  ["account=client&queue=", "The reporting obligation link is incomplete."],
  ["queue=missing", "The reporting obligation link is incomplete."],
])("shows an explicit unresolved state for %s", async (query, message) => {
  mount(true, "draft", {}, [], `/accounts/reports?${query}`);
  expect(await screen.findByText(message)).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Prepared reports" })).not.toBeInTheDocument();
  expect(hostedAccountsClient.getHostReportingSetup).not.toHaveBeenCalled();
  if (message.includes("incomplete")) expect(hostedAccountsClient.listHostReportQueue).toHaveBeenCalledTimes(1);
});
it("stops a selected-item lookup when an opaque cursor repeats", async () => {
  mount(true, "draft", {}, [], "/accounts/reports?account=client&queue=missing", [], "cycle");
  const list = vi
    .mocked(hostedAccountsClient.listHostReportQueue)
    .mockResolvedValue(create(ListHostReportQueueResponseSchema, { nextPageToken: "cycle" }));
  expect(
    await screen.findByText("The reporting obligation lookup could not advance. Retry the lookup."),
  ).toBeInTheDocument();
  expect(list).toHaveBeenCalledTimes(3);
  expect(hostedAccountsClient.getHostReportingSetup).not.toHaveBeenCalled();
});
it("aborts an old target lookup and ignores its late response after the URL target changes", async () => {
  const user = userEvent.setup();
  const { navigate } = mount(
    true,
    "draft",
    {},
    [],
    "/accounts/reports?account=client&queue=older-obligation",
    [],
    "older-page",
  );
  let finish!: (value: ListHostReportQueueResponse) => void;
  let previousSignal: AbortSignal | undefined;
  let switched = false;
  vi.mocked(hostedAccountsClient.listHostReportQueue).mockImplementation(async (request, options) => {
    if (switched)
      return create(ListHostReportQueueResponseSchema, {
        items: [{ ...olderQueueItem(), queueId: "new-obligation", accountName: "New obligation" }],
      });
    if (request.pageToken)
      return new Promise((resolve) => {
        previousSignal = options?.signal;
        finish = resolve;
      });
    return create(ListHostReportQueueResponseSchema, { nextPageToken: "older-page" });
  });
  await waitFor(() => expect(finish).toBeDefined());
  switched = true;
  await act(async () => {
    navigate("/accounts/reports?account=client&queue=new-obligation");
  });
  expect(previousSignal?.aborted).toBe(true);
  await user.click(
    within(await screen.findByLabelText("Reporting activity for New obligation")).getByText("Reporting activity"),
  );
  await act(async () => {
    finish(create(ListHostReportQueueResponseSchema, { items: [olderQueueItem()] }));
  });
  expect(screen.queryByLabelText("Reporting activity for Older obligation")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Acknowledge" })).toBeInTheDocument();
});
it("discards a resolved obligation when refreshed account access is denied", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    mount(true, "draft", {}, [], "/accounts/reports?account=client&queue=older-obligation", [olderQueueItem()]);
    await screen.findByLabelText("Reporting activity for Older obligation");
    vi.mocked(hostedAccountsClient.listHostReportQueue).mockRejectedValue(
      new ConnectError("Reporting access removed", Code.PermissionDenied),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    });
    await waitFor(() =>
      expect(screen.queryByLabelText("Reporting activity for Older obligation")).not.toBeInTheDocument(),
    );
    expect(screen.queryByRole("region", { name: "Prepared reports" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Acknowledge" })).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

it("dismisses a queue action when the URL account changes and never restores that stale dialog", async () => {
  const user = userEvent.setup();
  const { navigate } = mount(true, "draft", {}, [], "/accounts/reports?account=client&queue=older-obligation", [
    olderQueueItem(),
  ]);
  await user.click(
    within(await screen.findByLabelText("Reporting activity for Older obligation")).getByText("Reporting activity"),
  );
  await user.click(screen.getByRole("button", { name: "Acknowledge" }));
  expect(screen.getByRole("dialog", { name: "Acknowledge reporting obligation" })).toBeInTheDocument();
  await act(async () => {
    navigate("/accounts/reports?account=another-client&queue=older-obligation");
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Reporting activity for Older obligation")).not.toBeInTheDocument();
  await act(async () => {
    navigate("/accounts/reports?account=client&queue=older-obligation");
  });
  await screen.findByLabelText("Reporting activity for Older obligation");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it.each([
  ["provisional", false],
  ["unavailable", false],
  ["authoritative", true],
  ["authoritative", false],
])("qualifies queue due dates for %s / reasonable-efforts %s", async (status, reasonableEfforts) => {
  const item = create(ListHostReportQueueResponseSchema, {
    items: [
      {
        accountId: "client",
        accountName: "Due-date client",
        queueId: "due-date",
        notificationStatus: "pending",
        deadlines: { contract: { date: "2026-12-01", status, reasonableEfforts } },
      },
    ],
  }).items[0];
  mount(true, "draft", {}, [], "/accounts/reports?view=queue", [item]);
  const row = (await screen.findByText("Due-date client")).closest("tr")!;
  if (status === "authoritative" && !reasonableEfforts) expect(within(row).getByText("2026-12-01")).toBeInTheDocument();
  else expect(within(row).queryByText("2026-12-01")).not.toBeInTheDocument();
});
