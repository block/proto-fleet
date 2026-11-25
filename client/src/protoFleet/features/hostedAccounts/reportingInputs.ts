import { create, fromJson, type JsonObject, type JsonValue, toJson } from "@bufbuild/protobuf";

import {
  type ReportingConfiguration,
  type ReportingFact,
  ReportingFactKind,
  ReportingFactMutationSchema,
  ReportingFactSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

/**
 * Whether a configuration records anything reviewed against a client agreement.
 * The timezone and reporting-year start are plain settings; the server requires
 * a source reference for everything else, using this same rule. Must stay in
 * sync with Configuration.ContractEvidence in
 * server/internal/domain/hostedaccounts/reporting/inputs.go.
 */
export const hasContractEvidence = (configuration?: ReportingConfiguration) =>
  !!configuration &&
  (!!configuration.schedule ||
    configuration.registers.length +
      configuration.holidays.length +
      configuration.prerequisites.length +
      configuration.sources.length +
      configuration.representatives.length >
      0 ||
    [configuration.contractRef, configuration.deliveryChannel, configuration.sharedAllocationRef].some(
      (value) => !!value.trim(),
    ));

export const reportingColumns = {
  orders: "id,name,start,end,sourceRef",
  coverage:
    "id,allocationIntervalId,kind,assetId,siteId,containerId,orderId,start,end,commissionedAt,capacityThs,replacementOf,sourceRef",
  moduleCoverage: "id,moduleId,siteId,containerId,orderId,start,end,boardCoverageIds,wholeModule,sourceRef",
  meters: "id,meterId,sourceId,siteId,containerId,orderId,start,end,kwh,quality,sourceRef,allocationFraction",
  fees: "id,siteId,containerId,orderId,start,end,amount,currency,sourceRef",
  incidents: "id,siteId,containerId,orderId,start,end,asOf,category,summary,rootCause,exclusionClause,sourceRef",
  hashrates: "id,assetId,kind,siteId,containerId,orderId,sourceId,start,end,hashrateHs,complete,sourceRef",
  poolObservations: "id,assetId,kind,sourceId,sourceAt,receivedAt,state,reason,sourceRef",
  obligations:
    "id,episodeId,assetId,kind,siteId,containerId,orderId,start,end,asOf,severity,ticketRef,ticketAt,submittedAt,receivedAt,diagnosedAt,respondedAt,approvedAt,deadline,restoredAt,closedAt,qualified,sourceRef",
  externalResults: "id,metric,siteId,containerId,orderId,start,end,value,unit,sourceRef,calculationRef",
} as const;
export type ReportingRegister = keyof typeof reportingColumns;
export const reportingRegisters = Object.keys(reportingColumns) as ReportingRegister[];
export const reportingKinds: Record<ReportingRegister, ReportingFactKind> = {
  orders: ReportingFactKind.ORDER,
  coverage: ReportingFactKind.COVERAGE,
  moduleCoverage: ReportingFactKind.MODULE_COVERAGE,
  meters: ReportingFactKind.METER,
  fees: ReportingFactKind.FEE,
  incidents: ReportingFactKind.INCIDENT,
  hashrates: ReportingFactKind.HASHRATE,
  poolObservations: ReportingFactKind.POOL_OBSERVATION,
  obligations: ReportingFactKind.OBLIGATION,
  externalResults: ReportingFactKind.EXTERNAL_RESULT,
};
const reportingCases: Record<ReportingRegister, NonNullable<ReportingFact["value"]["case"]>> = {
  orders: "order",
  coverage: "coverage",
  moduleCoverage: "moduleCoverage",
  meters: "meter",
  fees: "fee",
  incidents: "incident",
  hashrates: "hashrate",
  poolObservations: "poolObservation",
  obligations: "obligation",
  externalResults: "externalResult",
};
export const MAX_REPORTING_IMPORT_BYTES = 5 * 1024 * 1024;
export const reportingFactID = (fact: ReportingFact) => fact.value.value?.id ?? "";
export const reportingFactJSON = (fact: ReportingFact): Record<string, JsonValue> => {
  const value = toJson(ReportingFactSchema, fact) as JsonObject;
  return (fact.value.case ? value[fact.value.case] : {}) as Record<string, JsonValue>;
};
export const reportingRow = (register: ReportingRegister, value: JsonObject) =>
  fromJson(ReportingFactSchema, { [reportingCases[register]]: value });
export const reportingUpsert = (register: ReportingRegister, fact: ReportingFact) =>
  create(ReportingFactMutationSchema, {
    kind: reportingKinds[register],
    id: reportingFactID(fact),
    change: { case: "upsert", value: fact },
  });

// Allow for omitted Go defaults and zero timestamps as well as JSON HTML escaping.
export const reportingImportBatches = (register: ReportingRegister, facts: ReportingFact[]) => {
  const batches: ReportingFact[][] = [];
  let batch: ReportingFact[] = [];
  let bytes = 2;
  const encoder = new TextEncoder();
  for (const fact of facts) {
    const json = JSON.stringify({
      kind: reportingCases[register],
      id: reportingFactID(fact),
      payload: reportingFactJSON(fact),
    }).replace(/[<>&\u2028\u2029]/g, (ch) => `\\u${ch.charCodeAt(0).toString(16).padStart(4, "0")}`);
    const cost = encoder.encode(json).length + reportingColumns[register].split(",").length * 96 + 64;
    if (cost + 2 > 1 << 20)
      throw new Error(`Row ${reportingFactID(fact)} exceeds one request. Shorten the row before importing.`);
    if (batch.length && (batch.length === 200 || bytes + cost + 1 > 1 << 20)) {
      batches.push(batch);
      batch = [];
      bytes = 2;
    }
    batch.push(fact);
    bytes += cost + 1;
  }
  if (batch.length) batches.push(batch);
  return batches;
};
export const reportingFieldValue = (name: string, value: string): string | number | boolean | string[] => {
  if (name === "boardCoverageIds") return value.split(";").map((id) => id.trim());
  if (["complete", "qualified", "wholeModule"].includes(name)) return value === "true";
  if (name === "kind") return Number(value);
  return value;
};
export const reportingTemplate = (register: ReportingRegister) => `${reportingColumns[register]}\n`;

const csvRows = (text: string) => {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === '"') {
      if (quoted && text[i + 1] === '"') {
        cell += '"';
        i++;
      } else if (!quoted && cell) throw new Error("Quote must begin a CSV field.");
      else quoted = !quoted;
    } else if (!quoted && (ch === "," || ch === "\n" || ch === "\r")) {
      row.push(cell);
      cell = "";
      if (ch !== ",") {
        if (row.some(Boolean)) rows.push(row);
        row = [];
        if (ch === "\r" && text[i + 1] === "\n") i++;
      }
    } else cell += ch;
  }
  if (quoted) throw new Error("Unclosed CSV quote.");
  row.push(cell);
  if (row.some(Boolean)) rows.push(row);
  return rows;
};

