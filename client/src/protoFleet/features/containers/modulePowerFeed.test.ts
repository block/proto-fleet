import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { modulePowerFeedView } from "./modulePowerFeed";
import {
  GetModulePowerFeedResponseSchema,
  PowerFeedMappingConfidence,
  PowerFeedState,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";

it("maps a live feed read without inventing values", () => {
  const view = modulePowerFeedView(
    create(GetModulePowerFeedResponseSchema, {
      mapping: {
        confidence: PowerFeedMappingConfidence.CONFIGURED,
        feedId: "pdu-2-outlet-05",
      },
      feed: { id: "pdu-2-outlet-05", state: PowerFeedState.DISABLED, overridden: true, voltageV: 0 },
      recoveryCycleKnown: true,
      recoveryCycle: {
        id: "c",
        phase: "holding",
        outletIds: ["pdu-2-outlet-05"],
        restoreAfter: timestampFromMs(1_000),
      },
    }),
  );
  expect(view.confidence).toBe("configured");
  expect(view.reading?.state).toBe("disabled");
  expect(view.reading?.voltageV).toBe(0);
  expect(view.reading?.powerW).toBeUndefined();
  expect(view.reading?.active).toBeUndefined();
  expect(view.recoveryCycle?.restoreAfter?.getTime()).toBe(1_000);
});

it("keeps an unreadable recovery cycle unknown and an unspecified state unknown", () => {
  const view = modulePowerFeedView(
    create(GetModulePowerFeedResponseSchema, {
      mapping: {
        confidence: PowerFeedMappingConfidence.AMBIGUOUS,
        reasons: ["feed_shared"],
        sharedWith: [{ tank: 1, module: 2 }],
      },
      feed: { id: "pdu-1-outlet-01" },
      recoveryCycleKnown: false,
    }),
  );
  expect(view.recoveryCycle).toBeUndefined();
  expect(view.reading?.state).toBe("unknown");
  expect(view.feedId).toBeUndefined();
  expect(view.sharedWith).toEqual([{ tank: 1, module: 2 }]);
  expect(
    modulePowerFeedView(create(GetModulePowerFeedResponseSchema, { recoveryCycleKnown: true })).recoveryCycle,
  ).toBeNull();
});
