import { useCallback } from "react";

import { type LogAttr, type LogEntry } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import LogLevelBadge from "@/protoFleet/features/serverLogs/components/LogLevelBadge";
import { formatTimestampFull } from "@/protoFleet/features/serverLogs/utils/format";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

interface LogDetailModalProps {
  entry: LogEntry | null;
  onDismiss: () => void;
}

/**
 * LogDetailModal renders the full record with time, level, source,
 * message, and a key/value table of attributes. It also offers a
 * "Copy as JSON" button so operators can paste the entry into a ticket.
 */
const LogDetailModal = ({ entry, onDismiss }: LogDetailModalProps) => {
  const copyAsJson = useCallback(() => {
    if (!entry) return;
    const json = entryToJson(entry);
    void navigator.clipboard?.writeText(json);
  }, [entry]);

  return (
    <Modal
      open={entry !== null}
      onDismiss={onDismiss}
      title="Log entry"
      size="standard"
      buttons={[
        {
          variant: variants.secondary,
          text: "Copy as JSON",
          onClick: copyAsJson,
        },
      ]}
    >
      {entry ? (
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-3">
            <LogLevelBadge level={entry.level} />
            <span className="font-mono text-[13px] text-text-primary-70">
              {entry.time ? formatTimestampFull(entry.time) : "—"}
            </span>
            {entry.source ? <span className="font-mono text-[12px] text-text-primary-50">{entry.source}</span> : null}
          </div>
          <div className="rounded-lg border border-border-5 bg-surface-base p-3">
            <div className="font-mono text-[13px] break-all whitespace-pre-wrap text-text-primary">{entry.message}</div>
          </div>
          {entry.attrs.length > 0 ? (
            <div className="flex flex-col gap-1">
              <div className="text-200 text-text-primary-50">Attributes</div>
              <div className="overflow-hidden rounded-lg border border-border-5">
                <table className="w-full font-mono text-[12px]">
                  <tbody>
                    {entry.attrs.map((a: LogAttr, i: number) => (
                      <tr key={`${a.key}-${i}`} className="border-b border-border-5 last:border-b-0">
                        <td className="w-[35%] bg-surface-base px-3 py-1.5 align-top break-all text-text-primary-70">
                          {a.key}
                        </td>
                        <td className="px-3 py-1.5 align-top break-all whitespace-pre-wrap text-text-primary">
                          {a.value}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </Modal>
  );
};

/**
 * entryToJson renders the entry into pretty-printed JSON suitable for
 * pasting into tickets. We do this by hand rather than JSON.stringify
 * because protobuf-es messages contain a non-enumerable $typeName, and
 * bigint id values aren't JSON-serializable directly.
 */
function entryToJson(entry: LogEntry): string {
  const obj: Record<string, unknown> = {
    id: entry.id.toString(),
    time: entry.time ? formatTimestampFull(entry.time) : null,
    level: entry.level,
    message: entry.message,
    source: entry.source || undefined,
    attrs: entry.attrs.length > 0 ? Object.fromEntries(entry.attrs.map((a: LogAttr) => [a.key, a.value])) : undefined,
  };
  // Strip undefined keys for tidier output.
  for (const k of Object.keys(obj)) if (obj[k] === undefined) delete obj[k];
  return JSON.stringify(obj, null, 2);
}

export default LogDetailModal;
