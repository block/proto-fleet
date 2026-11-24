import { useCallback, useMemo, useState } from "react";

import { entriesInWindow, logCoverageNotice } from "./coverage";
import { controllerLogsToCsv } from "./csv";
import { ControllerSelect, RefreshControl } from "./ReadControls";
import ReadNotices from "./ReadNotices";
import {
  CONTROLLER_LOG_LIMIT,
  type ControllerLogEntry,
  type ControllerLogSeverity,
  type ControllerLogSnapshot,
  type ControllerRecordsSource,
} from "./types";
import { useKnownControllers } from "./useKnownControllers";
import { usePolledRead } from "./usePolledRead";
import Logs, { type LogsData } from "@/shared/components/Logs";
import Select from "@/shared/components/Select";
import { padLeft } from "@/shared/utils/stringUtils";

const WINDOWS = [
  { value: "", label: "All retained" },
  { value: "900000", label: "Last 15 minutes" },
  { value: "3600000", label: "Last hour" },
  { value: "21600000", label: "Last 6 hours" },
  { value: "86400000", label: "Last 24 hours" },
];

const SEVERITIES: { value: ControllerLogSeverity; label: string }[] = [
  { value: "debug", label: "Debug and above" },
  { value: "info", label: "Info and above" },
  { value: "warn", label: "Warnings and errors" },
  { value: "error", label: "Errors only" },
];

const formatLogTime = (iso: string | undefined) => {
  const date = iso ? new Date(iso) : undefined;
  if (!date || Number.isNaN(date.getTime())) return null;
  return (
    `${date.getFullYear()}-${padLeft(date.getMonth() + 1, 2)}-${padLeft(date.getDate(), 2)} ` +
    `${padLeft(date.getHours(), 2)}:${padLeft(date.getMinutes(), 2)}:${padLeft(date.getSeconds(), 2)}`
  );
};

const toLogsData = (entries: ControllerLogEntry[]): LogsData => ({
  kind: "structured",
  snapshot: true,
  entries: entries.map((entry, index) => ({
    id: BigInt(index),
    timestamp: formatLogTime(entry.timestamp),
    level: entry.severity,
    message:
      (entry.component ? `${entry.component} ${entry.message}` : entry.message) +
      (entry.truncated ? " [truncated]" : ""),
  })),
  lines: entries.length,
});

// A silent stream may mean nothing matches or a slow controller, so a refresh
// keeps the logs already shown rather than replacing them with nothing.
const silentRead = (snapshot: ControllerLogSnapshot) =>
  snapshot.coverage.state === "no-response" ? "The controller didn't send any log entries." : undefined;

type ControllerLogViewProps = Pick<ControllerRecordsSource, "readLogs" | "pollIntervalMs">;

const ControllerLogView = ({ readLogs, pollIntervalMs }: ControllerLogViewProps) => {
  const [controllerId, setControllerId] = useState("");
  const [minSeverity, setMinSeverity] = useState<ControllerLogSeverity>("info");
  const [component, setComponent] = useState("");
  const [windowMs, setWindowMs] = useState("");
  // The controller has no component list, so offer every component seen so far.
  const [knownComponents, setKnownComponents] = useState<string[]>([]);

  const selection = useMemo(() => ({ controllerId, minSeverity, component }), [controllerId, minSeverity, component]);
  const read = useMemo(
    () =>
      readLogs &&
      ((query: typeof selection, signal: AbortSignal) =>
        readLogs(
          {
            minSeverity: query.minSeverity,
            component: query.component || undefined,
            controllerId: query.controllerId || undefined,
            limit: CONTROLLER_LOG_LIMIT,
          },
          signal,
        )),
    [readLogs],
  );
  const {
    data: snapshot,
    error,
    reading,
    refresh,
  } = usePolledRead(selection, read, { inconclusive: silentRead, pollIntervalMs });
  const controllerIds = useKnownControllers(snapshot?.controllerIds);

  const seen = snapshot?.entries.map((entry) => entry.component).filter(Boolean) ?? [];
  if (seen.some((name) => !knownComponents.includes(name))) {
    setKnownComponents([...new Set([...knownComponents, ...seen])].sort());
  }

  const windowStartMs = snapshot && windowMs ? Date.parse(snapshot.fetchedAt) - Number(windowMs) : undefined;
  const visible = useMemo(
    () => (snapshot ? entriesInWindow(snapshot.entries, windowStartMs) : undefined),
    [snapshot, windowStartMs],
  );
  const logsData = useMemo(() => (visible ? toLogsData(visible) : undefined), [visible]);
  const exportCsv = useCallback(async () => (visible ? controllerLogsToCsv(visible) : undefined), [visible]);
  const coverage = snapshot ? logCoverageNotice(snapshot, windowStartMs) : undefined;
  // The CSV has no room for the callouts, so a partial or stale export says so in its name.
  const downloadFilename = [
    "controller-logs",
    snapshot?.controllerId,
    snapshot?.coverage.state === "interrupted" ? "interrupted" : coverage ? "partial" : undefined,
    error ? "stale" : undefined,
  ]
    .filter(Boolean)
    .join("-");

  const notices = (
    <ReadNotices
      staleTitle="Couldn't refresh controller logs"
      unavailableTitle="Controller logs unavailable"
      fetchedAt={snapshot?.fetchedAt}
      error={error}
      coverage={coverage}
      testIdPrefix="controller-logs"
    />
  );
  // The log pane opens scrolled to the newest rows, so failures stay pinned above it.
  const urgent = Boolean(error) || coverage?.intent === "warning";
  const logsShown = !error || Boolean(snapshot);

  return (
    <>
      <div className="flex flex-wrap items-center gap-3 px-4 pb-3">
        <ControllerSelect
          id="controller-log-controller"
          controllerIds={controllerIds}
          value={controllerId}
          onChange={setControllerId}
        />
        <Select
          id="controller-log-window"
          label="Time range"
          variant="filter"
          value={windowMs}
          options={WINDOWS}
          onChange={setWindowMs}
        />
        <Select
          id="controller-log-severity"
          label="Severity"
          variant="filter"
          value={minSeverity}
          options={SEVERITIES}
          onChange={(value) => setMinSeverity(value as ControllerLogSeverity)}
        />
        <Select
          id="controller-log-component"
          label="Component"
          variant="filter"
          value={component}
          options={[
            { value: "", label: "All components" },
            ...knownComponents.map((name) => ({ value: name, label: name })),
          ]}
          onChange={setComponent}
        />
        {pollIntervalMs === undefined ? (
          <div className="ml-auto">
            <RefreshControl
              fetchedAt={snapshot?.fetchedAt}
              controllerId={snapshot?.controllerId}
              reading={reading}
              onRefresh={refresh}
              testId="controller-logs-refresh"
            />
          </div>
        ) : null}
      </div>
      {logsShown && urgent ? null : notices}
      {logsShown ? (
        <Logs
          key={`${controllerId}:${minSeverity}:${component}`}
          logsData={logsData}
          exportCsv={exportCsv}
          downloadFilename={downloadFilename}
          notices={urgent ? notices : undefined}
        />
      ) : null}
    </>
  );
};

export default ControllerLogView;
