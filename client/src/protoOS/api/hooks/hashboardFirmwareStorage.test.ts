import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HashboardFirmwareAttempt } from "../adapters/hashboardFirmware";
import { readAttempt, readAttempts, removeAttempt, saveAttempt } from "./hashboardFirmwareStorage";

const attempt = (taskId: string): HashboardFirmwareAttempt => ({
  taskId,
  boards: [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Board 1" }],
  version: "9.0.0",
  build: "prod",
});

describe("Hashboard firmware recovery records", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it.each([
    ["accepted", "refused"],
    ["refused", "accepted"],
  ])("keeps the accepted record when two tabs claim at the same moment (%s saves first)", (first, second) => {
    // Both tabs look before either saves: the check localStorage cannot make atomic across tabs.
    expect(readAttempts("device")).toEqual([]);
    expect(readAttempts("device")).toEqual([]);
    saveAttempt("device", attempt(first));
    saveAttempt("device", attempt(second));
    expect(readAttempts("device")).toEqual([attempt("accepted"), attempt("refused")]);
    // The controller accepts one Start and refuses the other, whose tab releases only its own record.
    removeAttempt("device", "refused");
    expect(readAttempts("device")).toEqual([attempt("accepted")]);
    expect(readAttempt("device")).toEqual(attempt("accepted"));
  });

  it("rewrites and releases a record without touching another task's", () => {
    saveAttempt("device", attempt("a"));
    saveAttempt("device", attempt("b"));
    saveAttempt("device", { ...attempt("a"), cancel: "requested" });
    expect(readAttempts("device")).toEqual([{ ...attempt("a"), cancel: "requested" }, attempt("b")]);
    removeAttempt("device", "a");
    removeAttempt("device", "a");
    expect(readAttempts("device")).toEqual([attempt("b")]);
  });

  it("reads only this device's well-formed records", () => {
    saveAttempt("device", attempt("task"));
    // A device ID that extends this one must not be read as this device's record.
    saveAttempt("device:other", attempt("task"));
    localStorage.setItem("proto-os:hashboard-firmware:device:broken", "{");
    localStorage.setItem("proto-os:hashboard-firmware:device:mismatch", JSON.stringify(attempt("elsewhere")));
    localStorage.setItem(
      "proto-os:hashboard-firmware:device:empty",
      JSON.stringify({ ...attempt("empty"), boards: [] }),
    );
    localStorage.setItem("unrelated", "value");
    expect(readAttempts("device")).toEqual([attempt("task")]);
    expect(readAttempts("device:other")).toEqual([attempt("task")]);
    expect(readAttempt("unknown")).toBeUndefined();
  });

  it("treats inaccessible storage as no saved record, and a failed save as an error", () => {
    saveAttempt("device", attempt("task"));
    vi.spyOn(Storage.prototype, "key").mockImplementation(() => {
      throw new Error("Storage is disabled");
    });
    expect(readAttempts("device")).toEqual([]);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage is full");
    });
    expect(() => saveAttempt("device", attempt("next"))).toThrow("Storage is full");
  });
});
