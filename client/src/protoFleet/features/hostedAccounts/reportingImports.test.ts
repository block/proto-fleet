import { describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { runReportingImport } from "./reportingImports";
import { importReportingRows } from "./reportingInputs";
const rows = (n: number) =>
  importReportingRows("orders", JSON.stringify(Array.from({ length: n }, (_, i) => ({ id: `row-${i}` }))), "json");
describe("bounded reporting imports", () => {
  it("advances only from each confirmed revision", async () => {
    const revisions: bigint[] = [];
    const submit = vi.fn(async (_batch, revision: bigint) => {
      revisions.push(revision);
      return revision + 1n;
    });
    const result = await runReportingImport(
      "orders",
      rows(401),
      8n,
      submit,
      () => false,
      () => {},
    );
    expect(revisions).toEqual([8n, 9n, 10n]);
    expect(result).toMatchObject({ status: "complete", confirmedRows: 401, revision: 11n, pending: [] });
  });
  it("cancels later batches while counting the in-flight batch's confirmed outcome", async () => {
    let cancel = false;
    const submit = vi.fn(async (_batch, revision: bigint) => {
      if (submit.mock.calls.length === 3) cancel = true;
      return revision + 1n;
    });
    const result = await runReportingImport(
      "orders",
      rows(601),
      8n,
      submit,
      () => cancel,
      () => {},
    );
    expect(submit).toHaveBeenCalledTimes(3);
    expect(result).toMatchObject({ status: "cancelled", confirmedRows: 600, revision: 11n, pending: [] });
  });
  it("reports the first two confirmed batches when a third conflicts", async () => {
    const submit = vi.fn(async (_batch, revision: bigint) => {
      if (submit.mock.calls.length === 3) throw new ConnectError("Inputs changed", Code.Aborted);
      return revision + 1n;
    });
    const result = await runReportingImport(
      "orders",
      rows(601),
      8n,
      submit,
      () => false,
      () => {},
    );
    expect(submit).toHaveBeenCalledTimes(3);
    expect(result).toMatchObject({ status: "failed", confirmedRows: 400, revision: 10n, pending: [] });
  });
  it("retains the last submitted batch when its committed response is lost, without retrying", async () => {
    const submit = vi.fn(async (_batch, revision: bigint) => {
      if (submit.mock.calls.length === 3) throw new ConnectError("Connection lost after commit", Code.Unavailable);
      return revision + 1n;
    });
    const result = await runReportingImport(
      "orders",
      rows(601),
      8n,
      submit,
      () => false,
      () => {},
    );
    expect(submit).toHaveBeenCalledTimes(3);
    expect(result).toMatchObject({ status: "unknown", confirmedRows: 400, revision: 10n });
    expect(result.pending).toHaveLength(200);
  });
});
