import { useMemo, useState } from "react";
import type {
  PoolAssignmentService,
  PoolConfigurationUpdate,
  PoolConfigurationUpdatePage,
} from "@/protoOS/api/adapters/poolAssignment";
import Button from "@/shared/components/Button";
import { PageControls } from "@/shared/components/PageControls";
import { ConfigurationUpdateRow } from "@/shared/components/PoolConfigurations/ConfigurationUpdateRow";
import { PoolUpdateResults, PoolUpdateResultSection } from "@/shared/components/PoolConfigurations/PoolUpdateResults";
import { usePoll } from "@/shared/hooks/usePoll";

const PAGE_SIZE = 5;

const recordedDate = (time?: string) => {
  if (!time) return;
  const date = new Date(time);
  return Number.isNaN(date.getTime()) ? undefined : date;
};

export default function PoolConfigurationUpdates({
  service,
  id,
  onView,
}: {
  service: PoolAssignmentService;
  id: string;
  onView: (update: PoolConfigurationUpdate) => void;
}) {
  const [history, setHistory] = useState<PoolConfigurationUpdatePage>();
  const [failed, setFailed] = useState(false);
  const [request, setRequest] = useState(0);
  const [page, setPage] = useState(0);
  const params = useMemo(() => ({ service, id, request }), [service, id, request]);
  usePoll({
    enabled: Boolean(service.listConfigurationUpdates),
    poll: false,
    params,
    fetchData: async (signal) => {
      if (!service.listConfigurationUpdates) return;
      try {
        const next = await service.listConfigurationUpdates(id, { signal });
        if (!signal.aborted) setHistory(next);
      } catch {
        if (!signal.aborted) setFailed(true);
      }
    },
  });

  const renderHistory = () => {
    if (!service.listConfigurationUpdates) return <p>Update history is unavailable on this controller.</p>;
    if (failed)
      return (
        <div className="flex items-center justify-between gap-3" role="alert">
          <p>Could not load update history.</p>
          <Button
            variant="textOnly"
            text="Retry"
            onClick={() => {
              setFailed(false);
              setRequest((value) => value + 1);
            }}
          />
        </div>
      );
    if (!history) return <p role="status">Loading updates…</p>;
    const { updates, limitReached } = history;
    if (!updates.length && !limitReached) return <p>No updates reported in the last 7 days.</p>;

    const pageStart = page * PAGE_SIZE;
    const pageEnd = pageStart + PAGE_SIZE;
    return (
      <>
        {limitReached ? (
          <p role="status" className="text-200 text-text-primary-70">
            Some updates may be missing. The controller reached its record limit.
          </p>
        ) : null}
        {updates.slice(pageStart, pageEnd).map((update) => (
          <ConfigurationUpdateRow
            key={update.id}
            time={recordedDate(update.time)}
            summary={update.macs.length ? `${update.action} · ${update.macs.length} equipment` : update.action}
            onView={() => onView(update)}
          />
        ))}
        <PageControls
          previous={page > 0}
          next={pageEnd < updates.length}
          onPrevious={() => setPage((value) => value - 1)}
          onNext={() => setPage((value) => value + 1)}
        />
      </>
    );
  };

  return (
    <section aria-label="Update history" className="flex flex-col gap-3">
      <h3 className="text-heading-100">Update history</h3>
      {renderHistory()}
    </section>
  );
}

function describeRequestOutcome(result: string) {
  if (result === "ok") {
    return {
      status: "Request accepted",
      explanation: "The controller accepted the request. Hashboard verification is unavailable for this record.",
    };
  }
  if (result === "error") {
    return {
      status: "Request returned an error",
      explanation: "The controller reported an error for this request.",
    };
  }
  return {
    status: "Request outcome unavailable",
    explanation: "The controller did not report an outcome for this request.",
  };
}

export function PoolConfigurationUpdateResults({
  update,
  configurationName,
  onDismiss,
}: {
  update: PoolConfigurationUpdate;
  configurationName: string;
  onDismiss: () => void;
}) {
  const { status, explanation } = describeRequestOutcome(update.result);
  const recordedAt = recordedDate(update.time);
  const targets = update.macs.length ? update.macs.map((mac) => `Hashboard ${mac}`) : [update.action];
  return (
    <PoolUpdateResults onDismiss={onDismiss}>
      {update.macs.length ? <p className="text-300 text-text-primary-70">{update.action}</p> : null}
      {targets.map((name) => (
        <PoolUpdateResultSection
          key={name}
          name={name}
          configurationName={configurationName}
          status={status}
          explanation={explanation}
          requestedAt={recordedAt}
          requestedAtLabel="Update recorded"
        />
      ))}
    </PoolUpdateResults>
  );
}
