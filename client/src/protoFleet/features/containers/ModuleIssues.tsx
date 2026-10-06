import { useEffect, useState } from "react";
import type { InventoryItem, ModuleIssue } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

const componentName = (issue: ModuleIssue) =>
  issue.component === "psu" ? "PSU" : issue.component === "hashboard" ? "Hashboard" : "Module";
const causeName = (cause: string) => {
  const name = cause
    .replace(/^HASHBOARD_ERROR_CAUSE_/, "")
    .replace(/^(BOARD|PSU)_/, "")
    .toLowerCase()
    .replaceAll("_", " ");
  return name.charAt(0).toUpperCase() + name.slice(1);
};
const issueName = (issue: ModuleIssue) => `${componentName(issue)} · ${causeName(issue.cause)}`;

export function ModuleIssues({ item }: { item: InventoryItem }) {
  const [open, setOpen] = useState(false);
  const evidence = item.moduleIssues;
  const sourceKey = evidence?.observedAt ? `${evidence.observedAt.seconds}:${evidence.observedAt.nanos}` : undefined;
  const [expiredSource, setExpiredSource] = useState<string>();
  useEffect(() => {
    if (!sourceKey || !evidence?.observedAt) return;
    const sourceMs = Number(evidence.observedAt.seconds) * 1000 + evidence.observedAt.nanos / 1e6;
    const age = Date.now() - sourceMs;
    const timer = setTimeout(() => setExpiredSource(sourceKey), age < -5000 ? 0 : Math.max(0, 120_000 - age));
    return () => clearTimeout(timer);
  }, [sourceKey, evidence?.observedAt]);
  const stale = evidence?.stale || (sourceKey !== undefined && sourceKey === expiredSource);
  const issues = stale ? [] : (evidence?.items ?? []);
  const unavailable = stale || !evidence || !evidence.complete || !sourceKey;
  const availability = stale
    ? "Issue evidence is stale. Current health is not confirmed."
    : issues.length
      ? "Issue report is partial. Additional issues may be missing."
      : "Issue evidence is incomplete or unavailable. Current health is not confirmed.";
  let summary = "No reported issues";
  if (issues.length) {
    summary = issueName(issues[0]);
    if (issues.length > 1) summary += ` (+${issues.length - 1})`;
  } else if (stale) {
    summary = "Issues stale";
  } else if (unavailable) {
    summary = "Issues unavailable";
  } else if (item.state === "attention") {
    summary = "No reported cause";
  }
  return (
    <div onClick={(event) => event.stopPropagation()}>
      <button
        type="button"
        className="block max-w-full truncate text-left text-200 underline"
        title={summary}
        aria-label={`${item.name} issues: ${summary}`}
        onClick={() => setOpen(true)}
      >
        {summary}
      </button>
      {open ? (
        <Dialog
          open
          testId="module-issues"
          title={`${item.name} issues`}
          onDismiss={() => setOpen(false)}
          buttons={[{ text: "Close", variant: variants.secondary, onClick: () => setOpen(false) }]}
        >
          {unavailable ? <p className="py-2 text-text-primary-70">{availability}</p> : null}
          {!issues.length ? <p>{unavailable ? "No current issue details available." : summary}</p> : null}
          <ul className="space-y-3 py-3">
            {issues.map((issue) => (
              <li key={`${issue.component}:${issue.componentId}:${issue.cause}`}>
                <p>{issueName(issue)}</p>
                {issue.component === "hashboard" && issue.componentId ? (
                  <p className="text-200 text-text-primary-70">MAC address: {issue.componentId}</p>
                ) : null}
                <p className="text-200 text-text-primary-70">
                  {issue.severity === "observation"
                    ? "Observation"
                    : issue.severity === "warning"
                      ? "Warning"
                      : "Fault"}
                  {issue.observedAt
                    ? ` · Observed ${new Date(Number(issue.observedAt.seconds) * 1000).toLocaleString()}`
                    : ""}
                </p>
              </li>
            ))}
          </ul>
        </Dialog>
      ) : null}
    </div>
  );
}
