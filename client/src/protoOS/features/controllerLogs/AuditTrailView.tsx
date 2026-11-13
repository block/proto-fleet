import { useCallback, useMemo, useState } from "react";

import AuditEventDetailModal from "./AuditEventDetailModal";
import { auditResultLabel, auditResultStatus } from "./auditResult";
import { auditCoverageNotice, formatControllerTime } from "./coverage";
import { auditEventsToCsv } from "./csv";
import ReadNotices from "./ReadNotices";
import { usePolledRead } from "./usePolledRead";
import {
  AUDIT_EVENT_LIMIT,
  AUDIT_WINDOW_MAX_MS,
  type AuditEvent,
  type AuditResult,
} from "@/protoOS/api/adapters/controllerLogs";
import type { AdapterRequestOptions, MinerAdapter } from "@/protoOS/api/adapters/types";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { downloadLogs } from "@/shared/components/Logs/utility";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { StatusLabel } from "@/shared/components/StatusCircle";
import { getFileName } from "@/shared/utils/utility";

const HOUR_MS = 60 * 60 * 1000;
const RANGES = [
  { value: String(HOUR_MS), label: "Last hour" },
  { value: String(24 * HOUR_MS), label: "Last 24 hours" },
  { value: String(AUDIT_WINDOW_MAX_MS), label: "Last 7 days" },
];
const RESULTS = [
  { value: "", label: "All results" },
  { value: "ok", label: "Succeeded" },
  { value: "error", label: "Failed" },
];

type AuditColumn = "time" | "actor" | "action" | "target" | "result";

const colTitles: ColTitles<AuditColumn> = {
  time: "Time",
  actor: "Actor",
  action: "Action",
  target: "Target",
  result: "Result",
};

// Source, controller, error and details are in the row's detail modal.
const colConfig: ColConfig<AuditEvent, string, AuditColumn> = {
  time: { width: "w-[190px]", component: (event) => formatControllerTime(event.time) || "—" },
  actor: { width: "w-[160px]", component: (event) => event.actor || "—" },
  action: {
    width: "w-[220px]",
    component: (event) => <span className="font-mono text-mono-text-50">{event.action || "—"}</span>,
  },
  target: { width: "w-[160px]", component: (event) => event.target || "—" },
  result: {
    width: "w-[140px]",
    component: (event) => (
      <StatusLabel status={auditResultStatus(event.result)}>{auditResultLabel(event.result)}</StatusLabel>
    ),
  },
};
const activeCols = Object.keys(colTitles) as AuditColumn[];

const AuditTrailView = ({ adapter }: { adapter: MinerAdapter }) => {
  const [rangeMs, setRangeMs] = useState(String(24 * HOUR_MS));
  const [result, setResult] = useState("");
  const selection = useMemo(() => ({ rangeMs: Number(rangeMs), result }), [rangeMs, result]);
  const read = useMemo(
    () =>
      adapter.listAuditEvents &&
      ((query: typeof selection, options: AdapterRequestOptions) => {
        const end = Date.now();
        return adapter.listAuditEvents!(
          {
            start: new Date(end - query.rangeMs).toISOString(),
            end: new Date(end).toISOString(),
            limit: AUDIT_EVENT_LIMIT,
            result: (query.result || undefined) as AuditResult | undefined,
          },
          options,
        );
      }),
    [adapter],
  );
  const { data: page, error, loading } = usePolledRead(selection, read);
  const coverage = page ? auditCoverageNotice(page) : undefined;
  const [selected, setSelected] = useState<AuditEvent | null>(null);
  const dismiss = useCallback(() => setSelected(null), []);
  // A refresh or filter that drops the open event closes its detail modal.
  if (selected && !page?.events.some((event) => event.id === selected.id)) setSelected(null);

  return (
    <>
      <div className="flex flex-wrap items-center gap-3 px-4 pb-3">
        <Select
          id="audit-range"
          label="Time range"
          variant="filter"
          value={rangeMs}
          options={RANGES}
          onChange={setRangeMs}
        />
        <Select
          id="audit-result"
          label="Result"
          variant="filter"
          value={result}
          options={RESULTS}
          onChange={setResult}
        />
        <div className="ml-auto">
          <Button
            size={sizes.compact}
            variant={variants.secondary}
            text="Export"
            disabled={!page?.events.length}
            onClick={() =>
              page &&
              downloadLogs(
                auditEventsToCsv(page.events),
                getFileName(
                  ["controller-audit", page.limitReached && "partial", error && "stale"].filter(Boolean).join("-"),
                  "csv",
                ),
              )
            }
          />
        </div>
      </div>
      <ReadNotices
        staleTitle="Couldn't refresh the audit trail"
        unavailableTitle="Audit trail unavailable"
        fetchedAt={page?.fetchedAt}
        error={error}
        coverage={coverage}
        testIdPrefix="audit"
      />
      {loading ? (
        <div className="flex h-[189px] items-center justify-center">
          <ProgressCircular indeterminate />
        </div>
      ) : page ? (
        <List
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          items={page.events}
          itemKey="id"
          itemName={{ singular: "event", plural: "events" }}
          total={page.events.length}
          paddingLeft={{ phone: "16px", tablet: "16px", laptop: "16px", desktop: "16px" }}
          paddingRight={{ phone: "16px", tablet: "16px", laptop: "16px", desktop: "16px" }}
          onRowClick={setSelected}
          noDataElement={
            <div className="flex h-[189px] w-full items-center justify-center rounded-2xl bg-core-primary-5">
              <div className="text-heading-100 text-text-primary-50">No audit events in this time range</div>
            </div>
          }
        />
      ) : null}
      <AuditEventDetailModal event={selected} onDismiss={dismiss} />
    </>
  );
};

export default AuditTrailView;
