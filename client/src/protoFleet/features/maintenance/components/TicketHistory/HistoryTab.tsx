import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import TicketDetailModal from "../TicketDetail/TicketDetailModal";
import {
  SortDirection,
  TicketResolution,
  TicketSortField,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
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
const resolutionLabels: Partial<Record<TicketResolution, string>> = {
  [TicketResolution.REPAIRED]: "Repaired",
  [TicketResolution.REPLACED]: "Replaced",
  [TicketResolution.DEFERRED]: "Deferred",
  [TicketResolution.UNREPAIRABLE]: "Unrepairable",
  [TicketResolution.NO_ACTION_NEEDED]: "No action needed",
};
const HistoryTab = () => {
  const { listCompleted } = useMaintenanceApi();
  const options = useMaintenanceOptions();
  const [items, setItems] = useState<Item[]>([]);
  const [total, setTotal] = useState(0);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [component, setComponent] = useState("");
  const [assignee, setAssignee] = useState("");
  const [detailId, setDetailId] = useState<string | null>(null);
  const controller = useRef<AbortController | undefined>(undefined);
  const load = useCallback(
    async (append = false) => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      setLoading(true);
      setError(null);
      await listCompleted({
        componentFilter: component || undefined,
        assigneeUserIdFilter: assignee ? BigInt(assignee) : undefined,
        sortField: TicketSortField.CREATED_AT,
        sortDirection: SortDirection.DESC,
        pageSize: 50,
        pageToken: append ? next : "",
        signal: current.signal,
        onSuccess: (response) => {
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
          setItems((old) => (append ? [...old, ...mapped] : mapped));
          setTotal(response.totalCount);
          setNext(response.nextPageToken);
        },
        onError: setError,
        onFinally: () => setLoading(false),
      });
    },
    [assignee, component, listCompleted, next],
  );
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void load();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [assignee, component]); // eslint-disable-line react-hooks/exhaustive-deps
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
            ...options.assignees.map((item) => ({ value: item.id, label: item.username })),
          ]}
          onChange={setAssignee}
        />
      </div>
      {error && !items.length ? (
        <div role="alert">{error}</div>
      ) : loading && !items.length ? (
        <div role="status">Loading history…</div>
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
          itemName={{ singular: "completed ticket", plural: "completed tickets" }}
          onRowClick={(item) => setDetailId(item.id)}
        />
      )}
      {next ? (
        <Button
          text="Load more"
          variant={variants.secondary}
          size={buttonSizes.compact}
          onClick={() => void load(true)}
        />
      ) : null}
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
