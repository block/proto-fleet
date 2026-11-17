import { useCallback, useState } from "react";

import { reportTime } from "../reportFormat";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import type { AccountReport, ReportSummary } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import List from "@/shared/components/List";

type Publication = Pick<
  ReportSummary,
  "reportId" | "seriesId" | "number" | "status" | "issuedAt" | "correctionOf" | "correctionReason"
>;

const ReportPublicationHistory = ({
  report,
  publications,
  loadReport,
  onSelect,
}: {
  report: AccountReport;
  publications: Publication[];
  loadReport: (id: string, signal: AbortSignal) => Promise<AccountReport | undefined>;
  onSelect: (id: string) => void;
}) => {
  const [open, setOpen] = useState(false);
  const hasHistory =
    !!report.correctionOf ||
    publications.some(
      (item) =>
        report.seriesId &&
        item.seriesId === report.seriesId &&
        item.reportId !== report.reportId &&
        item.status === "issued",
    );
  const known = [
    ...publications.filter((item) => report.seriesId && item.seriesId === report.seriesId && item.status === "issued"),
    report,
  ];
  // An explicit string keeps the async loader memoizable by the React compiler.
  const ancestry = `${JSON.stringify(
    known.map((item) => [item.reportId, item.correctionOf]).sort(([a], [b]) => a.localeCompare(b)),
  )}`;
  const seriesId = report.seriesId;
  const load = useCallback(
    async (signal: AbortSignal) => {
      const links: [string, string][] = JSON.parse(ancestry);
      const ids = new Set(links.map(([id]) => id));
      const pending = links.map(([, previous]) => previous).filter(Boolean);
      const earlier: Publication[] = [];
      for (const id of pending) {
        if (ids.has(id)) continue;
        const previous = await loadReport(id, signal);
        if (!previous || previous.reportId !== id || previous.status !== "issued" || previous.seriesId !== seriesId) {
          throw new Error("An earlier publication is unavailable.");
        }
        ids.add(previous.reportId);
        earlier.push(previous);
        if (previous.correctionOf) pending.push(previous.correctionOf);
      }
      return earlier;
    },
    [ancestry, seriesId, loadReport],
  );
  const query = useAccountQuery(load, hasHistory && open);
  const items = [...new Map([...(query.data ?? []), ...known].map((item) => [item.reportId, item])).values()]
    .filter((item) => item.status === "issued")
    .sort((a, b) => (a.number > b.number ? -1 : a.number < b.number ? 1 : 0));
  if (!hasHistory) return null;
  return (
    <section aria-label="Publication history">
      <details className="border-t border-border-5 pt-4" onToggle={(event) => setOpen(event.currentTarget.open)}>
        <summary className="cursor-pointer text-emphasis-300">Publication history</summary>
        {open ? (
          <div className="mt-4">
            <AccountState loading={query.loading} error={query.error} retry={query.refresh} />
            {query.data ? (
              <List<Publication, string, "date" | "state" | "reason">
                items={items}
                itemKey="reportId"
                itemSelectable={false}
                stickyFirstColumn={false}
                activeCols={["date", "state", "reason"]}
                colTitles={{ date: "Published", state: "Publication", reason: "Replacement reason" }}
                colConfig={{
                  date: {
                    width: "min-w-48",
                    allowWrap: true,
                    component: (item) => (item.issuedAt ? reportTime(item.issuedAt, report.period?.timezone) : "—"),
                  },
                  state: {
                    width: "min-w-48",
                    component: (item) => (item.reportId === items[0]?.reportId ? "Current publication" : "Replaced"),
                  },
                  reason: { width: "min-w-64", allowWrap: true, component: (item) => item.correctionReason || "—" },
                }}
                onRowClick={(item) => onSelect(item.reportId)}
              />
            ) : null}
          </div>
        ) : null}
      </details>
    </section>
  );
};

export default ReportPublicationHistory;
