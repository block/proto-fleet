import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";

import type {
  InventoryInsightsItem,
  InventoryPartItem,
  RepairLocationValue,
  TicketCategoryValue,
  TicketDetail,
  TicketItem,
  TicketResolutionValue,
  TicketStatusValue,
  WarrantyStatusValue,
} from "./types";
import type { InventoryInsights, InventoryPart } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import {
  RepairLocation,
  type RepairTicket,
  type RepairTicketDetail,
  type RepairTicketSummary,
  TicketCategory,
  TicketResolution,
  TicketStatus,
  WarrantyStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

const dateOrNull = (value?: Timestamp): Date | null => (value ? timestampDate(value) : null);
const stringOrNull = (value?: string): string | null => value || null;
const idOrNull = (value?: bigint): string | null => (value === undefined ? null : value.toString());

export const toTicketCategory = (value: TicketCategory): TicketCategoryValue => {
  if (value === TicketCategory.MINER) return "miner";
  if (value === TicketCategory.INFRASTRUCTURE) return "infrastructure";
  return "unknown";
};

export const toTicketStatus = (value: TicketStatus): TicketStatusValue => {
  if (value === TicketStatus.OPEN) return "open";
  if (value === TicketStatus.IN_PROGRESS) return "in_progress";
  if (value === TicketStatus.ON_HOLD) return "on_hold";
  if (value === TicketStatus.SENT_TO_VENDOR) return "sent_to_vendor";
  if (value === TicketStatus.COMPLETED) return "completed";
  return "unknown";
};

const toResolution = (value: TicketResolution): TicketResolutionValue => {
  if (value === TicketResolution.REPAIRED) return "repaired";
  if (value === TicketResolution.REPLACED) return "replaced";
  if (value === TicketResolution.DEFERRED) return "deferred";
  if (value === TicketResolution.UNREPAIRABLE) return "unrepairable";
  if (value === TicketResolution.NO_ACTION_NEEDED) return "no_action_needed";
  return "unknown";
};

const toRepairLocation = (value: RepairLocation): RepairLocationValue => {
  if (value === RepairLocation.ON_RACK) return "on_rack";
  if (value === RepairLocation.REPAIR_BENCH) return "repair_bench";
  return "unknown";
};

const toWarranty = (value: WarrantyStatus): WarrantyStatusValue => {
  if (value === WarrantyStatus.IN_WARRANTY) return "in_warranty";
  if (value === WarrantyStatus.OUT_OF_WARRANTY) return "out_of_warranty";
  if (value === WarrantyStatus.EXPIRING_SOON) return "expiring_soon";
  return "unknown";
};

const ticketBase = (ticket: RepairTicket) => ({
  id: ticket.id.toString(),
  ticketNumber: ticket.ticketNumber,
  category: toTicketCategory(ticket.category),
  status: toTicketStatus(ticket.status),
  urgent: ticket.urgent,
  component: ticket.component,
  diagnosis: ticket.diagnosis,
  minerIdentifier: stringOrNull(ticket.minerIdentifier),
  assigneeUserId: idOrNull(ticket.assigneeUserId),
  assigneeName: stringOrNull(ticket.assigneeName),
  siteId: idOrNull(ticket.siteId),
  siteName: stringOrNull(ticket.siteName),
  buildingId: idOrNull(ticket.buildingId),
  buildingName: stringOrNull(ticket.buildingName),
  rackId: idOrNull(ticket.rackId),
  rackLabel: ticket.rackLabel,
  zone: ticket.zone,
  groupLabel: ticket.groupLabel,
  createdAt: dateOrNull(ticket.createdAt),
  updatedAt: dateOrNull(ticket.updatedAt),
});

export function toTicketItem(summary: RepairTicketSummary): TicketItem {
  if (!summary.ticket) throw new Error("Ticket summary is missing its ticket");
  return { ...ticketBase(summary.ticket), commentCount: summary.commentCount, partsCount: summary.partsCount };
}

export function toTicketDetail(detail: RepairTicketDetail): TicketDetail {
  if (!detail.ticket) throw new Error("Ticket detail is missing its ticket");
  const ticket = detail.ticket;
  return {
    ...ticketBase(ticket),
    alertId: stringOrNull(ticket.alertId),
    warrantyStatus: toWarranty(ticket.warrantyStatus),
    resolution: toResolution(ticket.resolution),
    repairLocation: toRepairLocation(ticket.repairLocation),
    notes: ticket.notes,
    dailyImpactUsd: ticket.dailyImpactUsd,
    rmaVendor: stringOrNull(ticket.rmaVendor),
    rmaTracking: stringOrNull(ticket.rmaTracking),
    rmaEta: dateOrNull(ticket.rmaEta),
    completedAt: dateOrNull(ticket.completedAt),
    comments: detail.comments.map((comment) => ({
      id: comment.id.toString(),
      ticketId: comment.ticketId.toString(),
      userId: comment.userId.toString(),
      userName: comment.userName,
      text: comment.text,
      createdAt: dateOrNull(comment.createdAt),
      authoredByCaller: comment.authoredByCaller,
    })),
    partsUsed: detail.partsUsed.map((part) => ({
      inventoryPartId: part.inventoryPartId.toString(),
      partName: part.partName,
      quantity: part.quantity,
    })),
  };
}

export function toInventoryPart(part: InventoryPart): InventoryPartItem {
  const available = part.onHand - part.allocated;
  return {
    id: part.id.toString(),
    name: part.name,
    type: part.type,
    manufacturer: part.manufacturer,
    partNumber: part.partNumber,
    siteId: idOrNull(part.siteId),
    siteName: stringOrNull(part.siteName),
    onHand: part.onHand,
    allocated: part.allocated,
    available,
    reorderPoint: part.reorderPoint,
    binLocation: part.binLocation,
    lowStock: available <= part.reorderPoint,
    createdAt: dateOrNull(part.createdAt),
    updatedAt: dateOrNull(part.updatedAt),
  };
}

export const toInventoryInsights = (value: InventoryInsights): InventoryInsightsItem => ({
  totalOnHand: value.totalOnHand,
  totalAllocated: value.totalAllocated,
  lowStockCount: value.lowStockCount,
  sitesCount: value.sitesCount,
});
