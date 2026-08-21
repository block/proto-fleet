import { describe, expect, it } from "vitest";

import {
  getV2AuthorityKeyValidationError,
  urlValidationErrors,
  v2AuthorityKeyValidationErrors,
  validateURLScheme,
} from "./constants";

describe("ProtoOS standalone Stratum V2 validation", () => {
  it("accepts host-style URLs with protocol-default ports", () => {
    expect(
      validateURLScheme("stratum2+tcp://pool.example.com", {
        standaloneV2AuthorityKey: true,
      }),
    ).toBeUndefined();
  });

  it("rejects legacy authority-key URL paths", () => {
    expect(
      validateURLScheme("stratum2+tcp://pool.example.com:3336/authority-key", {
        standaloneV2AuthorityKey: true,
      }),
    ).toBe(urlValidationErrors.v2PathNotSupported);
  });

  it("requires standalone keys only for remote SV2 pools", () => {
    expect(getV2AuthorityKeyValidationError("stratum2+tcp://pool.example.com", "")).toBe(
      v2AuthorityKeyValidationErrors.required,
    );
    expect(getV2AuthorityKeyValidationError("stratum2+tcp://localhost", "")).toBeUndefined();
    expect(getV2AuthorityKeyValidationError("stratum2+tcp://127.0.0.1", "")).toBeUndefined();
    expect(getV2AuthorityKeyValidationError("stratum2+tcp://[::1]", "")).toBeUndefined();
    expect(getV2AuthorityKeyValidationError("stratum+tcp://pool.example.com", "")).toBeUndefined();
  });
});

describe("Fleet legacy Stratum V2 validation", () => {
  it("continues to require an explicit port and path authority key", () => {
    expect(validateURLScheme("stratum2+tcp://pool.example.com")).toBe(urlValidationErrors.v2MissingPort);
    expect(validateURLScheme("stratum2+tcp://pool.example.com:3336")).toBe(urlValidationErrors.v2MissingPubkey);
    expect(validateURLScheme("stratum2+tcp://pool.example.com:3336/authority-key")).toBeUndefined();
  });
});
