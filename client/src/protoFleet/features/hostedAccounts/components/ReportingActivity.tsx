import { useState } from "react";

import { reportCause, reportLabel, reportTime } from "../reportFormat";
import type { HostReportQueueItem, ReportingTarget } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import Button, { sizes } from "@/shared/components/Button";
import List from "@/shared/components/List";

const notificationLabels: Record<string, string> = {
  pending: "Pending",
  sent: "Accepted by destinations",
  failed: "Delivery failed",
  unrouted: "No destination accepted the notification",
  exhausted: "Automatic retries stopped",
  blocked: "Blocked by reporting permissions",
  acknowledged: "Acknowledged",
  quiet: "No notification needed",
  superseded: "Superseded",
};
const eventLabels: Record<string, string> = {
  changed: "Reporting status updated",
  attempted: "Notification attempted",
  failed: "Notification failed",
  sent: "Notification accepted",
  unrouted: "Notification not routed",
  exhausted: "Automatic retries stopped",
  blocked: "Notification blocked",
  retry: "Notification retry requested",
  acknowledged: "Acknowledged",
};

const Target = ({ target }: { target?: ReportingTarget }) => (
  <div className="flex flex-col gap-1">
    <span>{target?.date || "—"}</span>
    {target?.date && target.status ? <span className="text-text-primary-70">{reportLabel(target.status)}</span> : null}
    {target?.reasonableEfforts ? <span className="text-text-primary-70">Reasonable-efforts target</span> : null}
    {target?.causes
      .filter((cause) => !(target.reasonableEfforts && cause === "reasonable_efforts_target"))
      .map((cause) => (
        <p key={cause} className="text-text-primary-70">
          {reportCause(cause)}
        </p>
      ))}
  </div>
);

const ReportingActivity = ({
  item,
  onAcknowledge,
  onRetry,
}: {
  item: HostReportQueueItem;
  onAcknowledge: () => void;
  onRetry: () => void;
}) => {
  const [visibleEvents, setVisibleEvents] = useState(10);
  const events = item.events
    .map((event, key) => ({ event, key }))
    .sort((a, b) => {
      const seconds = (b.event.createdAt?.seconds ?? 0n) - (a.event.createdAt?.seconds ?? 0n);
      return seconds > 0n
        ? 1
        : seconds < 0n
          ? -1
          : (b.event.createdAt?.nanos ?? 0) - (a.event.createdAt?.nanos ?? 0) || b.key - a.key;
    });
  const reasons = [...new Set(item.reasons.map(reportCause))];
  return (
    <details aria-label={`Reporting activity for ${item.accountName}`} className="border-t border-border-5 pt-4">
      <summary className="cursor-pointer text-emphasis-300">Reporting activity</summary>
      <section aria-label="Reporting activity" className="mt-4 flex flex-col gap-4 text-300">
        <dl className="grid grid-cols-2 gap-4 phone:grid-cols-1">
          <div>
            <dt className="mb-1 text-emphasis-300">Contract due date</dt>
            <dd>
              <Target target={item.deadlines?.contract} />
            </dd>
          </div>
          <div>
            <dt className="mb-1 text-emphasis-300">Internal target</dt>
            <dd>
              <Target target={item.deadlines?.internal} />
            </dd>
          </div>
          <div>
            <dt className="mb-1 text-emphasis-300">Owner</dt>
            <dd>{item.ownerUsername || "—"}</dd>
          </div>
          <div>
            <dt className="mb-1 text-emphasis-300">Internal notification</dt>
            <dd>{notificationLabels[item.notificationStatus] || reportLabel(item.notificationStatus)}</dd>
          </div>
        </dl>
        {item.canAcknowledge || item.canRetry ? (
          <div className="flex flex-wrap gap-3">
            {item.canAcknowledge ? (
              <Button variant="secondary" size={sizes.compact} text="Acknowledge" onClick={onAcknowledge} />
            ) : null}
            {item.canRetry ? (
              <Button variant="secondary" size={sizes.compact} text="Retry notification" onClick={onRetry} />
            ) : null}
          </div>
        ) : null}
        {reasons.length ? (
          <details>
            <summary className="cursor-pointer text-emphasis-300">Data and workflow notes</summary>
            <ul className="mt-2 list-disc space-y-1 pl-5 text-text-primary-70">
              {reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          </details>
        ) : null}
        {events.length ? (
          <section aria-label="Activity history" className="flex flex-col gap-3">
            <h3 className="text-emphasis-300">Recent activity</h3>
            <List<(typeof events)[number], number, "time" | "event" | "reason">
              items={events.slice(0, visibleEvents)}
              itemKey="key"
              itemSelectable={false}
              stickyFirstColumn={false}
              activeCols={["time", "event", "reason"]}
              colTitles={{ time: "Time", event: "Activity", reason: "Details" }}
              colConfig={{
                time: { width: "min-w-48", allowWrap: true, component: ({ event }) => reportTime(event.createdAt) },
                event: {
                  width: "min-w-48",
                  allowWrap: true,
                  component: ({ event }) => eventLabels[event.kind] || reportLabel(event.kind),
                },
                reason: { width: "min-w-64", allowWrap: true, component: ({ event }) => event.reason || "—" },
              }}
            />
            {events.length > visibleEvents ? (
              <div>
                <Button
                  variant="secondary"
                  size={sizes.compact}
                  text="Show older activity"
                  onClick={() => setVisibleEvents((count) => count + 10)}
                />
              </div>
            ) : null}
          </section>
        ) : null}
      </section>
    </details>
  );
};

export default ReportingActivity;