export const importReportingRows = (register: ReportingRegister, text: string, format: "csv" | "json") => {
  let values: unknown;
  if (format === "json") values = JSON.parse(text);
  else {
    const [header, ...rows] = csvRows(text.replace(/^\uFEFF/, ""));
    if (!header?.length) throw new Error("CSV needs a header row.");
    const allowed = reportingColumns[register].split(",");
    for (const name of header) if (!allowed.includes(name)) throw new Error(`Unknown column: ${name}`);
    if (new Set(header).size !== header.length) throw new Error("CSV column names must be unique.");
    values = rows.map((row, index) => {
      if (row.length !== header.length) throw new Error(`Row ${index + 2} has the wrong number of fields.`);
      return Object.fromEntries(
        header.flatMap<[string, string | number | boolean | string[]]>((name, column) => {
          const value = row[column];
          if (!value) return [];
          if (["complete", "qualified", "wholeModule"].includes(name)) {
            if (!["true", "false"].includes(value)) throw new Error(`Row ${index + 2}: ${name} must be true or false.`);
          }
          return [[name, reportingFieldValue(name, value)]];
        }),
      );
    });
  }
  if (!Array.isArray(values)) throw new Error("JSON must contain an array of fact rows.");
  const seen = new Set<string>();
  const parsed = values.map((value) => {
    const fact = reportingRow(register, value as JsonObject);
    const id = reportingFactID(fact);
    if (!id.trim()) throw new Error("Every row needs an ID.");
    if (seen.has(id)) throw new Error(`Duplicate row ID: ${id}`);
    seen.add(id);
    return fact;
  });
  reportingImportBatches(register, parsed);
  return parsed;
};
