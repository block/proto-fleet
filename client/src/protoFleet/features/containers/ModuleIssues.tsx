import { type ReactNode, useEffect, useState } from "react";
import type { InventoryItem, ModuleIssue } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { Alert, Hashboard, Info, LightningAlt } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import StatusModalIcon from "@/shared/components/StatusModal/StatusModalIcon";
import StatusModalLayout from "@/shared/components/StatusModal/StatusModalLayout";
import { type GroupedStatusErrors, useMinerIssues } from "@/shared/hooks/useStatusSummary";

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
const componentIcon = (issue: ModuleIssue, width?: string) =>
  issue.component === "hashboard" ? (
    <Hashboard width={width} />
  ) : issue.component === "psu" ? (
    <LightningAlt width={width} />
  ) : issue.severity === "observation" ? (
    <Info width={width} />
  ) : (
    <Alert width={width} />
  );

export function ModuleIssues({
  item,
  hashboardPosition,
  children,
}: {
  item: Pick<InventoryItem, "name" | "moduleIssues">;
  hashboardPosition?: number;
  children?: ReactNode;
}) {
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
  const grouped: GroupedStatusErrors = { hashboard: [], psu: [], fan: [], controlBoard: [], other: [] };
  for (const issue of issues) {
    const componentType =
      issue.severity === "observation"
        ? "other"
        : issue.component === "hashboard"
          ? "hashboard"
          : issue.component === "psu"
            ? "psu"
            : "other";
    grouped[componentType].push({ componentType, slot: componentType === "hashboard" ? hashboardPosition : undefined });
  }
  const { summary } = useMinerIssues(false, false, grouped);
  if (!issues.length) return null;
  const icon =
    new Set(issues.map((issue) => issue.component)).size > 1 ? <Alert width="w-4" /> : componentIcon(issues[0], "w-4");
  return (
    <div onClick={(event) => event.stopPropagation()}>
      <button
        type="button"
        className="flex cursor-pointer items-center gap-2 hover:underline"
        title={summary ?? undefined}
        aria-label={`${item.name} issues: ${summary}`}
        onClick={() => setOpen(true)}
      >
        {icon}
        {summary}
      </button>
      {open ? (
        <Modal
          open
          testId="module-issues"
          title={`${item.name} issues`}
          dialogLabel={`${item.name} issues`}
          manageFocus
          onDismiss={() => setOpen(false)}
          buttons={[{ text: "Done", variant: variants.primary, onClick: () => setOpen(false) }]}
        >
          <StatusModalLayout
            icon={
              <StatusModalIcon intent={intent}>
                {intent === "info" ? <Info width={iconSizes.xLarge} /> : <Alert width={iconSizes.xLarge} />}
              </StatusModalIcon>
            }
            title={issues.length > 1 ? "Multiple issues detected" : issueName(issues[0])}
            subtitle={unavailable ? "Issue report is partial. Additional issues may be missing." : undefined}
            errors={issues.map((issue) => ({
              key: `${issue.component}:${issue.componentId}:${issue.cause}`,
              icon: componentIcon(issue),
              title: issueName(issue),
              subtitle: [
                issue.component === "hashboard" && issue.componentId ? `MAC address: ${issue.componentId}` : undefined,
                issue.severity === "observation" ? "Observation" : issue.severity === "warning" ? "Warning" : "Fault",
                issue.observedAt
                  ? `Observed ${new Date(Number(issue.observedAt.seconds) * 1000).toLocaleString()}`
                  : undefined,
              ]
                .filter(Boolean)
                .join(" · "),
            }))}
          >
            {children}
          </StatusModalLayout>
        </Modal>
      ) : null}
    </div>
  );
}
