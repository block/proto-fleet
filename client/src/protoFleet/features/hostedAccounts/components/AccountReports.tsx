import { useCallback, useEffect, useState } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import { Code } from "@connectrpc/connect";

import { AccountScopeContext, useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { canGenerateAnalyses } from "../capabilities";
import { analysisTypes, reportDate, reportDateRange, reportTitle } from "../reportFormat";
import { useAccountQuery } from "../useAccountQuery";
import { useReportDownload } from "../useReportDownload";
import { AccountAnalysisDetail, GenerateAnalysis } from "./AccountAnalysis";
import { AccountState } from "./AccountState";
import ReportContent from "./ReportContent";
import { ReportingError } from "./ReportingError";
import ReportPublicationHistory from "./ReportPublicationHistory";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useFleetStore } from "@/protoFleet/store";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes } from "@/shared/components/Button";
import List from "@/shared/components/List";
import Search from "@/shared/components/Search";
import Select from "@/shared/components/Select";

const AccountReportDetail = ({ reportId, onSelect }: { reportId: string; onSelect: (id: string) => void }) => {
  const scope = useRequiredAccountScope();
  const [denied, setDenied] = useState(false);
  const load = useCallback(
    async (signal: AbortSignal) => {
      const options = { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const response = await scope.client.getAccountReport({ accountId: scope.account.accountId, reportId }, options);
      const seriesId = response.report?.seriesId;
      const publications = seriesId
        ? await scope.client.listAccountReports({ accountId: scope.account.accountId, seriesId }, options)
        : undefined;
      return { ...response, publications: publications?.reports ?? [] };
    },
    [scope.client, scope.account.accountId, reportId],
  );
  const loadPublication = useCallback(
    async (id: string, requestSignal: AbortSignal) => {
      const response = await scope.client.getAccountReport(
        { accountId: scope.account.accountId, reportId: id },
        {
          signal: AbortSignal.any([scope.signal, requestSignal]),
          timeoutMs: ACCOUNT_READ_TIMEOUT_MS,
        },
      );
      return response.report;
    },
    [scope.client, scope.account.accountId, scope.signal],
  );
  const query = useAccountQuery(load, !denied, true, POLL_INTERVAL_MS);
  const onDenied = () => {
    setDenied(true);
    query.invalidate();
    scope.invalidateAccount();
  };
  const exporting = useReportDownload(
    reportId,
    scope.signal,
    (name, signal) =>
      scope.client.exportAccountReport({ accountId: scope.account.accountId, reportId, name }, { signal }),
    onDenied,
  );
  useEffect(() => {
    const clear = () => setDenied(true);
    scope.signal.addEventListener("abort", clear);
    return () => scope.signal.removeEventListener("abort", clear);
  }, [scope.signal]);
  const report = !denied ? query.data?.report : undefined;
  return (
    <section aria-label="Selected report" className="flex min-w-0 flex-col gap-6">
      <AccountState
        loading={query.loading}
        error={denied ? "Report access is no longer available." : query.error}
        retry={query.refresh}
      />
      {report?.status === "issued" ? (
        <>
          <div className="flex flex-wrap gap-3">
            <Button
              variant="secondary"
              text="Download report HTML"
              disabled={exporting.busy}
              onClick={() => void exporting.download("report.html")}
            />
            <Button
              variant="secondary"
              text="Download evidence ZIP"
              disabled={exporting.busy}
              onClick={() => void exporting.download("evidence.zip")}
            />
          </div>
          {exporting.error ? <ReportingError error={exporting.error} /> : null}
          <ReportContent
            report={report}
            replaced={Boolean(
              report.seriesId &&
              query.data?.publications.some((item) => item.seriesId === report.seriesId && item.number > report.number),
            )}
            publicationHistory={
              <ReportPublicationHistory
                report={report}
                publications={query.data?.publications ?? []}
                loadReport={loadPublication}
                onSelect={onSelect}
              />
            }
          />
        </>
      ) : null}
    </section>
  );
};
const AccountReportsList = () => {
  const scope = useRequiredAccountScope();
  const { account, client } = scope;
  const { pathname } = useLocation();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState("");
  const [kind, setKind] = useState("");
  const [generating, setGenerating] = useState(false);
  const [deletedAnalyses, setDeletedAnalyses] = useState<Set<string>>(() => new Set());
  const [pageTarget, setPageTarget] = useState({ search: "", kind: "", count: 1 });
  const pageCount = pageTarget.search === search && pageTarget.kind === kind ? pageTarget.count : 1;
  const loadReports = useCallback(
    async (signal: AbortSignal) => {
      const options = { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const request = {
        accountId: account.accountId,
        pageSize: 50,
        search: search.trim(),
        kind: kind in analysisTypes ? "" : kind,
      };
      let result = await client.listAccountReports(request, options);
      const reports = [...result.reports];
      for (let page = 1; page < pageCount && result.nextPageToken; page += 1) {
        result = await client.listAccountReports({ ...request, pageToken: result.nextPageToken }, options);
        reports.push(...result.reports);
      }
      return { reports, nextPageToken: result.nextPageToken };
    },
    [account.accountId, client, search, kind, pageCount],
  );
  const loadAnalyses = useCallback(
    async (signal: AbortSignal) => {
      const options = { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS };
      const request = {
        accountId: account.accountId,
        pageSize: 50,
        search: search.trim(),
        kind: kind in analysisTypes ? kind : "",
      };
      let result = await client.listAccountAnalyses(request, options);
      const analyses = [...result.analyses];
      for (let page = 1; page < pageCount && result.nextPageToken; page += 1) {
        result = await client.listAccountAnalyses({ ...request, pageToken: result.nextPageToken }, options);
        analyses.push(...result.analyses);
      }
      return { analyses, nextPageToken: result.nextPageToken };
    },
    [account.accountId, client, search, kind, pageCount],
  );
  const query = useAccountQuery(loadReports, true, true, POLL_INTERVAL_MS);
  const analysesQuery = useAccountQuery(loadAnalyses, true, true, POLL_INTERVAL_MS);
  const refresh = () => {
    query.refresh();
    analysesQuery.refresh();
  };
  const reports = query.data?.reports.filter((report) => report.status === "issued") ?? [];
  const analyses = (analysesQuery.data?.analyses ?? []).filter((analysis) => !deletedAnalyses.has(analysis.analysisId));
  const selectedId = params.get("report");
  const selectedAnalysis = params.get("analysis");
  const rows = [
    ...reports.map((report) => ({
      id: report.reportId,
      analysis: false,
      kind: report.period?.kind ?? "",
      name: reportTitle(report.period?.kind),
      type: report.period?.kind === "ad_hoc" ? "Host-prepared response" : "Official report",
      period: reportDateRange(report.period),
      date: reportDate(report.issuedAt, report.period?.timezone),
      seconds: report.issuedAt?.seconds ?? 0n,
      quality: report.complete ? "Complete" : "Required information missing",
    })),
    ...analyses.map((analysis) => ({
      id: analysis.analysisId,
      analysis: true,
      kind: analysis.kind,
      name: analysis.name,
      type: `Ad hoc · ${analysisTypes[analysis.kind]}`,
      period: reportDateRange(analysis),
      date: reportDate(analysis.createdAt, analysis.timezone),
      seconds: analysis.createdAt?.seconds ?? 0n,
      quality: analysis.limitations.length ? "See limitations" : "—",
    })),
  ].sort((a, b) => (a.seconds > b.seconds ? -1 : a.seconds < b.seconds ? 1 : 0));
  const filtered = rows.filter(
    (row) =>
      (!kind || row.kind === kind) &&
      `${row.name} ${row.type} ${row.period}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()),
  );
  const libraryParams = new URLSearchParams(params);
  libraryParams.delete("report");
  libraryParams.delete("analysis");
  const libraryUrl = `${pathname}${libraryParams.size ? `?${libraryParams}` : ""}`;
  const select = (id: string, analysis = false) => {
    const next = new URLSearchParams(libraryParams);
    if (id) next.set(analysis ? "analysis" : "report", id);
    setParams(next);
  };
  const analysisListed = analyses.some((analysis) => analysis.analysisId === selectedAnalysis);
  const libraryComplete = !!analysesQuery.data && !analysesQuery.data.nextPageToken && !search && !kind;
  const denied = [query.errorCode, analysesQuery.errorCode].some(
    (code) => code === Code.PermissionDenied || code === Code.Unauthenticated,
  );
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {selectedId || selectedAnalysis ? (
          <Breadcrumb segments={[{ label: "Reports", to: libraryUrl }, { label: "Report" }]} />
        ) : (
          <h1 className="text-heading-300">Reports</h1>
        )}
        {canGenerateAnalyses(account, scope.readOnly) && !selectedId && !selectedAnalysis ? (
          <Button variant="primary" size={sizes.compact} text="Generate report" onClick={() => setGenerating(true)} />
        ) : null}
      </div>
      <AccountState loading={query.loading} error={query.error} retry={refresh} />
      <AccountState
        loading={analysesQuery.loading}
        error={analysesQuery.error !== query.error ? analysesQuery.error : null}
        retry={refresh}
      />
      {!denied && !selectedId && !selectedAnalysis ? (
        <>
          <div className="flex flex-wrap items-center gap-4">
            <Search
              id="search-reports"
              label="Search reports"
              variant="toolbar"
              initValue={search}
              onChange={setSearch}
            />
            <Select
              id="report-type-filter"
              label="Report type"
              variant="filter"
              value={kind}
              options={[
                { value: "", label: "All report types" },
                ...["monthly", "quarterly", "annual", "ad_hoc"].map((value) => ({ value, label: reportTitle(value) })),
                ...Object.entries(analysisTypes).map(([value, label]) => ({ value, label })),
              ]}
              onChange={setKind}
            />
          </div>
          <List<(typeof rows)[number], string, "name" | "type" | "period" | "published" | "quality">
            items={filtered}
            itemKey="id"
            itemSelectable={false}
            stickyFirstColumn={false}
            activeCols={["name", "type", "period", "published", "quality"]}
            colTitles={{
              name: "Report",
              type: "Report type",
              period: "Reporting period",
              published: "Available since",
              quality: "Data quality",
            }}
            onRowClick={(report) => select(report.id, report.analysis)}
            colConfig={{
              name: {
                width: "min-w-48",
                allowWrap: true,
                component: (report) => report.name,
              },
              type: {
                width: "min-w-48",
                allowWrap: true,
                component: (report) => report.type,
              },
              period: {
                width: "min-w-48",
                allowWrap: true,
                component: (report) => report.period,
              },
              published: {
                width: "min-w-32",
                component: (report) => report.date,
              },
              quality: {
                width: "min-w-40",
                allowWrap: true,
                component: (report) => report.quality,
              },
            }}
            noDataElement={<p>{rows.length || search || kind ? "No reports match your search." : "No reports yet."}</p>}
          />
          {analysesQuery.data?.nextPageToken || query.data?.nextPageToken ? (
            <Button
              variant="secondary"
              text="Load more reports"
              disabled={analysesQuery.loading || analysesQuery.refreshing || query.loading || query.refreshing}
              onClick={() => setPageTarget({ search, kind, count: pageCount + 1 })}
            />
          ) : null}
        </>
      ) : null}
      {!denied && query.data && selectedId ? (
        <AccountReportDetail key={selectedId} reportId={selectedId} onSelect={select} />
      ) : null}
      {!denied && selectedAnalysis && (analysisListed || !libraryComplete) ? (
        <AccountAnalysisDetail
          key={selectedAnalysis}
          analysisId={selectedAnalysis}
          onDeleted={(analysisId) => {
            setDeletedAnalyses((previous) => new Set([...previous, analysisId]));
            select("");
            analysesQuery.refresh();
          }}
        />
      ) : null}
      {!denied && selectedAnalysis && !analysesQuery.error && libraryComplete && !analysisListed ? (
        analysesQuery.refreshing ? (
          <AccountState loading retry={refresh} />
        ) : (
          <p role="alert">This report is no longer available in your account library.</p>
        )
      ) : null}
      {generating && canGenerateAnalyses(account, scope.readOnly) ? (
        <GenerateAnalysis
          onDismiss={() => setGenerating(false)}
          onRefresh={refresh}
          onCreated={(id) => {
            refresh();
            select(id, true);
          }}
        />
      ) : null}
    </div>
  );
};
const AccountReports = () => {
  const scope = useRequiredAccountScope();
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  return (
    <AccountScopeContext.Provider value={{ ...scope, invalidateAccount: scope.refreshAccount }}>
      <AccountReportsList key={`${scope.account.accountId}:${generation}`} />
    </AccountScopeContext.Provider>
  );
};
export default AccountReports;
