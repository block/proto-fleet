import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { canPreservePassword, poolSlotInput, type SlotDraft, validWorkerTemplate } from "./poolDraft";
import { PoolRevisionSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

const draft: SlotDraft = {
  url: "stratum+tcp://pool.example:3333",
  username: "payout",
  workerTemplate: "{miner_name}",
  password: "",
  preservePassword: true,
};
describe("hosted pool revision input", () => {
  it("omits an unchanged password and distinguishes explicit clearing", () => {
    expect(poolSlotInput(draft)).not.toHaveProperty("password");
    expect(poolSlotInput({ ...draft, preservePassword: false })).toHaveProperty("password", "");
  });
  it("retains a secret only at its original priority, destination and username", () => {
    const original = create(PoolRevisionSchema, {
      slots: [
        { url: draft.url, username: draft.username, hasPassword: true },
        { url: "stratum+tcp://backup.example:3333", username: "backup", hasPassword: true },
      ],
    });
    expect(canPreservePassword(draft, original, 0)).toBe(true);
    expect(canPreservePassword({ ...draft, username: "new-owner" }, original, 0)).toBe(false);
    expect(canPreservePassword(draft, original, 1)).toBe(false);
  });
  it("allows only supported worker placeholders", () => {
    expect(validWorkerTemplate("{miner_id}-{miner_name}")).toBe(true);
    expect(validWorkerTemplate("")).toBe(true);
    expect(validWorkerTemplate("{serial_number}")).toBe(false);
  });
});
