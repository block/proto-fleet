import { type ReactNode } from "react";

import { reportPeriod, reportTime, reportTitle, reportWorkflow } from "../reportFormat";
import ReportWorkbook from "./ReportWorkbook";
import type { AccountReport } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

const ReportContent = ({
  report,
  actions,
  publicationHistory,
  replaced = false,
}: {
  report: AccountReport;
  actions?: ReactNode;
  publicationHistory?: ReactNode;
  replaced?: boolean;
}) => (
  <div className="flex min-w-0 flex-col gap-6 text-300">
    <div>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <h2 className="text-heading-200">{reportTitle(report.period?.kind)}</h2>
        {actions}
      </div>
      <p className="mt-2 text-text-primary-70">{reportPeriod(report.period)}</p>
      <div className="mt-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-text-primary-70">
        {replaced || !report.issuedAt ? <span>{replaced ? "Replaced" : reportWorkflow(report.status)}</span> : null}
        {report.issuedAt ? <span>Published {reportTime(report.issuedAt, report.period?.timezone)}</span> : null}
      </div>
      {report.exception ? (
        <p className="mt-3 text-text-primary-70">
          <span className="text-emphasis-300">Publication note: </span>
          {report.exception}
        </p>
      ) : null}
      {report.correctionReason ? (
        <p className="mt-3 text-text-primary-70">
          <span className="text-emphasis-300">Replacement reason: </span>
          {report.correctionReason}
        </p>
      ) : null}
    </div>
    {report.result ? (
      <>
        {!report.result.complete ? (
          <p role="note" className="text-text-primary-70">
            Data is incomplete. Provisional values may change; unavailable results could not be calculated.
          </p>
        ) : null}
        <ReportWorkbook key={report.reportId} report={report} />
      </>
    ) : null}
    {publicationHistory}
  </div>
);

export default ReportContent;
