import type { ReportedPool } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { ChevronDown } from "@/shared/assets/icons";

export const PoolSettingsComparison = ({
  expected,
  reported,
  assigned,
}: {
  expected: ReportedPool[];
  reported: ReportedPool[];
  assigned: boolean;
}) => (
  <div className="flex flex-col gap-5">
    {Array.from({ length: Math.max(expected.length, reported.length) }, (_, i) => {
      const desired = expected[i],
        actual = reported[i];
      const label = i === 0 ? "Primary pool" : `Backup pool ${i}`;
      const fields = [
        { label: "URL", wanted: desired?.url, actual: actual?.url },
        { label: "Username", wanted: desired?.username, actual: actual?.username },
      ];
      if (desired && actual && desired.priority !== actual.priority)
        fields.push({ label: "Priority", wanted: String(desired.priority), actual: String(actual.priority) });
      const different = assigned && fields.some((field) => field.wanted !== field.actual);
      const settings = assigned ? (
        <table aria-label={`${label} settings`} className="w-full table-fixed text-left text-200">
          <thead>
            <tr className="text-text-primary-50">
              <th className="w-22 pb-2 font-normal">Field</th>
              <th className="pr-4 pb-2 font-normal">Configuration</th>
              <th className="pb-2 font-normal">On device</th>
            </tr>
          </thead>
          <tbody>
            {fields.map((field) => (
              <tr key={field.label} className="border-t border-border-10 align-top">
                <th scope="row" className="py-3 pr-3 font-normal text-text-primary-70">
                  {field.label}
                </th>
                <td className="py-3 pr-4 [overflow-wrap:anywhere]">{field.wanted ?? "None"}</td>
                <td
                  className={`py-3 [overflow-wrap:anywhere] ${field.wanted !== field.actual ? "font-medium text-text-critical" : ""}`}
                >
                  {field.actual ?? "None"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <dl className="grid grid-cols-[5rem_1fr] gap-x-4 gap-y-3 text-200">
          {fields.map((field) => (
            <div key={field.label} className="contents">
              <dt className="text-text-primary-50">{field.label}</dt>
              <dd className="[overflow-wrap:anywhere]">{field.actual ?? "None"}</dd>
            </div>
          ))}
        </dl>
      );
      if (assigned && i > 0 && !different)
        return (
          <details key={i} className="group">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-emphasis-300 [&::-webkit-details-marker]:hidden">
              <ChevronDown width="w-3" className="shrink-0 -rotate-90 transition-transform group-open:rotate-0" />
              {label}
            </summary>
            <div className="mt-3">{settings}</div>
          </details>
        );
      return (
        <section key={i} aria-label={label} className="flex flex-col gap-3">
          <h4 className="text-emphasis-300">{label}</h4>
          {settings}
        </section>
      );
    })}
    {!expected.length && !reported.length ? (
      <p className="text-text-primary-70">No pools are configured on this device.</p>
    ) : null}
  </div>
);
