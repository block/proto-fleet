import { beforeEach, expect, test } from "vitest";
import {
  isMappingAttemptKey,
  readMappingAttempt,
  readMappingAttempts,
  removeMappingAttempt,
  saveMappingAttempt,
} from "./attemptStorage";

beforeEach(() => localStorage.clear());

test("keeps attempts and devices independent when records are updated or removed", () => {
  saveMappingAttempt("ct1", { token: "accepted" });
  saveMappingAttempt("ct1", { token: "refused" });
  saveMappingAttempt("other", { token: "accepted", runId: "other-run" });
  saveMappingAttempt("ct1", { token: "accepted", runId: "accepted-run" });
  removeMappingAttempt("ct1", "refused");
  expect(readMappingAttempts("ct1")).toEqual([{ token: "accepted", runId: "accepted-run" }]);
  expect(readMappingAttempts("other")).toEqual([{ token: "accepted", runId: "other-run" }]);
});

test("prefers known run results on reload and retains a selected attempt until it is removed", () => {
  saveMappingAttempt("ct1", { token: "a-unknown" });
  saveMappingAttempt("ct1", { token: "z-known", runId: "run" });
  expect(readMappingAttempt("ct1")?.token).toBe("z-known");
  expect(readMappingAttempt("ct1", "a-unknown")?.token).toBe("a-unknown");
  removeMappingAttempt("ct1", "z-known");
  expect(readMappingAttempt("ct1", "z-known")?.token).toBe("a-unknown");
});

test("skips malformed records and records whose token does not match their key", () => {
  localStorage.setItem("proto-os:feed-mapping:ct1:broken", "{");
  localStorage.setItem("proto-os:feed-mapping:ct1:wrong", JSON.stringify({ token: "different" }));
  saveMappingAttempt("ct1", { token: "valid", runId: "run" });
  expect(readMappingAttempts("ct1")).toEqual([{ token: "valid", runId: "run" }]);
});

test("matches this device's storage events and storage.clear", () => {
  expect(isMappingAttemptKey("a:b", "proto-os:feed-mapping:a%3Ab:token")).toBe(true);
  expect(isMappingAttemptKey("a", "proto-os:feed-mapping:a%3Ab:token")).toBe(false);
  expect(isMappingAttemptKey("a", null)).toBe(true);
});
