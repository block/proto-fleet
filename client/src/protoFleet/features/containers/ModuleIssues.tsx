import { useEffect, useState } from "react";
import type { InventoryItem, ModuleIssue } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { Alert, Hashboard, Info, LightningAlt } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

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
const timestampMs = (at: ModuleIssue["observedAt"]) => (at ? Number(at.seconds) * 1000 + at.nanos / 1e6 : undefined);
const componentIcon = (issue: ModuleIssue) =>
  issue.component === "hashboard" ? (
    <Hashboard />
  ) : issue.component === "psu" ? (
    <LightningAlt />
  ) : issue.severity === "observation" ? (
    <Info />
  ) : (
    <Alert />
  );

export function ModuleIssues({ item }: { item: InventoryItem }) {
  const [open, setOpen] = useState(false);
  const evidence = item.moduleIssues;
  const sourceMs = timestampMs(evidence?.observedAt);
  const [expiredTimes, setExpiredTimes] = useState<number[]>([]);
  useEffect(() => {
    const times = [sourceMs, ...(evidence?.items ?? []).map((issue) => timestampMs(issue.observedAt))].filter(
      (at): at is number => at !== undefined,
    );
    if (!times.length) return;
    const now = Date.now();
    const delay = Math.min(...times.map((at) => (at > now + 5000 ? 0 : Math.max(0, at + 120_000 - now))));
    let timer: ReturnType<typeof setTimeout>;
    const expire = () => {
      const now = Date.now();
      setExpiredTimes(times.filter((at) => at > now + 5000 || now - at >= 120_000));
      const next = times.filter((at) => at <= now + 5000 && at + 120_000 > now);
      if (next.length) timer = setTimeout(expire, Math.ceil(Math.min(...next) + 120_000 - now));
    };
    timer = setTimeout(expire, Math.ceil(delay));
    return () => clearTimeout(timer);
  }, [sourceMs, evidence?.items]);
  const stale = evidence?.stale || (sourceMs !== undefined && expiredTimes.includes(sourceMs));
  const issues = (evidence?.items ?? []).filter((issue) => {
    const at = timestampMs(issue.observedAt);
    return at !== undefined && !expiredTimes.includes(at);
  });
  const unavailable = stale || !evidence || !evidence.complete || sourceMs === undefined;
  const intent = issues.some((issue) => issue.severity === "fault")
    ? "critical"
    : issues.some((issue) => issue.severity === "warning")
      ? "warning"
      : "info";
  const availability = issues.length
    ? "Issue report is partial. Additional issues may be missing."
    : stale
      ? "Issue evidence is stale. Current health is not confirmed."
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
          icon={
            intent === "info" ? (
              <Info width={iconSizes.xLarge} className="text-text-primary-30" />
            ) : (
              <Alert
                width={iconSizes.xLarge}
                className={intent === "critical" ? "text-intent-critical-fill" : "text-intent-warning-fill"}
              />
            )
          }
          onDismiss={() => setOpen(false)}
          buttons={[{ text: "Close", variant: variants.secondary, onClick: () => setOpen(false) }]}
        >
          {unavailable ? <p className="py-2 text-text-primary-70">{availability}</p> : null}
          {!issues.length ? <p>{unavailable ? "No current issue details available." : summary}</p> : null}
          <ul className="space-y-3 py-3">
            {issues.map((issue) => (
              <li key={`${issue.component}:${issue.componentId}:${issue.cause}`} className="flex items-start gap-3">
                <div className="shrink-0 text-text-primary-70">
                  <DialogIcon>{componentIcon(issue)}</DialogIcon>
                </div>
                <div className="min-w-0 flex-1">
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
                </div>
              </li>
            ))}
          </ul>
        </Dialog>
      ) : null}
    </div>
  );
}
