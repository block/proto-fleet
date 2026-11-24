import { useCallback, useMemo, useState } from "react";

import { eventLabel } from "./labels";
import RecoveryEvidenceModal from "./RecoveryEvidenceModal";
import { BOARD_EVENT_WINDOW_MAX_MS, type BoardEvent, type BoardEventQuery, type BoardHistorySource } from "./types";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { formatControllerTime } from "@/shared/features/controllerLogs/coverage";
import { RefreshControl } from "@/shared/features/controllerLogs/ReadControls";
import ReadNotices from "@/shared/features/controllerLogs/ReadNotices";
import { usePolledRead } from "@/shared/features/controllerLogs/usePolledRead";

const DAY_MS = 24 * 60 * 60 * 1000;
const RANGES = [
  { value: String(DAY_MS), label: "24 hours" },
  { value: String(BOARD_EVENT_WINDOW_MAX_MS), label: "7 days" },
];

type EventColumn = "time" | "event" | "lastSeen" | "samples";

const colTitles: ColTitles<EventColumn> = {
  time: "First seen",
  event: "Event",
  lastSeen: "Last seen",
  samples: "Readings",
};

const colConfig: ColConfig<BoardEvent, string, EventColumn> = {
  time: { width: "w-[190px]", component: (event) => formatControllerTime(event.time) || "—" },
  event: { width: "w-[320px]", component: eventLabel },
  lastSeen: { width: "w-[190px]", component: (event) => formatControllerTime(event.lastSeen) || "—" },
  samples: { width: "w-[100px]", component: (event) => event.samples ?? "—" },
};
const activeCols = Object.keys(colTitles) as EventColumn[];
const listPadding = { phone: "0px", tablet: "0px", laptop: "0px", desktop: "0px" };

interface BoardHistoryProps extends BoardHistorySource {
  /** Names downloaded evidence files. */
  boardLabel: string;
}

/**
 * Past events and retained recovery evidence for one CT1 hashboard. Everything
 * here is history the controller recorded; none of it is the board's current
 * state, which the board's status shows.
 */
const BoardHistory = ({ readEvents, listEvidence, readEvidence, boardLabel }: BoardHistoryProps) => {
  const [rangeMs, setRangeMs] = useState(String(DAY_MS));
  // Ends, in ms, of the windows paged back to; none reads up to now. Earlier
  // ends where the window the controller actually read started, so windows
  // meet without gaps, and Later returns through the same windows to now.
  const [ends, setEnds] = useState<number[]>([]);
  const endMs = ends[ends.length - 1];
  const [evidenceOpen, setEvidenceOpen] = useState(false);
  const selection = useMemo(() => ({ rangeMs: Number(rangeMs), endMs }), [rangeMs, endMs]);
  const read = useMemo(
    () =>
      readEvents &&
      ((query: typeof selection, signal: AbortSignal) => {
        const end = query.endMs ?? Date.now();
        const window: BoardEventQuery = {
          start: new Date(end - query.rangeMs).toISOString(),
          end: new Date(end).toISOString(),
        };
        return readEvents(window, signal);
      }),
    [readEvents],
  );
  const { data: page, error, loading, reading, refresh } = usePolledRead(selection, read);
  const closeEvidence = useCallback(() => setEvidenceOpen(false), []);

  return (
    <section aria-labelledby="board-history-title" className="flex flex-col gap-4" data-testid="board-history">
      <div className="flex flex-col gap-1">
        <h2 id="board-history-title" className="text-heading-200 text-text-primary">
          Board history
        </h2>
        <p className="text-300 text-text-primary-70">
          Past events the controller recorded for this board, not its current state. Times without recorded health
          readings show no events.
        </p>
      </div>
      {readEvents ? (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <Select
              id="board-history-range"
              label="Window"
              variant="filter"
              value={rangeMs}
              options={RANGES}
              onChange={(value) => {
                setRangeMs(value);
                setEnds([]);
              }}
            />
            <Button
              size={sizes.compact}
              variant={variants.secondary}
              text="Earlier"
              disabled={!page?.window}
              onClick={() => page?.window && setEnds([...ends, Date.parse(page.window.start)])}
              testId="board-history-earlier"
            />
            <Button
              size={sizes.compact}
              variant={variants.secondary}
              text="Later"
              disabled={endMs === undefined}
              onClick={() => setEnds(ends.slice(0, -1))}
              testId="board-history-later"
            />
            {page?.window ? (
              <span className="text-200 text-text-primary-50" data-testid="board-history-window">
                {formatControllerTime(page.window.start)} to {formatControllerTime(page.window.end)}
              </span>
            ) : null}
            <div className="ml-auto">
              <RefreshControl
                fetchedAt={page?.fetchedAt}
                reading={reading}
                onRefresh={refresh}
                testId="board-history-refresh"
              />
            </div>
          </div>
          <ReadNotices
            staleTitle="Couldn't refresh board history"
            unavailableTitle="Board history unavailable"
            fetchedAt={page?.fetchedAt}
            error={error}
            testIdPrefix="board-history"
            className=""
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
              paddingLeft={listPadding}
              paddingRight={listPadding}
              noDataElement={
                <div className="flex h-[189px] w-full items-center justify-center rounded-2xl bg-core-primary-5">
                  <div className="text-heading-100 text-text-primary-50">No events recorded in this window</div>
                </div>
              }
            />
          ) : null}
        </>
      ) : null}
      {listEvidence && readEvidence ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl bg-core-primary-5 px-4 py-3">
          <div className="flex flex-col">
            <div className="text-emphasis-300 text-text-primary">Recovery evidence</div>
            <div className="text-200 text-text-primary-70">
              Snapshots the controller captured when it tried to recover this board.
            </div>
          </div>
          <Button
            size={sizes.compact}
            variant={variants.secondary}
            text="View evidence"
            onClick={() => setEvidenceOpen(true)}
            testId="board-history-evidence"
          />
        </div>
      ) : null}
      {evidenceOpen && listEvidence && readEvidence ? (
        <RecoveryEvidenceModal
          listEvidence={listEvidence}
          readEvidence={readEvidence}
          boardLabel={boardLabel}
          onDismiss={closeEvidence}
        />
      ) : null}
    </section>
  );
};

export default BoardHistory;
