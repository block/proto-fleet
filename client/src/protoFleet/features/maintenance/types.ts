export type TicketCategoryValue = "miner" | "infrastructure" | "unknown";
export type TicketStatusValue = "open" | "in_progress" | "on_hold" | "sent_to_vendor" | "completed" | "unknown";
export type TicketResolutionValue =
  "repaired" | "replaced" | "deferred" | "unrepairable" | "no_action_needed" | "unknown";
export type RepairLocationValue = "on_rack" | "repair_bench" | "unknown";
export type WarrantyStatusValue = "in_warranty" | "out_of_warranty" | "expiring_soon" | "unknown";

export type TicketItem = {
  id: string;
  ticketNumber: string;
  category: TicketCategoryValue;
  status: TicketStatusValue;
  urgent: boolean;
  component: string;
  diagnosis: string;
  minerIdentifier: string | null;
  assigneeUserId: string | null;
  assigneeName: string | null;
  siteId: string | null;
  siteName: string | null;
  buildingId: string | null;
  buildingName: string | null;
  rackId: string | null;
  rackLabel: string;
  zone: string;
  groupLabel: string;
  commentCount: number;
  partsCount: number;
  createdAt: Date | null;
  updatedAt: Date | null;
};

export type TicketCommentItem = {
  id: string;
  ticketId: string;
  userId: string;
  userName: string;
  text: string;
  createdAt: Date | null;
  authoredByCaller: boolean;
};

export type PartUsageItem = { inventoryPartId: string; partName: string; quantity: number };

export type TicketDetail = Omit<TicketItem, "commentCount" | "partsCount"> & {
  alertId: string | null;
  warrantyStatus: WarrantyStatusValue;
  resolution: TicketResolutionValue;
  repairLocation: RepairLocationValue;
  notes: string;
  dailyImpactUsd: number;
  rmaVendor: string | null;
  rmaTracking: string | null;
  rmaEta: Date | null;
  completedAt: Date | null;
  comments: TicketCommentItem[];
  partsUsed: PartUsageItem[];
};

export type TicketStats = {
  openCount: number;
  inProgressCount: number;
  onHoldCount: number;
  sentToVendorCount: number;
  overdueCount: number;
  urgentCount: number;
};

export type AssigneeOption = { id: string; username: string; roleName: string };
export type SiteOption = { id: string; name: string };

export type InventoryPartItem = {
  id: string;
  name: string;
  type: string;
  manufacturer: string;
  partNumber: string;
  siteId: string | null;
  siteName: string | null;
  onHand: number;
  allocated: number;
  available: number;
  reorderPoint: number;
  binLocation: string;
  lowStock: boolean;
  createdAt: Date | null;
  updatedAt: Date | null;
};

export type InventoryInsightsItem = {
  totalOnHand: number;
  totalAllocated: number;
  lowStockCount: number;
  sitesCount: number;
};
export type LoadState<T> = { data: T; loading: boolean; error: string | null };
