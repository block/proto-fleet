import { create } from "@bufbuild/protobuf";

import {
  AccountReportSchema,
  HostReportingSetupSchema,
  ReportSummarySchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

export const reportingStoryAccount = "11111111-1111-4111-8111-111111111111";
export const reportingStoryUser = "22222222-2222-4222-8222-222222222222";
export const reportFixture = (complete = false, corrected = true) =>
  create(AccountReportSchema, {
    reportId: corrected ? "44444444-4444-4444-8444-444444444444" : "33333333-3333-4333-8333-333333333333",
    seriesId: "55555555-5555-4555-8555-555555555555",
    accountId: reportingStoryAccount,
    accountName: "Northstar Mining",
    number: corrected ? 2n : 1n,
    revision: 4n,
    status: "issued",
    issuedAt: { seconds: 1793577600n },
    correctionOf: corrected ? "33333333-3333-4333-8333-333333333333" : "",
    correctionReason: corrected ? "Replaced an estimated meter interval with the verified meter reading." : "",
    exception: complete
      ? ""
      : "Operations and finance reviewed the missing contractual definitions; affected values remain unavailable.",
    period: {
      kind: "monthly",
      start: { seconds: 1790802000n },
      end: { seconds: 1793480400n },
      asOf: { seconds: 1793577600n },
      timezone: "Africa/Nairobi",
    },
    result: {
      complete,
      metrics: {
        monthly_uptime: complete
          ? {
              status: "authoritative",
              value: "98.2",
              unit: "%",
              method: "approved_contract_v1",
              sources: ["Signed agreement / approved monthly calculation"],
            }
          : { status: "unavailable", causes: ["missing_G1"], sources: ["Contract definitions awaiting approval"] },
        metered_energy_kwh: {
          status: "authoritative",
          value: "12345.6789",
          unit: "kWh",
          sources: ["Finance meter register / October"],
          method: "interval_sum",
        },
        hosting_fees: {
          status: "authoritative",
          amounts: [{ value: "1500.50", currency: "USD" }],
          sources: ["Accrued fees / October"],
          method: "approved_accrual",
        },
        prior_year_comparison: { status: "unavailable", causes: ["first_reporting_year"] },
      },
      containers: [
        {
          siteId: 1n,
          containerId: 2n,
          metrics: { metered_energy_kwh: { status: "authoritative", value: "12345.6789", unit: "kWh" } },
        },
      ],
      orders: [
        {
          orderId: "Northstar order A",
          metrics: { metered_energy_kwh: { status: "authoritative", value: "12345.6789", unit: "kWh" } },
        },
      ],
      reconciliations: [
        {
          metric: "metered_energy_kwh",
          view: "containers",
          status: "authoritative",
          exactTotal: "123456789/10000",
          exactParts: "123456789/10000",
          matches: true,
        },
      ],
      interruptions: [
        {
          id: "incident",
          summary: "Planned electrical maintenance",
          category: "maintenance",
          start: { seconds: 1791000000n },
          end: { seconds: 1791003600n },
          rootCause: "Scheduled switchgear inspection",
          exclusionStatus: "not_claimed",
          sourceRef: "Approved maintenance log",
        },
      ],
    },
  });
export const summaryFixture = (complete = false, corrected = true) => {
  const report = reportFixture(complete, corrected);
  return create(ReportSummarySchema, {
    reportId: report.reportId,
    seriesId: report.seriesId,
    accountId: report.accountId,
    accountName: report.accountName,
    number: report.number,
    revision: report.revision,
    status: report.status,
    period: report.period,
    complete,
    correctionOf: report.correctionOf,
    correctionReason: report.correctionReason,
    issuedAt: report.issuedAt,
    deliveryStatus: "failed",
    deadlines: { contract: { date: "2026-11-20", status: "authoritative" } },
  });
};
export const setupFixture = () =>
  create(HostReportingSetupSchema, {
    owners: [{ userId: reportingStoryUser, username: "Casey" }],
    accountId: reportingStoryAccount,
    name: "Northstar Mining",
    canCreate: true,
    canAdminister: true,
    canReadRequests: true,
    canCreateRequests: true,
    authorities: [
      {
        userId: reportingStoryUser,
        username: "Casey",
        canCreate: true,
        allSites: true,
        revision: 1n,
      },
    ],
    input: {
      revision: 2n,
      sourceRef: "Signed input register / October",

      configuration: {
        timezone: "Africa/Nairobi",
        reportingYearMonth: 1,
        reportingYearDay: 1,
        contractRef: "Signed hosting agreement",
        deliveryChannel: "Agreed client portal",
        prerequisites: [
          { key: "G1", owner: "Commercial owner", ready: false, sourceRef: "Pending full definitions" },
          { key: "G2", owner: "Operations", ready: true, sourceRef: "Source certification" },
        ],
        sources: [{ id: "Site meter", metric: "meter", certified: true, sourceRef: "Meter certification" }],
      },
    },
  });
