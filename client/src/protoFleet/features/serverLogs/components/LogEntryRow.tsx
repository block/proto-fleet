import clsx from "clsx";

import { type LogEntry } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import LogLevelBadge from "@/protoFleet/features/serverLogs/components/LogLevelBadge";
import { formatTimestamp, summarizeAttrs } from "@/protoFleet/features/serverLogs/utils/format";

interface LogEntryRowProps {
  entry: LogEntry;
  onClick: (entry: LogEntry) => void;
}

const LogEntryRow = ({ entry, onClick }: LogEntryRowProps) => {
  const time = entry.time ? formatTimestamp(entry.time) : "";
  const attrSummary = summarizeAttrs(entry.attrs);

  return (
    <button
      type="button"
      onClick={() => onClick(entry)}
      className={clsx(
        "flex w-full items-baseline gap-3 border-b border-border-5 px-4 py-1.5 text-left",
        "font-mono text-[12px] text-text-primary",
        "hover:bg-surface-elevated-base focus-visible:bg-surface-elevated-base",
        "focus:outline-none",
      )}
    >
      <span className="w-[160px] shrink-0 text-text-primary-50">{time}</span>
      <LogLevelBadge level={entry.level} />
      <span className="min-w-0 flex-1 truncate" title={entry.message}>
        {entry.message}
        {attrSummary ? <span className="ml-2 text-text-primary-50">{attrSummary}</span> : null}
      </span>
    </button>
  );
};

export default LogEntryRow;
