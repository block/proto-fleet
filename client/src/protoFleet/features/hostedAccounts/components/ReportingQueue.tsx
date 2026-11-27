import { useCallback, useLayoutEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

import { ACCOUNT_READ_TIMEOUT_MS, hostedAccountsClient } from "../api";
import {
  reportDueDate,
  reportLabel,
  reportPeriod,
  reportPeriodSummary,
  reportTime,
  reportWorkflow,
} from "../reportFormat";
import {
  calendarDay,
  calendarDayStart,
  type CalendarRange,
  calendarToday,
  scheduledPeriodOptions,
} from "../reportPeriodOptions";
import { useAccountQuery } from "../useAccountQuery";
import { type HostReportFile, useReportDownload } from "../useReportDownload";
import { AccountState } from "./AccountState";
import ReportContent from "./ReportContent";
import ReportingActivity from "./ReportingActivity";
import { ReportingError } from "./ReportingError";
import { ReportingDate, ReportingForm } from "./ReportingForm";
import ReportingSetup from "./ReportingSetup";
import ReportPeriodFields from "./ReportPeriodFields";
import ReportPublicationHistory from "./ReportPublicationHistory";
import {
  type HostReport,
  type HostReportingSetup,
  type HostReportQueueItem,
  type HostReportRequest,
  type HostReportSummary,
  type ReportingPeriod,
  ReportingPeriodSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";
import { createOperationID } from "@/protoFleet/utils/crypto";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes } from "@/shared/components/Button";
import ButtonGroup, { type ButtonProps as GroupButtonProps } from "@/shared/components/ButtonGroup";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";
import Search from "@/shared/components/Search";
import Select from "@/shared/components/Select";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import Textarea from "@/shared/components/Textarea";

const useReportingLifetime = () => {
  const [lifetime] = useState(() => {
    let controller = new AbortController();
    return {
      get signal() {
        return controller.signal;
      },
      activate: () => {
        if (controller.signal.aborted) controller = new AbortController();
      },
      dispose: () => controller.abort(),
    };
  });
  useLayoutEffect(() => {
    lifetime.activate();
    return () => lifetime.dispose();
  }, [lifetime]);
  return lifetime;
};
const PrepareReport = ({
  setup,
  signal,
  seed,
  existing,
  replacement,
  request,
  onDismiss,
  onChanged,
  onPrepared,
}: {
  setup: HostReportingSetup;
  signal: AbortSignal;
  seed?: ReportingPeriod;
  existing?: HostReport;
  replacement?: HostReport;
  request?: HostReportRequest;
  onDismiss: () => void;
  onChanged: () => void;
  onPrepared: (id: string) => void;
}) => {
  const username = useFleetStore((state) => state.auth.username);
  const [period, setPeriod] = useState(() =>
    seed
      ? create(ReportingPeriodSchema, { ...seed, ...(replacement ? { asOf: timestampFromDate(new Date()) } : {}) })
      : create(ReportingPeriodSchema, {
          kind: request ? "ad_hoc" : "monthly",
          timezone: setup.input?.configuration?.timezone ?? "",
          asOf: timestampFromDate(new Date()),
          requestRef: request?.requestId,
          requestReceivedAt: request?.receivedAt,
        }),
  );
  const zone = period.timezone || "UTC";
  const configuration = setup.input?.configuration;
  const yearStart = { month: configuration?.reportingYearMonth || 1, day: configuration?.reportingYearDay || 1 };
  const latestPeriod = (kind: string): Partial<CalendarRange> =>
    scheduledPeriodOptions(kind, calendarToday(zone), yearStart)[0] ?? {};
  // Calendar days the user chose. Until they choose, a seeded period keeps its exact instants.
  const [chosen, setChosen] = useState<Partial<CalendarRange> | undefined>(() =>
    seed ? undefined : latestPeriod(period.kind),
  );
  const range = chosen ?? { start: calendarDay(period.start, zone), end: calendarDay(period.end, zone) };
  const start = chosen ? calendarDayStart(chosen.start, zone) : period.start;
  const end = chosen ? calendarDayStart(chosen.end, zone) : period.end;
  const [editingAsOf, setEditingAsOf] = useState(false);
  const [owner, setOwner] = useState(() => setup.owners.find((owner) => owner.username === username)?.userId ?? "");
  const [reason, setReason] = useState("");
  const valid =
    !!start &&
    !!end &&
    !!period.asOf &&
    end.seconds > start.seconds &&
    !!period.timezone &&
    !!owner &&
    (!replacement || !!reason.trim()) &&
    (period.kind !== "ad_hoc" || (!!period.requestRef.trim() && !!period.requestReceivedAt));
  return (
    <ReportingForm
      title={replacement ? "Replace report" : existing ? "Recalculate draft" : "Create report"}
      confirm={existing ? "Recalculate draft" : "Create draft"}
      signal={signal}
      valid={valid}
      onDismiss={onDismiss}
      onConflict={onChanged}
      onSubmit={async (requestSignal) => {
        const response = await hostedAccountsClient.prepareHostReport(
          {
            accountId: setup.accountId,
            period: { ...period, start, end },
            inputRevision: setup.input!.revision,
            ownerUserId: owner,
            reportId: existing?.report?.reportId,
            expectedRevision: existing?.report?.revision ?? 0n,
            correctionOf: replacement?.report?.reportId,
            correctionReason: reason.trim(),
          },
          { signal: requestSignal },
        );
        if (!requestSignal.aborted) {
          onChanged();
          if (response.report?.report) onPrepared(response.report.report.reportId);
        }
      }}
    >
      {replacement ? (
        <p>{reportPeriod(seed)}</p>
      ) : (
        <>
          <Select
            id="report-kind"
            label="Report type"
            value={period.kind}
            options={(request || seed?.kind === "ad_hoc" ? ["ad_hoc"] : ["monthly", "quarterly", "annual"]).map(
              (value) => ({ value, label: reportLabel(value) }),
            )}
            onChange={(kind) => {
              if (kind === period.kind) return;
              setPeriod({
                ...period,
                kind,
                requestRef: kind === "ad_hoc" ? period.requestRef : "",
                requestReceivedAt: kind === "ad_hoc" ? period.requestReceivedAt : undefined,
              });
              setChosen(latestPeriod(kind));
            }}
          />
          <ReportPeriodFields
            kind={period.kind}
            timezone={zone}
            yearStart={yearStart}
            range={range}
            onChange={setChosen}
          />
        </>
      )}
      {editingAsOf ? (
        <ReportingDate
          id="period-as-of"
          timezone={zone}
          label="Evidence as of"
          value={period.asOf}
          onChange={(asOf) => setPeriod({ ...period, asOf })}
        />
      ) : (
        <div className="flex items-center justify-between gap-4">
          <p>Evidence as of {reportTime(period.asOf, zone)}</p>
          <Button variant="textOnly" onClick={() => setEditingAsOf(true)}>
            Change
          </Button>
        </div>
      )}
      {!replacement ? (
        <Input
          id="period-timezone"
          label="Reporting timezone"
          initValue={period.timezone}
          onChange={(timezone) => setPeriod({ ...period, timezone })}
        />
      ) : null}
      {period.kind === "ad_hoc" ? (
        <div>
          <p>Request received: {reportTime(period.requestReceivedAt)}</p>
          {request ? <p>Request evidence: {request.sourceRef}</p> : null}
        </div>
      ) : null}
      <Select
        id="report-owner"
        label="Accountable reporting owner"
        value={owner}
        options={setup.owners.map((owner) => ({ value: owner.userId, label: owner.username }))}
        onChange={setOwner}
      />
      {replacement ? (
        <Textarea
          id="replacement-reason"
          label="Replacement reason (visible to client)"
          initValue={reason}
          onChange={setReason}
        />
      ) : null}
      {editingAsOf ? (
        <p>
          Enter the time in the reporting timezone. A time skipped or repeated by a clock change must be adjusted to a
          unique local time.
        </p>
      ) : null}
    </ReportingForm>
  );
};

const ReportRequestForm = ({
  setup,
  signal,
  onDismiss,
  onChanged,
}: {
  setup: HostReportingSetup;
  signal: AbortSignal;
  onDismiss: () => void;
  onChanged: () => void;
}) => {
  const [receivedAt, setReceivedAt] = useState<HostReportRequest["receivedAt"]>();
  const [targetAt, setTargetAt] = useState<HostReportRequest["targetAt"]>();
  const [requester, setRequester] = useState("");
  const [scope, setScope] = useState("");
  const [sourceRef, setSourceRef] = useState("");
  const [ownerUserId, setOwner] = useState("");
  const [limitations, setLimitations] = useState("");
  const [communicationRef, setCommunication] = useState("");
  const [deliveryRef, setDelivery] = useState("");
  const valid = Boolean(receivedAt && requester.trim() && scope.trim() && sourceRef.trim() && ownerUserId);
  return (
    <ReportingForm
      title="Record ad hoc request"
      confirm="Record request"
      signal={signal}
      valid={valid}
      onDismiss={onDismiss}
      onConflict={onChanged}
      onSubmit={async (requestSignal) => {
        await hostedAccountsClient.createHostReportRequest(
          {
            accountId: setup.accountId,
            request: {
              receivedAt,
              targetAt,
              requester: requester.trim(),
              scope: scope.trim(),
              sourceRef: sourceRef.trim(),
              ownerUserId,
              limitations,
              communicationRef,
              deliveryRef,
            },
          },
          { signal: requestSignal },
        );
        if (!requestSignal.aborted) onChanged();
      }}
    >
      <ReportingDate id="request-received" label="Actual receipt time" value={receivedAt} onChange={setReceivedAt} />
      <Input id="request-requester" label="Requester" initValue={requester} onChange={setRequester} />
      <Textarea id="request-scope" label="Requested scope" initValue={scope} onChange={setScope} />
      <Input id="request-source" label="Request evidence reference" initValue={sourceRef} onChange={setSourceRef} />
      <Select
        id="request-owner"
        label="Accountable owner"
        value={ownerUserId}
        options={setup.owners.map((owner) => ({ value: owner.userId, label: owner.username }))}
        onChange={setOwner}
      />
      <ReportingDate id="request-target" label="Reasonable-efforts target" value={targetAt} onChange={setTargetAt} />
      <Textarea id="request-limitations" label="Limitations" initValue={limitations} onChange={setLimitations} />
      <Input
        id="request-communication"
        label="Communication evidence reference"
        initValue={communicationRef}
        onChange={setCommunication}
      />
      <Input
        id="request-delivery"
        label="Existing delivery evidence reference"
        initValue={deliveryRef}
        onChange={setDelivery}
      />
    </ReportingForm>
  );
};

const HostReportDetail = ({
  reportId,
  setup,
  signal,
  onChanged,
  onSelect,
}: {
  reportId: string;
  setup: HostReportingSetup;
  signal: AbortSignal;
  onChanged: () => void;
  onSelect: (id: string) => void;
}) => {
  const [action, setAction] = useState<"publish" | "delivery" | "replacement" | "recalculate" | null>(null);
  const [actionReport, setActionReport] = useState<HostReport>();
  const [reason, setReason] = useState("");
  const [deliveryStatus, setDeliveryStatus] = useState("attempted");
  const [channel, setChannel] = useState(setup.input?.configuration?.deliveryChannel ?? "");
  const [sentAt, setSentAt] = useState<HostReportRequest["receivedAt"]>();
  const [receivedAt, setReceivedAt] = useState<HostReportRequest["receivedAt"]>();
  const [sourceRef, setSourceRef] = useState("");
  const [failureCategory, setFailureCategory] = useState("unconfirmed");
  const [evidence, setEvidence] = useState("");
  const [deliveryRequestId, setDeliveryRequestId] = useState(() => createOperationID());
  const [denied, setDenied] = useState(false);
  const load = useCallback(
    async (requestSignal: AbortSignal) => {
      const options = { signal: AbortSignal.any([signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const response = await hostedAccountsClient.getHostReport({ accountId: setup.accountId, reportId }, options);
      const seriesId = response.report?.report?.seriesId;
      const publications = seriesId
        ? await hostedAccountsClient.listHostReports({ accountId: setup.accountId, seriesId, pageSize: 100 }, options)
        : undefined;
      return { ...response, versions: publications?.reports ?? [] };
    },
    [setup.accountId, reportId, signal],
  );
  const loadPublication = useCallback(
    async (id: string, requestSignal: AbortSignal) => {
      const response = await hostedAccountsClient.getHostReport(
        { accountId: setup.accountId, reportId: id },
        {
          signal: AbortSignal.any([signal, requestSignal]),
          timeoutMs: ACCOUNT_READ_TIMEOUT_MS,
        },
      );
      return response.report?.report;
    },
    [setup.accountId, signal],
  );
  const query = useAccountQuery(load, !denied, true, POLL_INTERVAL_MS);
  const changed = () => {
    query.refresh();
    onChanged();
  };
  const exporting = useReportDownload<HostReportFile>(
    reportId,
    signal,
    (name, requestSignal) =>
      hostedAccountsClient.exportHostReport({ accountId: setup.accountId, reportId, name }, { signal: requestSignal }),
    () => {
      setDenied(true);
      query.invalidate();
      onChanged();
    },
  );
  const versions = query.data?.versions ?? [];
  const detail = query.data?.report;
  const report = detail?.report;
  const open = (nextAction: typeof action) => {
    setReason("");
    setActionReport(detail);
    if (nextAction === "delivery") {
      setDeliveryRequestId(createOperationID());
      setDeliveryStatus("attempted");
      setSentAt(undefined);
      setReceivedAt(undefined);
      setSourceRef("");
      setEvidence("");
    }
    setAction(nextAction);
  };
  const actions = {
    publish: { title: "Publish report", confirm: "Publish report" },
    delivery: { title: "Record report delivery", confirm: "Record delivery" },
  };
  const actionSnapshot = actionReport?.report ?? report;
  const actionValid = () => {
    switch (action) {
      case "publish":
        return !!actionSnapshot?.result?.complete || !!reason.trim();
      case "delivery":
        return (
          !!channel.trim() &&
          !!sourceRef.trim() &&
          (deliveryStatus !== "confirmed" || (!!receivedAt && !!sentAt && !!evidence.trim())) &&
          (deliveryStatus !== "failed" || !!failureCategory)
        );
      default:
        return true;
    }
  };
  const replaced =
    !!report?.seriesId &&
    versions.some(
      ({ report: item }) =>
        item?.seriesId === report.seriesId && item.status === "issued" && item.number > report.number,
    );
  const pendingReplacement = versions.find(
    ({ report: item }) => !!report && !!item && item.correctionOf === report.reportId && item.status !== "issued",
  )?.report;
  const buttons: GroupButtonProps[] = [];
  const menuActions: RowAction[] = [];
  if (report && detail) {
    if (
      setup.canCreate &&
      setup.input &&
      report.status === "draft" &&
      (report.period?.kind !== "ad_hoc" || setup.canReadRequests)
    )
      buttons.push({ variant: "secondary", text: "Recalculate draft", onClick: () => open("recalculate") });
    if (report.status === "issued") {
      menuActions.push({
        label: "Download source inputs",
        disabled: exporting.busy,
        onClick: () => void exporting.download("source-inputs.json"),
      });
      buttons.push(
        {
          variant: "secondary",
          text: "Download report HTML",
          disabled: exporting.busy,
          onClick: () => void exporting.download("report.html"),
        },
        {
          variant: "secondary",
          text: "Download evidence ZIP",
          disabled: exporting.busy,
          onClick: () => void exporting.download("evidence.zip"),
        },
      );
      if (setup.canCreate && (report.period?.kind !== "ad_hoc" || setup.canCreateRequests))
        buttons.push({ variant: "secondary", text: "Record report delivery", onClick: () => open("delivery") });
      if (!replaced && setup.canCreate && setup.input && (report.period?.kind !== "ad_hoc" || setup.canReadRequests))
        menuActions.push(
          pendingReplacement
            ? { label: "Open replacement draft", onClick: () => onSelect(pendingReplacement.reportId) }
            : { label: "Replace report", onClick: () => open("replacement") },
        );
    }
    if (detail.canPublish) buttons.push({ variant: "primary", text: "Publish report", onClick: () => open("publish") });
  }
  return (
    <section aria-label="Host report detail" className="flex min-w-0 flex-col gap-6">
      <AccountState
        title={report && !denied ? "Could not refresh report" : undefined}
        loading={query.loading}
        error={denied ? "Report access is no longer available." : query.error}
        retry={query.refresh}
      />
      {report && !denied ? (
        <>
          {exporting.error ? <ReportingError error={exporting.error} /> : null}
          <ReportContent
            report={report}
            replaced={replaced}
            actions={
              <div className="flex flex-wrap items-center gap-3">
                <ButtonGroup buttons={buttons} variant="rightAligned" size={sizes.compact} className="flex-wrap" />
                <RowActionsMenu actions={menuActions} ariaLabel="Report actions" triggerClassName="" />
              </div>
            }
            publicationHistory={
              <ReportPublicationHistory
                report={report}
                publications={versions.flatMap((version) => (version.report ? [version.report] : []))}
                loadReport={loadPublication}
                onSelect={onSelect}
              />
            }
          />
          {detail.deliveries.length ? (
            <section aria-label="Delivery history" className="text-300">
              <h3 className="mb-3 text-emphasis-300">Delivery history</h3>
              {detail.deliveries.map((item) => (
                <div key={item.requestId} className="mb-3">
                  <p>
                    {reportLabel(item.status)} · {item.channel} ·{" "}
                    {reportTime(item.receivedAt ?? item.sentAt ?? item.createdAt)}
                  </p>
                  <p>{item.sourceRef}</p>
                  {item.failureCategory ? <p>{reportLabel(item.failureCategory)}</p> : null}
                  {item.evidence ? <p>{item.evidence}</p> : null}
                </div>
              ))}
            </section>
          ) : null}
          {action === "replacement" || action === "recalculate" ? (
            <PrepareReport
              setup={setup}
              signal={signal}
              seed={actionSnapshot?.period}
              replacement={action === "replacement" ? actionReport : undefined}
              existing={action === "recalculate" ? actionReport : undefined}
              onDismiss={() => setAction(null)}
              onChanged={changed}
              onPrepared={onSelect}
            />
          ) : action ? (
            <ReportingForm
              title={actions[action].title}
              confirm={actions[action].confirm}
              signal={signal}
              valid={actionValid()}
              onDismiss={() => setAction(null)}
              onConflict={changed}
              onSubmit={async (requestSignal) => {
                const identity = { accountId: setup.accountId, reportId, expectedRevision: actionSnapshot!.revision };
                if (action === "publish")
                  await hostedAccountsClient.publishHostReport(
                    { ...identity, exception: reason.trim() },
                    { signal: requestSignal },
                  );
                else
                  await hostedAccountsClient.recordHostReportDelivery(
                    {
                      accountId: setup.accountId,
                      reportId,
                      requestId: deliveryRequestId,
                      channel: channel.trim(),
                      status: deliveryStatus,
                      sentAt,
                      receivedAt,
                      sourceRef: sourceRef.trim(),
                      failureCategory: deliveryStatus === "failed" ? failureCategory : "",
                      evidence,
                    },
                    { signal: requestSignal },
                  );
                if (!requestSignal.aborted) changed();
              }}
            >
              {action === "publish" ? (
                <>
                  <p>
                    Publishing freezes this report and makes it available to the client. Record delivery separately
                    through the agreed channel.
                  </p>
                  {!actionSnapshot?.result?.complete ? (
                    <Textarea
                      id="publish-exception"
                      label="Incomplete-report explanation"
                      initValue={reason}
                      onChange={setReason}
                    />
                  ) : null}
                </>
              ) : (
                <>
                  <Select
                    id="delivery-status"
                    label="Delivery outcome"
                    value={deliveryStatus}
                    options={["attempted", "failed", "confirmed"].map((value) => ({
                      value,
                      label: reportLabel(value),
                    }))}
                    onChange={setDeliveryStatus}
                  />
                  <Input
                    id="delivery-channel"
                    label="Actual delivery channel"
                    initValue={channel}
                    onChange={setChannel}
                  />
                  <ReportingDate id="delivery-sent" label="Actual sent time" value={sentAt} onChange={setSentAt} />
                  <ReportingDate
                    id="delivery-received"
                    label="Confirmed receipt time"
                    value={receivedAt}
                    onChange={setReceivedAt}
                  />
                  <Input
                    id="delivery-source"
                    label="Delivery evidence reference"
                    initValue={sourceRef}
                    onChange={setSourceRef}
                  />
                  {deliveryStatus === "failed" ? (
                    <Select
                      id="delivery-failure"
                      label="Failure category"
                      value={failureCategory}
                      options={["unavailable", "rejected", "unconfirmed", "other"].map((value) => ({
                        value,
                        label: reportLabel(value),
                      }))}
                      onChange={setFailureCategory}
                    />
                  ) : null}
                  <Textarea
                    id="delivery-evidence"
                    label="Delivery evidence or failure details"
                    initValue={evidence}
                    onChange={setEvidence}
                  />
                </>
              )}
            </ReportingForm>
          ) : null}
        </>
      ) : null}
    </section>
  );
};

const RequestFulfillmentForm = ({
  accountId,
  request,
  signal,
  onDismiss,
  onChanged,
}: {
  accountId: string;
  request: HostReportRequest;
  signal: AbortSignal;
  onDismiss: () => void;
  onChanged: () => void;
}) => {
  const [entryKey] = useState(() => createOperationID());
  const [deliveredAt, setDeliveredAt] = useState<HostReportRequest["fulfilledAt"]>();
  const [deliveryRef, setDeliveryRef] = useState("");
  const [communicationRef, setCommunicationRef] = useState("");
  const [evidence, setEvidence] = useState("");
  return (
    <ReportingForm
      title="Record external fulfillment"
      confirm="Record fulfillment"
      signal={signal}
      valid={Boolean(deliveredAt && deliveryRef.trim() && evidence.trim())}
      onDismiss={onDismiss}
      onConflict={onChanged}
      onSubmit={async (requestSignal) => {
        await hostedAccountsClient.recordHostReportRequestFulfillment(
          {
            accountId,
            requestId: request.requestId,
            entryKey,
            deliveredAt,
            deliveryRef: deliveryRef.trim(),
            communicationRef: communicationRef.trim(),
            evidence: evidence.trim(),
          },
          { signal: requestSignal },
        );
        if (!requestSignal.aborted) onChanged();
      }}
    >
      <p>Record evidence of the requested package already delivered outside Fleet.</p>
      <ReportingDate
        id="fulfillment-delivered"
        label="Actual delivery time"
        value={deliveredAt}
        onChange={setDeliveredAt}
      />
      <Input
        id="fulfillment-reference"
        label="Delivery evidence reference"
        initValue={deliveryRef}
        onChange={setDeliveryRef}
      />
      <Input
        id="fulfillment-communication"
        label="Communication reference"
        initValue={communicationRef}
        onChange={setCommunicationRef}
      />
      <Textarea
        id="fulfillment-evidence"
        label="Delivered package and confirmation evidence"
        initValue={evidence}
        onChange={setEvidence}
      />
    </ReportingForm>
  );
};

const ReportingAccount = ({
  accountId,
  queueItem,
  reportId,
  onSelectReport,
  onChanged,
  view,
}: {
  accountId: string;
  queueItem?: HostReportQueueItem;
  reportId: string;
  onSelectReport: (id: string) => void;
  onChanged: () => void;
  view: string;
}) => {
  const lifetime = useReportingLifetime();
  const [preparing, setPreparing] = useState(false);
  const [newRequest, setNewRequest] = useState(false);
  const [fulfillingRequest, setFulfillingRequest] = useState<HostReportRequest>();
  const [request, setRequest] = useState<HostReportRequest>();
  const [pageCount, setPageCount] = useState(1);
  const [requestPageCount, setRequestPageCount] = useState(1);
  const load = useCallback(
    async (requestSignal: AbortSignal) => {
      const options = { signal: AbortSignal.any([lifetime.signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const { setup } = await hostedAccountsClient.getHostReportingSetup({ accountId }, options);
      const [reports, firstRequests] = await Promise.all([
        setup?.canCreate ? hostedAccountsClient.listHostReports({ accountId, pageSize: 50 }, options) : undefined,
        setup?.canReadRequests
          ? hostedAccountsClient.listHostReportRequests({ accountId, pageSize: 50 }, options)
          : undefined,
      ]);
      const summaries = [...(reports?.reports ?? [])];
      let nextPageToken = reports?.nextPageToken;
      for (let page = 1; page < pageCount && nextPageToken; page += 1) {
        const response = await hostedAccountsClient.listHostReports(
          { accountId, pageSize: 50, pageToken: nextPageToken },
          options,
        );
        summaries.push(...response.reports);
        nextPageToken = response.nextPageToken;
      }
      const requests = [...(firstRequests?.requests ?? [])];
      let nextRequestPageToken = firstRequests?.nextPageToken;
      for (let page = 1; page < requestPageCount && nextRequestPageToken; page += 1) {
        const response = await hostedAccountsClient.listHostReportRequests(
          { accountId, pageSize: 50, pageToken: nextRequestPageToken },
          options,
        );
        requests.push(...response.requests);
        nextRequestPageToken = response.nextPageToken;
      }
      return { setup, reports: summaries, nextPageToken, requests, nextRequestPageToken };
    },
    [accountId, lifetime, pageCount, requestPageCount],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const changed = () => {
    query.refresh();
    onChanged();
  };
  const currentReports = useMemo(() => {
    const reports = query.data?.reports ?? [];
    const latest = new Map<string, bigint>();
    for (const { report } of reports) {
      if (report?.status !== "issued") continue;
      const series = report.seriesId || report.reportId;
      if (report.number > (latest.get(series) ?? 0n)) latest.set(series, report.number);
    }
    return reports.filter(
      ({ report }) =>
        report && (report.status !== "issued" || report.number === latest.get(report.seriesId || report.reportId)),
    );
  }, [query.data?.reports]);
  const setup = query.data?.setup;
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <AccountState
        title={setup ? "Could not refresh reporting" : undefined}
        loading={query.loading}
        error={query.error}
        retry={query.refresh}
      />
      {setup ? (
        <>
          {view === "settings" ? (
            <ReportingSetup
              key={`${setup.accountId}:${setup.authorities.map((authority) => authority.revision).join(":")}`}
              setup={setup}
              signal={lifetime.signal}
              onChanged={changed}
            />
          ) : null}
          {view === "requests" && !setup.canReadRequests ? (
            <p role="status">Report requests are unavailable for this account.</p>
          ) : null}
          {view === "reports" && !reportId && setup.canCreate ? (
            <section aria-label="Prepared reports" className="flex min-w-0 flex-col gap-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-heading-200">Prepared reports</h2>
                {setup.canCreate ? (
                  <Button
                    variant="primary"
                    size={sizes.compact}
                    text="Create report"
                    disabled={!setup.input}
                    onClick={() => {
                      setRequest(undefined);
                      setPreparing(true);
                    }}
                  />
                ) : null}
              </div>
              {setup.canCreate && !setup.input ? (
                <p>
                  Choose a reporting timezone in{" "}
                  <Link
                    className="underline"
                    to={{ search: new URLSearchParams({ account: accountId, view: "settings" }).toString() }}
                  >
                    Reporting settings
                  </Link>{" "}
                  before creating a report.
                </p>
              ) : null}
              <List<HostReportSummary & { key: string }, string, "period" | "status" | "quality" | "delivery" | "due">
                items={currentReports
                  .filter((item) => !!item.report)
                  .map((item) => ({ ...item, key: item.report!.reportId }))}
                itemKey="key"
                itemSelectable={false}
                onRowClick={(item) => onSelectReport(item.report!.reportId)}
                stickyFirstColumn={false}
                activeCols={["period", "status", "quality", "delivery", "due"]}
                colTitles={{
                  period: "Report",
                  status: "Workflow",
                  quality: "Data quality",
                  delivery: "Delivery",
                  due: "Contractual due date",
                }}
                colConfig={{
                  period: {
                    width: "min-w-64",
                    allowWrap: true,
                    component: (item) => reportPeriodSummary(item.report?.period),
                  },
                  status: {
                    width: "min-w-40",
                    component: (item) => reportWorkflow(item.report!.status),
                  },
                  quality: {
                    width: "min-w-40",
                    allowWrap: true,
                    component: (item) => (item.report!.complete ? "Complete" : "Required information missing"),
                  },
                  delivery: {
                    width: "min-w-32",
                    component: (item) =>
                      item.report!.status === "issued"
                        ? reportLabel(item.report!.deliveryStatus || "unconfirmed")
                        : "Not published",
                  },
                  due: {
                    width: "min-w-40",
                    component: (item) => {
                      const due = item.report!.deadlines?.contract;
                      return reportDueDate(due);
                    },
                  },
                }}
                noDataElement={<p>No reports.</p>}
              />
              {query.data?.nextPageToken ? (
                <Button
                  variant="secondary"
                  text="Load more reports"
                  disabled={query.loading || query.refreshing}
                  onClick={() => setPageCount((count) => count + 1)}
                />
              ) : null}
            </section>
          ) : null}
          {view === "reports" && reportId && setup.canCreate ? (
            <HostReportDetail
              key={reportId}
              reportId={reportId}
              setup={setup}
              signal={lifetime.signal}
              onChanged={changed}
              onSelect={onSelectReport}
            />
          ) : null}
          {view === "requests" && setup.canReadRequests ? (
            <section aria-label="Ad hoc report requests" className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-heading-200">Ad hoc requests</h2>
                {setup.canCreateRequests ? (
                  <Button
                    variant="secondary"
                    size={sizes.compact}
                    text="Record ad hoc request"
                    onClick={() => setNewRequest(true)}
                  />
                ) : null}
              </div>
              {query.data?.requests.length ? (
                query.data.requests.map((item) => (
                  <details key={item.requestId}>
                    <summary className="cursor-pointer">
                      {item.requester} · Received {reportTime(item.receivedAt)}
                    </summary>
                    <div className="mt-3 flex flex-col gap-2">
                      <p>{item.scope}</p>
                      <p>
                        Owner:{" "}
                        {setup.owners.find((owner) => owner.userId === item.ownerUserId)?.username ||
                          "Assigned reporting owner"}
                      </p>
                      <p>Reasonable-efforts target: {reportTime(item.targetAt)}</p>
                      {item.limitations ? <p>{item.limitations}</p> : null}
                      <p>Evidence: {item.sourceRef}</p>
                      {item.communicationRef ? <p>Communications: {item.communicationRef}</p> : null}
                      <p>{item.fulfilledAt ? "Fulfilled · delivery confirmed" : "Delivery unconfirmed"}</p>
                      {item.deliveryRef ? <p>Recorded delivery evidence: {item.deliveryRef}</p> : null}
                      {item.fulfillmentRef ? <p>Fulfillment evidence: {item.fulfillmentRef}</p> : null}
                      {setup.canCreateRequests && !item.fulfilledAt ? (
                        <Button
                          variant="secondary"
                          text="Record external fulfillment"
                          onClick={() => setFulfillingRequest(item)}
                        />
                      ) : null}
                      {setup.canCreate && setup.input ? (
                        <Button
                          variant="secondary"
                          text="Create requested report"
                          onClick={() => {
                            setRequest(item);
                            setPreparing(true);
                          }}
                        />
                      ) : null}
                    </div>
                  </details>
                ))
              ) : (
                <p>No ad hoc requests.</p>
              )}
              {query.data?.nextRequestPageToken ? (
                <Button
                  variant="secondary"
                  text="Load more requests"
                  disabled={query.loading || query.refreshing}
                  onClick={() => setRequestPageCount((count) => count + 1)}
                />
              ) : null}
            </section>
          ) : null}
          {preparing && setup.canCreate && (!request || setup.canReadRequests) ? (
            <PrepareReport
              setup={setup}
              signal={lifetime.signal}
              seed={request ? undefined : queueItem?.period}
              request={request}
              onDismiss={() => setPreparing(false)}
              onChanged={changed}
              onPrepared={onSelectReport}
            />
          ) : null}
          {fulfillingRequest && setup.canCreateRequests ? (
            <RequestFulfillmentForm
              accountId={accountId}
              request={fulfillingRequest}
              signal={lifetime.signal}
              onDismiss={() => setFulfillingRequest(undefined)}
              onChanged={changed}
            />
          ) : null}
          {newRequest && setup.canCreateRequests ? (
            <ReportRequestForm
              setup={setup}
              signal={lifetime.signal}
              onDismiss={() => setNewRequest(false)}
              onChanged={changed}
            />
          ) : null}
        </>
      ) : null}
    </div>
  );
};

const ReportingQueue = () => {
  const [params, setParams] = useSearchParams();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const lifetime = useReportingLifetime();
  const [queueAction, setQueueAction] = useState<{
    item: HostReportQueueItem;
    kind: "acknowledge" | "retry";
    scope: symbol;
  }>();
  const [reason, setReason] = useState("");
  const [search, setSearch] = useState("");
  const [pageCount, setPageCount] = useState(1);
  const load = useCallback(
    async (requestSignal: AbortSignal) => {
      const options = { signal: AbortSignal.any([lifetime.signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const [accounts, firstPage] = await Promise.all([
        hostedAccountsClient.listHostReportingAccounts({}, options),
        hostedAccountsClient.listHostReportQueue({ pageSize: 50 }, options),
      ]);
      let queue = firstPage;
      const items = [...queue.items];
      for (let page = 1; page < pageCount && queue.nextPageToken; page += 1) {
        queue = await hostedAccountsClient.listHostReportQueue(
          { pageSize: 50, pageToken: queue.nextPageToken },
          options,
        );
        items.push(...queue.items);
      }
      return { accounts: accounts.accounts, items, nextPageToken: queue.nextPageToken };
    },
    [lifetime, pageCount],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const requestedAccount = params.get("account") || "";
  const selectedAccount = requestedAccount || query.data?.accounts[0]?.accountId || "";
  const account = query.data?.accounts.find((account) => account.accountId === selectedAccount);
  const queueId = params.get("queue") || "";
  const hasQueueTarget = params.has("queue");
  const canResolveTarget = !!requestedAccount && !!queueId && !!account;
  const loadSelectedQueueItem = useCallback(
    async (requestSignal: AbortSignal) => {
      const options = { signal: AbortSignal.any([lifetime.signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const tokens = new Set<string>();
      let pageToken = "";
      while (!options.signal.aborted) {
        if (tokens.has(pageToken))
          throw new Error("The reporting obligation lookup could not advance. Retry the lookup.");
        tokens.add(pageToken);
        const page = await hostedAccountsClient.listHostReportQueue({ pageSize: 50, pageToken }, options);
        if (options.signal.aborted) return null;
        const item = page.items.find((item) => item.queueId === queueId);
        if (item) return item.accountId === requestedAccount ? item : null;
        if (!page.nextPageToken) return null;
        pageToken = page.nextPageToken;
      }
      return null;
    },
    [lifetime, requestedAccount, queueId],
  );
  const selectedQueue = useAccountQuery(loadSelectedQueueItem, canResolveTarget, true, POLL_INTERVAL_MS);
  const queueItem = selectedQueue.data ?? undefined;
  const refresh = () => {
    query.refresh();
    selectedQueue.refresh();
  };
  const queueActionScope = useMemo(
    () => Symbol(`${requestedAccount}:${queueId}:${queueItem?.generation}`),
    [requestedAccount, queueId, queueItem?.generation],
  );
  const currentQueueAction =
    queueAction &&
    queueAction.scope === queueActionScope &&
    queueItem?.queueId === queueAction.item.queueId &&
    queueItem.accountId === queueAction.item.accountId &&
    queueItem.generation === queueAction.item.generation
      ? queueAction
      : undefined;
  const requestedView =
    params.get("view") || (params.has("account") ? (account && !account.canCreate ? "settings" : "reports") : "queue");
  const view = ["queue", "reports", "requests", "settings"].includes(requestedView) ? requestedView : "queue";
  const inaccessible =
    !account || (view === "reports" && !account.canCreate) || (view === "requests" && !account.canCreate);
  const select = (accountId: string, reportId = "", queueId = "", nextView = "reports") => {
    const next = new URLSearchParams();
    if (accountId) next.set("account", accountId);
    next.set("view", nextView);
    if (reportId) next.set("report", reportId);
    if (queueId) next.set("queue", queueId);
    setParams(next);
  };
  const openQueueItem = (item: HostReportQueueItem) =>
    select(item.accountId, item.report?.report?.reportId, item.queueId, item.request ? "requests" : "reports");
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <h1 className="text-heading-300">Reports</h1>
      <AccountState
        title={query.data ? "Could not refresh reports" : undefined}
        loading={query.loading}
        error={query.error}
        retry={refresh}
      />
      {hasQueueTarget ? (
        <AccountState
          title={queueItem ? "Could not refresh reporting obligation" : undefined}
          loading={canResolveTarget ? selectedQueue.loading || (!queueItem && selectedQueue.refreshing) : false}
          error={!requestedAccount || !queueId ? "The reporting obligation link is incomplete." : selectedQueue.error}
          retry={refresh}
        />
      ) : null}
      {hasQueueTarget &&
      requestedAccount &&
      queueId &&
      query.data &&
      (!account ||
        (canResolveTarget &&
          !selectedQueue.loading &&
          !selectedQueue.refreshing &&
          !selectedQueue.error &&
          !queueItem)) ? (
        <p role="status">This reporting obligation is no longer available for the selected account.</p>
      ) : null}
      {query.data ? (
        <>
          <div className="overflow-x-auto">
            <TabStrip
              activeId={view}
              ariaLabel="Report workspace"
              onSelect={(nextView) => select(selectedAccount, "", "", nextView)}
            >
              <TabStripItem id="queue" label="Work queue" />
              <TabStripItem id="reports" label="Client reports" />
              {query.data.accounts.some((item) => item.canCreate) ? (
                <TabStripItem id="requests" label="Report requests" />
              ) : null}
              <TabStripItem id="settings" label="Reporting settings" />
            </TabStrip>
          </div>
          {view === "queue" ? (
            <>
              <Search
                id="search-report-queue"
                label="Search clients or reports"
                variant="toolbar"
                initValue={search}
                onChange={setSearch}
              />
              <List<HostReportQueueItem, string, "account" | "period" | "owner" | "workflow" | "due">
                items={query.data.items.filter(
                  (item) =>
                    !["quiet", "superseded"].includes(item.notificationStatus) &&
                    `${item.accountName} ${item.period ? reportPeriodSummary(item.period) : ""} ${item.request?.scope ?? ""} ${item.ownerUsername}`
                      .toLocaleLowerCase()
                      .includes(search.trim().toLocaleLowerCase()),
                )}
                itemKey="queueId"
                itemSelectable={false}
                onRowClick={openQueueItem}
                stickyFirstColumn={false}
                activeCols={["account", "period", "workflow", "due", "owner"]}
                colTitles={{
                  account: "Client",
                  period: "Report",
                  owner: "Owner",
                  workflow: "Workflow",
                  due: "Due date",
                }}
                colConfig={{
                  account: { width: "min-w-40", component: (item) => item.accountName },
                  owner: { width: "min-w-32", component: (item) => item.ownerUsername || "—" },
                  period: {
                    width: "min-w-64",
                    allowWrap: true,
                    component: (item) =>
                      item.period ? reportPeriodSummary(item.period) : item.request?.scope || reportLabel(item.kind),
                  },
                  workflow: {
                    width: "min-w-32",
                    component: (item) =>
                      item.report?.report
                        ? reportWorkflow(item.report.report.status)
                        : item.request?.fulfilledAt
                          ? "Fulfilled"
                          : item.request
                            ? "Requested"
                            : "Preparation due",
                  },
                  due: { width: "min-w-40", component: (item) => reportDueDate(item.deadlines?.contract) },
                }}
                noDataElement={
                  <p>
                    {search.trim()
                      ? "No reporting obligations match your search."
                      : "No reporting obligations in the queue."}
                  </p>
                }
              />
              {query.data.nextPageToken ? (
                <Button
                  variant="secondary"
                  text="Load more obligations"
                  disabled={query.loading || query.refreshing}
                  onClick={() => setPageCount((count) => count + 1)}
                />
              ) : null}
            </>
          ) : null}
          {view !== "queue" && query.data.accounts.length ? (
            <Select
              id="reporting-account"
              label="Client reporting account"
              value={selectedAccount}
              options={query.data.accounts.map((account) => ({ value: account.accountId, label: account.name }))}
              onChange={(accountId) => select(accountId, "", "", view)}
            />
          ) : !query.data.accounts.length ? (
            <p>No client reporting access. An account administrator must grant reporting authority.</p>
          ) : null}
          {view === "reports" && (params.get("report") || queueItem?.report?.report?.reportId) ? (
            <Breadcrumb
              segments={[
                {
                  label: "Client reports",
                  to: `/accounts/reports?${new URLSearchParams({ account: selectedAccount, view: "reports" })}`,
                },
                { label: "Report" },
              ]}
            />
          ) : null}
          {view !== "queue" && inaccessible ? (
            <p role="status">
              This reporting view is unavailable for the selected account. Choose another account or open Reporting
              settings.
            </p>
          ) : null}
          {account && !inaccessible && view !== "queue" && (!hasQueueTarget || queueItem) ? (
            <ReportingAccount
              key={`${selectedAccount}:${generation}:${account.canCreate}:${account.canAdminister}`}
              accountId={selectedAccount}
              queueItem={queueItem}
              reportId={params.get("report") || queueItem?.report?.report?.reportId || ""}
              onSelectReport={(reportId) => select(selectedAccount, reportId, queueItem?.queueId)}
              onChanged={refresh}
              view={view}
            />
          ) : null}
          {queueItem ? (
            <ReportingActivity
              key={`${queueItem.queueId}:${queueItem.generation}:${generation}`}
              item={queueItem}
              onAcknowledge={() => {
                setReason("");
                setQueueAction({ item: queueItem, kind: "acknowledge", scope: queueActionScope });
              }}
              onRetry={() => {
                setReason("");
                setQueueAction({ item: queueItem, kind: "retry", scope: queueActionScope });
              }}
            />
          ) : null}
          {currentQueueAction ? (
            <ReportingForm
              title={
                currentQueueAction.kind === "acknowledge"
                  ? "Acknowledge reporting obligation"
                  : "Retry reporting notification"
              }
              confirm={currentQueueAction.kind === "acknowledge" ? "Acknowledge" : "Retry notification"}
              signal={lifetime.signal}
              valid={!!reason.trim()}
              onDismiss={() => setQueueAction(undefined)}
              onConflict={refresh}
              onSubmit={async (requestSignal) => {
                const request = {
                  accountId: currentQueueAction.item.accountId,
                  queueId: currentQueueAction.item.queueId,
                  expectedGeneration: currentQueueAction.item.generation,
                  reason: reason.trim(),
                };
                if (currentQueueAction.kind === "acknowledge")
                  await hostedAccountsClient.acknowledgeHostReportQueue(request, { signal: requestSignal });
                else await hostedAccountsClient.retryHostReportQueueNotification(request, { signal: requestSignal });
                if (!requestSignal.aborted) refresh();
              }}
            >
              <Textarea id="queue-reason" label="Reason" initValue={reason} onChange={setReason} />
            </ReportingForm>
          ) : null}
        </>
      ) : null}
    </div>
  );
};
export default ReportingQueue;
