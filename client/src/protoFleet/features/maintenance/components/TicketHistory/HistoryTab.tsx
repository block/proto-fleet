import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ListPagination from "../ListPagination";
import TicketDetailModal from "../TicketDetail/TicketDetailModal";
import type { Assignee } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import {
  SortDirection,
  TicketResolution,
  TicketSortField,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";

type Columns = "issue" | "asset" | "resolution" | "completedAt" | "assignee";
type Item = {
  id: string;
  ticketNumber: string;
  component: string;
  diagnosis: string;
  minerIdentifier: string | null;
  resolution: string;
  assigneeName: string | null;
  completedAt: string;
  siteName: string;
  buildingName: string;
};
const activeCols: Columns[] = ["issue", "asset", "resolution", "completedAt", "assignee"];
const colTitles: ColTitles<Columns> = {
  issue: "Issue",
  asset: "Asset",
  resolution: "Resolution",
  completedAt: "Completed",
  assignee: "Technician",
};
const POLL_INTERVAL_MS = 15_000;
const resolutionLabels: Partial<Record<TicketResolution, string>> = {
  [TicketResolution.REPAIRED]: "Repaired",
  [TicketResolution.REPLACED]: "Replaced",
  [TicketResolution.DEFERRED]: "Deferred",
  [TicketResolution.UNREPAIRABLE]: "Unrepairable",
  [TicketResolution.NO_ACTION_NEEDED]: "No action needed",
};
const HistoryTab = () => {
  const { listCompleted } = useMaintenanceApi();
  const [items, setItems] = useState<Item[]>([]);
  const [assigneeFacets, setAssigneeFacets] = useState<Assignee[]>([]);
  const [total, setTotal] = useState(0);
  const [next, setNext] = useState("");
  const [currentPage, setCurrentPage] = useState(0);
  const [cursorHistory, setCursorHistory] = useState<string[]>([""]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [component, setComponent] = useState("");
  const [assignee, setAssignee] = useState("");
  const [detailId, setDetailId] = useState<string | null>(null);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const load = useCallback(
    async (page = 0, pageToken = "") => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
      await listCompleted({
        componentFilter: component || undefined,
        assigneeUserIdFilter: assignee ? BigInt(assignee) : undefined,
        sortField: TicketSortField.COMPLETED_AT,
        sortDirection: SortDirection.DESC,
        pageSize: 50,
        pageToken,
        signal: current.signal,
        onSuccess: (response) => {
          if (request !== sequence.current) return;
          const mapped = response.tickets.flatMap((summary) =>
            summary.ticket
              ? [
                  {
                    id: summary.ticket.id.toString(),
                    ticketNumber: summary.ticket.ticketNumber,
                    component: summary.ticket.component,
                    diagnosis: summary.ticket.diagnosis,
                    minerIdentifier: summary.ticket.minerIdentifier ?? null,
                    resolution: resolutionLabels[summary.ticket.resolution] ?? "Unknown",
                    assigneeName: summary.ticket.assigneeName || null,
                    completedAt: summary.ticket.completedAt
                      ? new Date(Number(summary.ticket.completedAt.seconds) * 1000).toLocaleString()
                      : "",
                    siteName: summary.ticket.siteName,
                    buildingName: summary.ticket.buildingName,
                  },
                ]
              : [],
          );
          setItems(mapped);
          setAssigneeFacets(response.assigneeFacets);
          setTotal(response.totalCount);
          setNext(response.nextPageToken);
          setCurrentPage(page);
          if (response.nextPageToken) {
            setCursorHistory((old) => {
              const cursors = [...old];
              cursors[page + 1] = response.nextPageToken;
              return cursors;
            });
          }
        },
        onError: (message) => {
          if (request === sequence.current) setError(message);
        },
        onFinally: () => {
          if (request === sequence.current) setLoading(false);
        },
      });
    },
    [assignee, component, listCompleted],
  );
  const refreshCurrentPage = useCallback(
    () => load(currentPage, cursorHistory[currentPage] ?? ""),
    [currentPage, cursorHistory, load],
  );
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setCurrentPage(0);
      setCursorHistory([""]);
      setNext("");
      void load(0, "");
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [assignee, component]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    let active = true;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;
    const scheduleNext = () => {
      timeoutId = setTimeout(async () => {
        if (!active) return;
        try {
          await refreshCurrentPage();
        } catch {
          // RPC adapters report request failures through load's onError callback.
        } finally {
          if (active) scheduleNext();
        }
      }, POLL_INTERVAL_MS);
    };
    scheduleNext();
    return () => {
      active = false;
      if (timeoutId !== undefined) clearTimeout(timeoutId);
    };
  }, [refreshCurrentPage]);
  const colConfig: ColConfig<Item, string, Columns> = useMemo(
    () => ({
      issue: {
        component: (t) => (
          <div>
            <div>
              {t.component}: {t.diagnosis}
            </div>
            <span>{t.ticketNumber}</span>
          </div>
        ),
        width: "w-64",
      },
      asset: {
        component: (t) => (
          <div>
            {t.minerIdentifier ?? t.component}
            <div>
              {t.buildingName}, {t.siteName}
            </div>
          </div>
        ),
        width: "w-48",
      },
      resolution: { component: (t) => <span>{t.resolution}</span>, width: "w-32" },
      completedAt: { component: (t) => <span>{t.completedAt}</span>, width: "w-28" },
      assignee: { component: (t) => <span>{t.assigneeName ?? "Unassigned"}</span>, width: "w-36" },
    }),
    [],
  );
  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-3">
        <Input id="history-component" label="Component" initValue={component} onChange={setComponent} />
        <Select
          id="history-assignee"
          label="Technician"
          value={assignee}
          options={[
            { value: "", label: "All technicians" },
            ...assigneeFacets.map((item) => ({ value: item.userId.toString(), label: item.username })),
          ]}
          onChange={setAssignee}
        />
      </div>
      {error && !items.length ? (
        <div role="alert">{error}</div>
      ) : loading && !items.length ? (
        <div role="status" aria-label="Loading history" className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <List
          items={items}
          itemKey="id"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          stickyFirstColumn={false}
          overflowContainer={false}
          total={total}
          hideTotal
          itemName={{ singular: "completed ticket", plural: "completed tickets" }}
          onRowClick={(item) => setDetailId(item.id)}
        />
      )}
      <ListPagination
        currentPage={currentPage}
        pageSize={50}
        visibleCount={items.length}
        total={total}
        itemName="completed tickets"
        hasNextPage={!!next}
        loading={loading}
        onPrevious={() => void load(currentPage - 1, cursorHistory[currentPage - 1])}
        onNext={() => void load(currentPage + 1, cursorHistory[currentPage + 1] ?? next)}
      />
      {detailId ? (
        <TicketDetailModal
          ticketId={detailId}
          ticketIds={items.map((item) => item.id)}
          onDismiss={() => setDetailId(null)}
        />
      ) : null}
    </div>
  );
};
export default HistoryTab;
