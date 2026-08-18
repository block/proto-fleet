import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import { timestampToIsoString } from "@/protoFleet/api/timestamps";

describe("timestampToIsoString", () => {
  it("preserves the existing millisecond flooring behavior", () => {
    expect(timestampToIsoString(create(TimestampSchema, { seconds: 1n, nanos: 1_999_999 }))).toBe(
      "1970-01-01T00:00:01.001Z",
    );
  });

  it("returns undefined for absent and invalid timestamps", () => {
    expect(timestampToIsoString()).toBeUndefined();
    expect(timestampToIsoString(create(TimestampSchema, { seconds: 8_640_000_000_001n }))).toBeUndefined();
  });
});
