import { type ReactNode, useId, useMemo, useState } from "react";

import { reportCause, reportLabel, reportMetricLabel, reportTime } from "../reportFormat";
import { ReportingRows } from "./ReportingRows";
import type {
  AccountReport,
  ReportingInterruption,
  ReportingMetric,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { Slider } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import ManageColumnsModal, { type ColumnPreference } from "@/shared/components/ManageColumnsModal";
import Modal from "@/shared/components/Modal";
import Search from "@/shared/components/Search";
import Select from "@/shared/components/Select";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { formatHashrate } from "@/shared/utils/telemetryFormat";

const formatValue = (metric: ReportingMetric) => {
  if (metric.status === "unavailable") return "—";
  if (metric.unit === "H/s") {
    const value = Number(metric.value);
    if (!metric.value.trim() || !Number.isFinite(value)) return "—";
    const formatted = formatHashrate(Math.abs(value) / 1e12) ?? "—";
    return value < 0 ? `-${formatted}` : formatted;
  }
  if (metric.amounts.length) return metric.amounts.map((amount) => `${amount.value} ${amount.currency}`).join("; ");
  return metric.value !== "" ? `${metric.value} ${metric.unit}`.trim() : "—";
};

type MetricRow = { key: string; label: string; metrics: Record<string, ReportingMetric> };
type RecordRow = Record<string, string>;
type RecordDetails = {
  title: string;
  fields: { label: string; value: string; wide?: boolean }[];
};

const Records = ({
  rows,
  columns,
  empty,
  details,
}: {
  rows: RecordRow[];
  columns: Record<string, string>;
  empty: string;
  details?: (row: RecordRow) => RecordDetails;
}) => {
  const [selectedKey, setSelectedKey] = useState<string>();
  const selected = selectedKey === undefined ? undefined : rows.find((row) => row.key === selectedKey);
  const selectedDetails = selected ? details?.(selected) : undefined;
  return (
    <>
      <ReportingRows rows={rows}>
        {(pageRows) => (
          <List<RecordRow, string, string>
            items={pageRows}
            itemKey="key"
            itemSelectable={false}
            activeCols={Object.keys(columns)}
            colTitles={columns}
            stickyFirstColumn
            applyColumnWidthsToCells
            tableClassName="!table-auto"
            onRowClick={details ? (row) => setSelectedKey(row.key) : undefined}
            colConfig={Object.fromEntries(
              Object.keys(columns).map((key, index) => [
                key,
                {
                  width: details
                    ? index === 0
                      ? "w-72 min-w-56"
                      : "min-w-40"
                    : ["notes", "sources", "cause"].includes(key)
                      ? "min-w-64"
                      : "min-w-40",
                  allowWrap: true,
                  component: (row: RecordRow) => (
                    <span className={details ? "line-clamp-2 max-w-72 break-words" : "whitespace-pre-line"}>
                      {row[key] || "—"}
                    </span>
                  ),
                },
              ]),
            )}
            noDataElement={<p className="text-text-primary-70">{empty}</p>}
          />
        )}
      </ReportingRows>
      {selectedDetails ? (
        <Modal
          open
          title={selectedDetails.title}
          dialogLabel={`${selectedDetails.title} details`}
          manageFocus
          forceTitleCollapsed
          onDismiss={() => setSelectedKey(undefined)}
        >
          <dl className="grid grid-cols-2 gap-4 phone:grid-cols-1">
            {selectedDetails.fields.map(({ label, value, wide }) => (
              <div key={label} className={wide ? "col-span-2 phone:col-span-1" : undefined}>
                <dt className="text-text-primary-70">{label}</dt>
                <dd className="mt-1 break-words whitespace-pre-wrap text-text-primary">{value || "—"}</dd>
              </div>
            ))}
          </dl>
        </Modal>
      ) : null}
    </>
  );
};

const Events = ({ interruptions, timezone }: { interruptions: ReportingInterruption[]; timezone: string }) => {
  const time = (value: Parameters<typeof reportTime>[0]) => (value ? reportTime(value, timezone) : "—");
  return (
    <Records
      rows={interruptions.map((entry) => ({
        key: entry.id,
        event: entry.summary || reportLabel(entry.category),
        category: entry.category ? reportLabel(entry.category) : "",
        container: entry.siteId && entry.containerId ? `Site ${entry.siteId} · Container ${entry.containerId}` : "",
        order: entry.orderId,
        start: time(entry.start),
        end: entry.open
          ? entry.category === "curtailment_control"
            ? "Restoration unconfirmed"
            : "Ongoing"
          : time(entry.end),
        observedThrough: entry.open ? time(entry.observedThrough) : "",
        cause: entry.rootCause,
        exclusion: entry.claimedExclusion || "None claimed",
        exclusionStatus: entry.exclusionStatus ? reportLabel(entry.exclusionStatus) : "",
        source: entry.sourceRef,
      }))}
      columns={{ event: "Event", container: "Container", order: "Order ref", start: "Start", end: "End" }}
      details={(row) => ({
        title: "Event",
        fields: [
          { label: "Event", value: row.event, wide: true },
          { label: "Category", value: row.category },
          { label: "Container", value: row.container },
          { label: "Order ref", value: row.order, wide: true },
          { label: "Start", value: row.start },
          { label: "End", value: row.end },
          ...(row.observedThrough
            ? [
                {
                  label: row.category === "Curtailment control" ? "Reporting cutoff" : "Observed through",
                  value: row.observedThrough,
                },
              ]
            : []),
          {
            label: row.category === "Curtailment control" ? "Evidence basis" : "Root cause",
            value: row.cause,
            wide: true,
          },
          { label: "Exclusion", value: row.exclusion },
          { label: "Exclusion status", value: row.exclusionStatus },
          { label: "Source", value: row.source, wide: true },
        ],
      })}
      empty="No events are recorded for this report."
    />
  );
};

const Results = ({
  rows,
  titles,
  defaults,
  columns,
  onColumnsChange,
  rowTitle,
  controls,
  onEvidence,
}: {
  rows: MetricRow[];
  titles: Record<string, string>;
  defaults: string[];
  columns: ColumnPreference[];
  onColumnsChange: (columns: ColumnPreference[]) => void;
  rowTitle: string;
  controls: ReactNode;
  onEvidence: (row: MetricRow, metric: string) => void;
}) => {
  const [managing, setManaging] = useState(false);
  const defaultColumns = Object.keys(titles).map((id) => ({ id, visible: defaults.includes(id) }));
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">{controls}</div>
        <Button
          variant="secondary"
          size="compact"
          text="Columns"
          prefixIcon={<Slider width="w-4" />}
          ariaLabel="Manage result columns"
          ariaHasPopup="dialog"
          onClick={() => setManaging(true)}
        />
      </div>
      <ReportingRows rows={rows}>
        {(pageRows) => (
          <List<MetricRow, string, string>
            items={pageRows}
            itemKey="key"
            itemSelectable={false}
            activeCols={["scope", ...columns.filter((column) => column.visible).map((column) => column.id)]}
            colTitles={{ scope: rowTitle, ...titles }}
            stickyFirstColumn
            applyColumnWidthsToCells
            tableClassName="!table-auto"
            colConfig={{
              scope: { width: "min-w-48", allowWrap: true, component: (row) => row.label },
              ...Object.fromEntries(
                Object.keys(titles).map((key) => [
                  key,
                  {
                    width: "min-w-40",
                    allowWrap: true,
                    component: (row: MetricRow) => {
                      const metric = row.metrics[key];
                      return metric ? (
                        <button
                          type="button"
                          aria-label={`Evidence for ${titles[key]} · ${row.label}`}
                          className="flex cursor-pointer flex-col items-start gap-1 text-left tabular-nums outline-none hover:underline focus-visible:underline"
                          onClick={() => onEvidence(row, key)}
                        >
                          <span>{formatValue(metric)}</span>
                          <span className="text-text-primary-70">{reportLabel(metric.status)}</span>
                        </button>
                      ) : (
                        "—"
                      );
                    },
                  },
                ]),
              ),
            }}
            noDataElement={<p className="text-text-primary-70">No results match your search.</p>}
          />
        )}
      </ReportingRows>
      {managing ? (
        <ManageColumnsModal
          columns={columns}
          defaultColumns={defaultColumns}
          titles={titles}
          onDismiss={() => setManaging(false)}
          onSave={(next) => {
            onColumnsChange(next);
            setManaging(false);
          }}
        />
      ) : null}
    </>
  );
};

