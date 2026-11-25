import { beforeEach, describe, expect, it } from "vitest";
import type { FrigateUpdateRecord } from "../adapters/frigateServerUpdate";
import { readFrigateUpdate, readFrigateUpdates, removeFrigateUpdate, saveFrigateUpdate } from "./frigateUpdateStorage";

const record = (jobId: string, fields: Partial<FrigateUpdateRecord> = {}): FrigateUpdateRecord => ({
  jobId,
  idempotencyKey: `key-${jobId}`,
  targetVersion: "9.9.9",
  packageName: "frigate_9.9.9_linux_arm64.deb",
  instanceIds: ["ct1-a", "ct1-b"],
  fingerprints: {},
  step: "staging",
  ...fields,
});

describe("Frigate update recovery records", () => {
  beforeEach(() => localStorage.clear());

  it("keeps one record per job and releases only its own", () => {
    saveFrigateUpdate("device", record("b"));
    saveFrigateUpdate("device", record("a"));
    saveFrigateUpdate("device", record("a", { step: "starting", fingerprints: { "ct1-a": "fa" } }));
    saveFrigateUpdate("other", record("c"));
    expect(readFrigateUpdates("device").map((saved) => [saved.jobId, saved.step])).toEqual([
      ["a", "starting"],
      ["b", "staging"],
    ]);
    removeFrigateUpdate("device", "a");
    expect(readFrigateUpdate("device")).toEqual(record("b"));
  });

  it("skips malformed entries and entries saved under another job's key", () => {
    const prefix = "proto-os:frigate-update:device:";
    localStorage.setItem(`${prefix}broken`, "{");
    localStorage.setItem(`${prefix}unknown-step`, JSON.stringify(record("unknown-step", { step: "done" as never })));
    localStorage.setItem(`${prefix}moved`, JSON.stringify(record("elsewhere")));
    localStorage.setItem(`${prefix}no-instances`, JSON.stringify(record("no-instances", { instanceIds: [] })));
    saveFrigateUpdate("device", record("ok"));
    expect(readFrigateUpdates("device")).toEqual([record("ok")]);
  });
});
