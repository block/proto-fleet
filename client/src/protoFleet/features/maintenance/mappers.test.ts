import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { toInventoryInsights, toInventoryPart, toTicketDetail, toTicketItem } from "./mappers";
import { InventoryInsightsSchema, InventoryPartSchema } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import {
  RepairTicketDetailSchema,
  RepairTicketSchema,
  RepairTicketSummarySchema,
  TicketCategory,
  TicketStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

describe("maintenance mappers", () => {
  it("maps IDs, enums, and absent values into stable UI types", () => {
    const ticket = create(RepairTicketSchema, {
      id: 42n,
      ticketNumber: "TK-42",
      category: TicketCategory.MINER,
      status: TicketStatus.IN_PROGRESS,
    });
    const result = toTicketItem(create(RepairTicketSummarySchema, { ticket, commentCount: 2, partsCount: 1 }));
    expect(result).toMatchObject({
      id: "42",
      category: "miner",
      status: "in_progress",
      assigneeUserId: null,
      siteName: null,
      commentCount: 2,
    });
    expect(result.createdAt).toBeNull();
  });
  it("preserves the precise ticket revision for delete preconditions", () => {
    const updatedAt = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1788609600n, nanos: 123456000 };
    const ticket = create(RepairTicketSchema, { id: 42n, updatedAt });

    const result = toTicketItem(create(RepairTicketSummarySchema, { ticket }));

    expect(result.updatedAtSnapshot).toEqual(updatedAt);
  });
  it("derives availability and preserves the precise inventory revision", () => {
    const updatedAt = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1788609600n, nanos: 123456000 };
    const result = toInventoryPart(
      create(InventoryPartSchema, { id: 7n, onHand: 9, allocated: 4, reorderPoint: 5, updatedAt }),
    );
    expect(result).toMatchObject({ id: "7", available: 5, lowStock: true, siteId: null });
    expect(result.updatedAtSnapshot).toEqual(updatedAt);
  });
  it("preserves the precise ticket revision in detail", () => {
    const updatedAt = { $typeName: "google.protobuf.Timestamp" as const, seconds: 1788998400n, nanos: 123456000 };
    const result = toTicketDetail(
      create(RepairTicketDetailSchema, { ticket: create(RepairTicketSchema, { id: 42n, updatedAt }) }),
    );
    expect(result.updatedAtSnapshot).toEqual(updatedAt);
  });
  it("maps organization-wide inventory type facets", () => {
    const result = toInventoryInsights(create(InventoryInsightsSchema, { partTypes: ["cable", "fan"] }));
    expect(result.partTypes).toEqual(["cable", "fan"]);
  });
  it("rejects malformed summaries", () => {
    expect(() => toTicketItem(create(RepairTicketSummarySchema))).toThrow("missing its ticket");
  });
});
