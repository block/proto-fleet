import { describe, expect, it } from "vitest";

import { importReportingRows, reportingFactID, reportingImportBatches, reportingTemplate } from "./reportingInputs";

describe("reporting fact imports", () => {
  it("imports quoted CSV without losing exact decimals", () => {
    const imported = importReportingRows(
      "fees",
      'id,siteId,containerId,orderId,start,end,amount,currency,sourceRef\nf1,1,2,signed,2026-10-01T00:00:00Z,2026-11-01T00:00:00Z,123456789.123456789,USD,"Finance, approved"',
      "csv",
    );
    expect(imported[0].value.value).toMatchObject({
      amount: "123456789.123456789",
      sourceRef: "Finance, approved",
      siteId: 1n,
    });
  });
  it("rejects unknown columns and invalid typed JSON rather than silently dropping them", () => {
    expect(() => importReportingRows("fees", "id,amunt\nf1,25", "csv")).toThrow(/amunt/);
    expect(() => importReportingRows("meters", '[{"siteId":"wrong"}]', "json")).toThrow();
  });
  it("provides a fixed CSV schema including evidence references", () => {
    expect(reportingTemplate("meters").split("\n")[0]).toContain("sourceRef");
    expect(reportingTemplate("coverage").split("\n")[0]).toContain("commissionedAt");
  });
});

it("imports reviewed module mappings from CSV and JSON without changing board coverage", () => {
  const imported = importReportingRows(
    "moduleCoverage",
    "id,moduleId,siteId,containerId,orderId,start,end,boardCoverageIds,wholeModule,sourceRef\nm1,9,1,2,order,2026-10-01T00:00:00Z,2026-11-01T00:00:00Z,b1;b2;b3,true,reviewed",
    "csv",
  );
  expect(imported[0].value.value).toMatchObject({
    moduleId: 9n,
    boardCoverageIds: ["b1", "b2", "b3"],
    wholeModule: true,
  });
  const partial = importReportingRows(
    "moduleCoverage",
    '[{"id":"partial","boardCoverageIds":["b1"],"wholeModule":false}]',
    "json",
  );
  expect(partial[0].value.value).toMatchObject({ wholeModule: false });
  expect(() => importReportingRows("moduleCoverage", "wholeModule\nyes", "csv")).toThrow(/true or false/);
});

it("splits more than ten thousand rows into ordered bounded upserts without replacing unseen rows", () => {
  const facts = importReportingRows(
    "orders",
    JSON.stringify(Array.from({ length: 10001 }, (_, i) => ({ id: `order-${i}`, name: "Order" }))),
    "json",
  );
  const batches = reportingImportBatches("orders", facts);
  expect(batches.every((batch) => batch.length <= 200)).toBe(true);
  expect(batches.flat().map(reportingFactID)).toEqual(facts.map(reportingFactID));
  expect(batches.length).toBeGreaterThan(50);
});
it("rejects duplicate IDs before submitting an import", () => {
  expect(() => importReportingRows("orders", '[{"id":"same"},{"id":"same"}]', "json")).toThrow(/Duplicate row ID/);
});
it("shortens batches for encoded bytes, including HTML characters", () => {
  const facts = importReportingRows(
    "fees",
    JSON.stringify(Array.from({ length: 200 }, (_, i) => ({ id: `fee-${i}`, sourceRef: "<".repeat(1800) }))),
    "json",
  );
  const batches = reportingImportBatches("fees", facts);
  expect(batches.length).toBeGreaterThan(1);
  expect(batches.flat()).toHaveLength(200);
});
