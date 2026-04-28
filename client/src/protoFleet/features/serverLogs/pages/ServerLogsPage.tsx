import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { type LogEntry, LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import { useServerLogs } from "@/protoFleet/api/useServerLogs";
import LogDetailModal from "@/protoFleet/features/serverLogs/components/LogDetailModal";
import LogEntryRow from "@/protoFleet/features/serverLogs/components/LogEntryRow";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Search from "@/shared/components/Search";
import Switch from "@/shared/components/Switch";
import { debounce } from "@/shared/utils/utility";

const LEVEL_OPTIONS: { value: LogLevel; label: string }[] = [
  { value: LogLevel.DEBUG, label: "Debug" },
  { value: LogLevel.INFO, label: "Info" },
  { value: LogLevel.WARN, label: "Warn" },
  { value: LogLevel.ERROR, label: "Error" },
];

// Distance from the bottom of the scroll container, in pixels, within
// which we consider the user to be "at the bottom" — and therefore okay
// to auto-scroll on new entries. A small fudge factor handles fractional
// scroll positions that browsers sometimes report.
const STICK_TO_BOTTOM_THRESHOLD = 32;

const ServerLogsPage = () => {
  const [minLevel, setMinLevel] = useState<LogLevel>(LogLevel.INFO);
  const [searchInput, setSearchInput] = useState("");
  const [searchText, setSearchText] = useState("");
  const [follow, setFollow] = useState(true);
  const [selectedEntry, setSelectedEntry] = useState<LogEntry | null>(null);

  // Debounce search to avoid flooding the server while the user types.
  const debouncedSetSearch = useMemo(() => debounce((text: string) => setSearchText(text), 300), []);
  useEffect(() => () => debouncedSetSearch.cancel(), [debouncedSetSearch]);

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchInput(value);
      if (value === "") {
        debouncedSetSearch.cancel();
        setSearchText("");
      } else {
        debouncedSetSearch(value);
      }
    },
    [debouncedSetSearch],
  );

  const { entries, isInitialLoading, error, bufferSize, bufferCapacity, truncated, refresh } = useServerLogs({
    minLevel,
    searchText,
    follow,
  });

  // Auto-scroll-to-bottom logic: keep the view pinned to the newest log
  // unless the user scrolled up to read history. This mirrors the
  // standard terminal-tail behavior used by Datadog/Grafana/etc.
  const scrollerRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const handleScroll = useCallback(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickToBottomRef.current = distanceFromBottom <= STICK_TO_BOTTOM_THRESHOLD;
  }, []);

  // Whenever entries grow, scroll to bottom only if the user was already
  // pinned there. We deliberately read the ref synchronously here.
  useEffect(() => {
    if (!stickToBottomRef.current) return;
    const el = scrollerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [entries.length]);

  const jumpToLatest = useCallback(() => {
    stickToBottomRef.current = true;
    const el = scrollerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  return (
    <>
      <div className="flex h-full flex-col">
        {/* Toolbar — sticky at the top, holds title, controls, filters. */}
        <div className="sticky top-0 z-3 flex flex-col gap-4 bg-surface-base px-6 pt-6 laptop:px-10 laptop:pt-10">
          <div className="flex items-center justify-between gap-3">
            <Header title="Server Logs" titleSize="text-heading-300" />
            <div className="flex items-center gap-3">
              <Switch label="Follow" checked={follow} setChecked={setFollow} />
              <Button variant={variants.secondary} size={sizes.compact} onClick={refresh}>
                Refresh
              </Button>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <Search initValue={searchInput} onChange={handleSearchChange} />
            <div className="flex items-center gap-1 rounded-lg border border-border-5 p-1">
              {LEVEL_OPTIONS.map((opt) => {
                const active = opt.value === minLevel;
                return (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setMinLevel(opt.value)}
                    className={clsx(
                      "rounded-md px-3 py-1 text-200",
                      active
                        ? "bg-core-accent-fill text-text-contrast"
                        : "text-text-primary-70 hover:bg-surface-elevated-base",
                    )}
                  >
                    {opt.label}
                  </button>
                );
              })}
            </div>
            <div className="ml-auto flex items-center gap-3 text-200 text-text-primary-50">
              <span>
                Buffer {bufferSize}/{bufferCapacity || "—"}
              </span>
              {truncated ? <span className="text-intent-warning-text">More entries available</span> : null}
            </div>
          </div>
        </div>

        {error ? (
          <Callout className="mx-6 mt-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={error} />
        ) : null}

        {/* Scrollable list region. */}
        <div className="relative flex min-h-0 flex-1 flex-col px-6 pt-4 pb-6 laptop:px-10 laptop:pb-10">
          <div
            ref={scrollerRef}
            onScroll={handleScroll}
            className="flex-1 overflow-y-auto rounded-xl border border-border-5 bg-surface-elevated-base"
          >
            {isInitialLoading ? (
              <div className="flex h-full items-center justify-center py-12">
                <ProgressCircular indeterminate />
              </div>
            ) : entries.length === 0 ? (
              <div className="flex h-full items-center justify-center py-12 text-300 text-text-primary-50">
                No log entries match the current filter.
              </div>
            ) : (
              entries.map((entry) => <LogEntryRow key={entry.id.toString()} entry={entry} onClick={setSelectedEntry} />)
            )}
          </div>

          {/* Floating "jump to latest" button shown only when the user
              has scrolled away from the tail. */}
          {!stickToBottomRef.current && entries.length > 0 ? (
            <div className="pointer-events-none absolute right-8 bottom-10 laptop:right-12 laptop:bottom-14">
              <Button
                variant={variants.primary}
                size={sizes.compact}
                onClick={jumpToLatest}
                className="pointer-events-auto"
              >
                Jump to latest
              </Button>
            </div>
          ) : null}
        </div>
      </div>

      <LogDetailModal entry={selectedEntry} onDismiss={() => setSelectedEntry(null)} />
    </>
  );
};

export default ServerLogsPage;
