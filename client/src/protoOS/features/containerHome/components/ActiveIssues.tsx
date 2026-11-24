import clsx from "clsx";
import type { IssueSummary } from "../types";

const MAX_ROWS = 6;

const severityLabels = { critical: "Errors", warning: "Warnings" } as const;

const SeverityMark = ({ severity }: { severity: "critical" | "warning" }) => (
  <span
    aria-hidden="true"
    className={clsx(
      "inline-block size-2.5 shrink-0",
      severity === "critical"
        ? "rounded-full bg-intent-critical-fill"
        : "bg-intent-warning-fill [clip-path:polygon(50%_0,100%_100%,0_100%)]",
    )}
  />
);

/** Grouped active issues, most severe first; the full list stays in the health details. */
export default function ActiveIssues({ issues }: { issues: IssueSummary }) {
  if (!issues.groups.length) return <p className="text-300 text-text-primary-50">No active issues.</p>;
  const shown = issues.groups.slice(0, MAX_ROWS);
  const hidden = issues.groups.length - shown.length;
  return (
    <div className="flex flex-col gap-4">
      {(["critical", "warning"] as const).map((severity) => {
        const rows = shown.filter((group) => group.severity === severity);
        if (!rows.length) return null;
        return (
          <section key={severity} aria-label={severityLabels[severity]}>
            <h3 className="flex items-center gap-2 text-200 text-text-primary-50">
              <SeverityMark severity={severity} />
              {severityLabels[severity]}
              <span className="tabular-nums">{issues[severity]}</span>
            </h3>
            <ul className="mt-1 divide-y divide-border-5">
              {rows.map((group) => (
                <li key={group.id} className="flex items-baseline justify-between gap-4 py-2.5 text-300">
                  <span className="min-w-0 text-text-primary">{group.label}</span>
                  <span className="shrink-0 text-200 text-text-primary-50 tabular-nums">{group.affected}</span>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
      {hidden > 0 ? (
        <p className="text-200 text-text-primary-50">
          {hidden} more {hidden === 1 ? "issue" : "issues"}
        </p>
      ) : null}
    </div>
  );
}