const ReportWorkbook = ({ report }: { report: AccountReport }) => {
  const id = useId();
  const result = report.result!;
  const timezone = report.period?.timezone || "UTC";
  const time = (value: Parameters<typeof reportTime>[0]) => (value ? reportTime(value, timezone) : "—");
  const [sheet, setSheet] = useState("results");
  const [grouping, setGrouping] = useState(result.containers.length ? "containers" : "account");
  const [search, setSearch] = useState("");
  const [evidenceFilter, setEvidenceFilter] = useState<{ row: string; metric: string }>();
  const scope = grouping === "containers" ? "Container" : grouping === "orders" ? "Order ref" : "Account";
  const allRows: MetricRow[] =
    grouping === "account"
      ? [{ key: "account", label: report.accountName || "Account total", metrics: result.metrics }]
      : grouping === "containers"
        ? result.containers.map((entry) => ({
            key: `${entry.siteId}:${entry.containerId}`,
            label: `Site ${entry.siteId} · Container ${entry.containerId}`,
            metrics: entry.metrics,
          }))
        : result.orders.map((entry) => ({ key: entry.orderId, label: entry.orderId, metrics: entry.metrics }));
  const rows = allRows.filter((row) => row.label.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const keys = [
    ...new Set(
      [
        result.metrics,
        ...result.containers.map((entry) => entry.metrics),
        ...result.orders.map((entry) => entry.metrics),
      ].flatMap(Object.keys),
    ),
  ];
  const uptime =
    report.period?.kind === "annual"
      ? "annual_uptime"
      : report.period?.kind === "quarterly"
        ? "rolling_three_month_uptime"
        : report.period?.kind === "monthly"
          ? "monthly_uptime"
          : "";
  const preferred = ["hashrate_performance_hs", "metered_energy_kwh", uptime, "accrued_hosting_fees"].filter((key) =>
    keys.includes(key),
  );
  const orderedKeys = [...preferred, ...keys.filter((key) => !preferred.includes(key)).sort()];
  const titles = Object.fromEntries(orderedKeys.map((key) => [key, reportMetricLabel(key)]));
  const defaults = preferred.length ? preferred : orderedKeys.slice(0, 4);
  const [columnPreferences, setColumnPreferences] = useState<ColumnPreference[]>([]);
  const columnSchema = JSON.stringify(orderedKeys.map((id) => ({ id, visible: defaults.includes(id) })));
  const columns = useMemo(() => {
    const available: ColumnPreference[] = JSON.parse(columnSchema);
    return [
      ...columnPreferences.filter((column) => available.some(({ id }) => id === column.id)),
      ...available.filter(({ id }) => !columnPreferences.some((column) => column.id === id)),
    ];
  }, [columnPreferences, columnSchema]);
  const evidence =
    sheet === "evidence"
      ? allRows.flatMap((row) =>
          Object.entries(row.metrics)
            .filter(([key]) => !evidenceFilter || (row.key === evidenceFilter.row && key === evidenceFilter.metric))
            .map(([key, metric]) => ({
              key: `${row.key}:${key}`,
              scope: row.label,
              measure: reportMetricLabel(key),
              value: formatValue(metric),
              quality: reportLabel(metric.status),
              notes: metric.causes.map(reportCause).join("\n"),
              method: metric.method ? reportLabel(metric.method) : "",
              sources: metric.sources.join("\n"),
            })),
        )
      : [];
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <TabStrip
        activeId={sheet}
        onSelect={(next) => {
          setSheet(next);
          setEvidenceFilter(undefined);
        }}
        ariaLabel="Report sheets"
        className="overflow-x-auto"
      >
        {Object.entries({
          results: "Results",
          events: "Events",
          repairs: "Repairs",
          evidence: "Evidence",
          reconciliation: "Reconciliation",
        }).map(([key, label]) => (
          <TabStripItem key={key} id={key} label={label} />
        ))}
      </TabStrip>
      <section aria-label={`Report ${sheet}`} className="flex min-w-0 flex-col gap-4">
        {sheet === "results" ? (
          <Results
            key={grouping}
            rows={rows}
            titles={titles}
            defaults={defaults}
            columns={columns}
            onColumnsChange={setColumnPreferences}
            rowTitle={scope}
            onEvidence={(row, metric) => {
              setEvidenceFilter({ row: row.key, metric });
              setSheet("evidence");
            }}
            controls={
              <>
                <Select
                  id={`${id}-grouping`}
                  label="Row grouping"
                  variant="filter"
                  value={grouping}
                  options={[
                    { value: "account", label: "Account total" },
                    ...(result.containers.length ? [{ value: "containers", label: "Containers" }] : []),
                    ...(result.orders.length ? [{ value: "orders", label: "Orders" }] : []),
                  ]}
                  onChange={(next) => {
                    setGrouping(next);
                    setSearch("");
                  }}
                />
                <Search
                  key={grouping}
                  id={`${id}-search`}
                  label={
                    grouping === "orders"
                      ? "Search hosting orders"
                      : grouping === "containers"
                        ? "Search containers"
                        : "Search account"
                  }
                  variant="toolbar"
                  initValue={search}
                  onChange={setSearch}
                />
              </>
            }
          />
        ) : null}
        {sheet === "events" ? <Events interruptions={result.interruptions} timezone={timezone} /> : null}
        {sheet === "repairs" ? (
          <Records
            rows={result.obligations.map((entry, index) => ({
              key: `${entry.id}:${index}`,
              ticket: entry.ticketRef || "Recorded repair",
              severity: entry.severity,
              qualification: entry.qualified ? "Qualified" : "Unqualified",
              episode: time(entry.episodeStart),
              submitted: time(entry.submittedAt),
              received: time(entry.receivedAt),
              responded: time(entry.respondedAt),
              approved: time(entry.approvedAt),
              deadline: time(entry.deadline),
              restored: time(entry.restoredAt),
              closed: time(entry.closedAt),
              sources: entry.sourceRef,
            }))}
            columns={{
              ticket: "Repair",
              severity: "Severity",
              qualification: "Qualification",
              episode: "Episode start",
              submitted: "Submitted",
              received: "Received",
              responded: "Responded",
              approved: "Approved",
              deadline: "Restoration deadline",
              restored: "Restored",
              closed: "Closed",
              sources: "Source",
            }}
            empty="No repairs are recorded for this report."
          />
        ) : null}
        {sheet === "evidence" ? (
          <>
            {evidenceFilter ? (
              <div className="flex flex-wrap items-center justify-between gap-3">
                <span>{titles[evidenceFilter.metric]}</span>
                <Button
                  variant="secondary"
                  size="compact"
                  text="Clear evidence filter"
                  onClick={() => setEvidenceFilter(undefined)}
                />
              </div>
            ) : null}
            <Records
              key={evidenceFilter ? `${evidenceFilter.row}:${evidenceFilter.metric}` : "all"}
              rows={evidence}
              columns={{
                scope,
                measure: "Measure",
                value: "Value",
                quality: "Quality",
              }}
              details={(row) => ({
                title: "Evidence",
                fields: [
                  { label: scope, value: row.scope, wide: true },
                  { label: "Measure", value: row.measure },
                  { label: "Value", value: row.value },
                  { label: "Quality", value: row.quality },
                  { label: "Qualifications", value: row.notes, wide: true },
                  { label: "Method", value: row.method, wide: true },
                  { label: "Sources", value: row.sources, wide: true },
                ],
              })}
              empty="No evidence is included for this selection."
            />
          </>
        ) : null}
        {sheet === "reconciliation" ? (
          <Records
            rows={result.reconciliations.map((entry, index) => ({
              key: String(index),
              measure: reportMetricLabel(entry.metric),
              view: reportLabel(entry.view),
              total: entry.exactTotal,
              parts: entry.exactParts,
              quality: reportLabel(entry.status),
              match: entry.matches ? "Matches" : entry.exactTotal && entry.exactParts ? "Does not match" : "—",
            }))}
            columns={{
              measure: "Measure",
              view: "Breakdown",
              total: "Exact total",
              parts: "Exact attributed total",
              quality: "Quality",
              match: "Reconciliation",
            }}
            empty="No reconciliation is included for this report."
          />
        ) : null}
      </section>
      <p className="text-text-primary-70">Evidence as of {time(report.period?.asOf)}</p>
    </div>
  );
};

export default ReportWorkbook;
