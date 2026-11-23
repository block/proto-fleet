import { useCallback, useEffect, useRef, useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { canGenerateAnalyses, canManageAnalyses } from "../capabilities";
import { analysisTypes, reportDateRange, reportTime } from "../reportFormat";
import { useAccountMutation } from "../useAccountMutation";
import { useAccountQuery } from "../useAccountQuery";
import { useReportDownload } from "../useReportDownload";
import { AccountState } from "./AccountState";
import { ReportingError } from "./ReportingError";
import { ReportingForm } from "./ReportingForm";
import { ReportingRows } from "./ReportingRows";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { createOperationID } from "@/protoFleet/utils/crypto";
import { Alert } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import Select from "@/shared/components/Select";

export const GenerateAnalysis = ({
  onDismiss,
  onCreated,
  onRefresh,
}: {
  onDismiss: () => void;
  onCreated: (id: string) => void;
  onRefresh: () => void;
}) => {
  const { account, client, signal, readOnly } = useRequiredAccountScope();
  const [kind, setKind] = useState("miner_performance");
  const [openedAt] = useState(() => Date.now());
  const [requestId] = useState(() => createOperationID());
  const [name, setName] = useState("Miner performance");
  const [startDate, setStart] = useState(() => new Date(Date.now() - 7 * 86400000).toISOString().slice(0, 10));
  const [endDate, setEnd] = useState(() => new Date(Date.now() - 86400000).toISOString().slice(0, 10));
  const start = new Date(`${startDate}T00:00:00Z`);
  const end = new Date(Math.min(new Date(`${endDate}T00:00:00Z`).getTime() + 86400000, openedAt));
  const duration = end.getTime() - start.getTime();
  const validRange =
    startDate &&
    endDate &&
    endDate >= startDate &&
    endDate <= new Date(openedAt).toISOString().slice(0, 10) &&
    duration > 0 &&
    duration <= 366 * 86400000;
  if (!canGenerateAnalyses(account, readOnly)) return null;
  return (
    <ReportingForm
      title="Generate report"
      confirm="Generate report"
      signal={signal}
      valid={Boolean(name.trim() && validRange)}
      onDismiss={onDismiss}
      onConflict={onRefresh}
      onSubmit={async (requestSignal) => {
        if (!canGenerateAnalyses(account, readOnly)) return;
        const { analysis } = await client.generateAccountAnalysis(
          {
            accountId: account.accountId,
            name: name.trim(),
            requestId,
            kind,
            start: timestampFromDate(start),
            end: timestampFromDate(end),
            timezone: "UTC",
          },
          { signal: requestSignal, timeoutMs: 60_000 },
        );
        if (!requestSignal.aborted && analysis) onCreated(analysis.analysisId);
      }}
    >
      <Input id="analysis-name" label="Report name" initValue={name} onChange={setName} maxLength={200} />
      <Select
        id="analysis-kind"
        label="Report type"
        value={kind}
        options={Object.entries(analysisTypes)
          .filter(([value]) => value !== "issues_repairs" || account.ticketAccess)
          .map(([value, label]) => ({ value, label }))}
        onChange={(value) => {
          if (name === analysisTypes[kind]) setName(analysisTypes[value]);
          setKind(value);
        }}
      />
      <div className="grid grid-cols-2 gap-4 phone:grid-cols-1">
        <Input id="analysis-start" type="date" label="Start date (UTC)" initValue={startDate} onChange={setStart} />
        <Input id="analysis-end" type="date" label="End date (UTC)" initValue={endDate} onChange={setEnd} />
      </div>
      <p className="text-200 text-text-primary-70">
        Includes the account’s authorized allocation history. Dates include the full day in UTC; today includes
        observations captured so far. Saved reports are shared with authorized account members.
      </p>
      {!validRange ? <p role="alert">Choose a period of up to 366 days ending today or earlier.</p> : null}
      {kind === "energy_usage" ? (
        <p className="text-200 text-text-primary-70">
          Uses recorded meter records. Missing or partial intervals remain unavailable.
        </p>
      ) : null}
    </ReportingForm>
  );
};

export const AccountAnalysisDetail = ({
  analysisId,
  onDeleted,
}: {
  analysisId: string;
  onDeleted: (id: string) => void;
}) => {
  const scope = useRequiredAccountScope();
  const [denied, setDenied] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [deleteDenied, setDeleteDenied] = useState(false);
  const deletionLifetime = useRef<AbortController | null>(null);
  const mutation = useAccountMutation();
  const canManage = canManageAnalyses(scope.account, scope.readOnly);
  useEffect(() => {
    const controller = new AbortController();
    deletionLifetime.current = controller;
    return () => controller.abort();
  }, [canManage]);
  useEffect(() => {
    const clear = () => setDenied(true);
    scope.signal.addEventListener("abort", clear);
    return () => scope.signal.removeEventListener("abort", clear);
  }, [scope.signal]);
  const load = useCallback(
    (signal: AbortSignal) =>
      scope.client.getAccountAnalysis(
        { accountId: scope.account.accountId, analysisId },
        { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [scope.client, scope.account.accountId, analysisId],
  );
  const query = useAccountQuery(load, !denied, true, POLL_INTERVAL_MS);
  const exporting = useReportDownload<"analysis.csv">(
    analysisId,
    scope.signal,
    (_, signal) => scope.client.exportAccountAnalysis({ accountId: scope.account.accountId, analysisId }, { signal }),
    () => {
      setDenied(true);
      query.invalidate();
      scope.invalidateAccount();
    },
  );
  const confirmDelete = async () => {
    if (!canManage || deleteDenied || !deletionLifetime.current) return;
    const signal = AbortSignal.any([scope.signal, deletionLifetime.current.signal]);
    const deleted = await mutation.run(async () => {
      try {
        await scope.client.deleteAccountAnalysis(
          { accountId: scope.account.accountId, analysisId },
          { signal, timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
        );
      } catch (cause) {
        if (!signal.aborted && [Code.PermissionDenied, Code.Unauthenticated].includes(ConnectError.from(cause).code)) {
          setDeleteDenied(true);
          query.invalidate();
        }
        throw cause;
      }
    });
    if (deleted && !signal.aborted) onDeleted(analysisId);
  };
  const analysis = !denied && !scope.signal.aborted ? query.data?.analysis : undefined;
  return (
    <section aria-label="Generated report" className="flex min-w-0 flex-col gap-6">
      <AccountState
        title={analysis ? "Could not refresh analysis" : undefined}
        loading={query.loading}
        error={denied ? "Report access is no longer available." : query.error}
        retry={query.refresh}
      />
      {analysis ? (
        <>
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 className="text-heading-200">{analysis.name}</h2>
              <p className="mt-2 text-text-primary-70">
                {reportDateRange(analysis)} · {scope.account.name}
              </p>
              <p className="mt-2 text-200 text-text-primary-70">
                {analysisTypes[analysis.kind]} · Ad hoc analysis · Generated{" "}
                {reportTime(analysis.createdAt, analysis.timezone)}
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
              <Button
                variant="secondary"
                text="Download CSV"
                disabled={exporting.busy}
                onClick={() => void exporting.download("analysis.csv")}
              />
              {canManage && !deleteDenied ? (
                <Button
                  variant={variants.secondaryDanger}
                  text="Delete report"
                  onClick={() => {
                    mutation.clearError();
                    setDeleting(analysis.name);
                  }}
                />
              ) : null}
            </div>
          </div>
          {exporting.error ? <ReportingError error={exporting.error} /> : null}
          {analysis.limitations.length ? (
            <div role="note" className="rounded-xl border border-border-20 p-4">
              <p className="text-emphasis-300">Data coverage and limitations</p>
              <ul className="mt-2 list-disc space-y-1 pl-5 text-200 text-text-primary-70">
                {analysis.limitations.map((text) => (
                  <li key={text}>{text}</li>
                ))}
              </ul>
            </div>
          ) : null}
          {analysis.tables.map((table) => (
            <section key={table.key} aria-label={table.title} className="flex min-w-0 flex-col gap-4">
              <h3 className="text-heading-100">{table.title}</h3>
              <ReportingRows rows={table.rows}>
                {(rows) => (
                  <List<{ key: string; cells: string[] }, string, string>
                    items={rows.map((row, index) => ({ key: String(index), cells: row.cells }))}
                    itemKey="key"
                    itemSelectable={false}
                    stickyFirstColumn={false}
                    activeCols={table.columns.map((_, index) => String(index))}
                    colTitles={Object.fromEntries(table.columns.map((title, index) => [String(index), title]))}
                    colConfig={Object.fromEntries(
                      table.columns.map((_, index) => [
                        String(index),
                        {
                          width: "min-w-36",
                          allowWrap: true,
                          component: (row: { cells: string[] }) => row.cells[index] || "—",
                        },
                      ]),
                    )}
                    noDataElement={<p>No supported records for this period.</p>}
                  />
                )}
              </ReportingRows>
            </section>
          ))}
        </>
      ) : null}
      {deleting && canManage && !denied && !scope.signal.aborted ? (
        <Dialog
          open
          manageFocus
          title="Delete saved report?"
          icon={
            <DialogIcon intent="critical">
              <Alert />
            </DialogIcon>
          }
          onDismiss={mutation.busy ? undefined : () => setDeleting(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, disabled: mutation.busy, onClick: () => setDeleting(null) },
            {
              text: "Permanently delete",
              variant: variants.danger,
              loading: mutation.busy,
              disabled: deleteDenied,
              onClick: () => void confirmDelete(),
            },
          ]}
        >
          <div className="flex flex-col gap-4">
            <p>
              Permanently delete “{deleting}” and its downloadable data from this account? This cannot be undone. The
              audit record is retained.
            </p>
            {mutation.error ? <ReportingError error={mutation.error} /> : null}
          </div>
        </Dialog>
      ) : null}
    </section>
  );
};
