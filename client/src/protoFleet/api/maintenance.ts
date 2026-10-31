import { useCallback } from "react";
import { type Timestamp, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import { maintenanceClient } from "@/protoFleet/api/clients";
import type {
  Assignee,
  RepairTicket,
  RepairTicketDetail,
  RepairTicketSummary,
  TicketAsset,
  TicketAssetSelection,
  TicketComment,
  TicketFilter,
  UpdateTicketTargetRequest,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import {
  RepairLocation,
  SortDirection,
  TicketCategory,
  TicketResolution,
  TicketSortField,
  TicketStatus,
  WarrantyStatus,
} from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

type Callbacks<T> = {
  signal?: AbortSignal;
  onSuccess?: (value: T) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
};

export type ListTicketsProps = Callbacks<{
  tickets: RepairTicketSummary[];
  nextPageToken: string;
  totalCount: number;
}> & {
  filter?: Partial<TicketFilter>;
  sortField?: TicketSortField;
  sortDirection?: SortDirection;
  pageSize?: number;
  pageToken?: string;
};

export type CreateTicketProps = Callbacks<RepairTicket | undefined> & {
  category: TicketCategory;
  component: string;
  description: string;
  targets?: Omit<TicketAssetSelection, "$typeName">[];
  clientAccountIds?: string[];
  shareWithClients?: boolean;
  idempotencyKey: string;
  diagnosis?: string;
  urgent?: boolean;
  minerIdentifier?: string;
  alertId?: string;
  assigneeUserId?: bigint;
  warrantyStatus?: WarrantyStatus;
  siteId?: bigint;
  buildingId?: bigint;
  zone?: string;
  rackId?: bigint;
  rackLabel?: string;
  groupLabel?: string;
  notes?: string;
};

export type PartSelection = { inventoryPartId: bigint; partName: string; quantity: number };
export type UpdateTicketProps = Callbacks<RepairTicket | undefined> & {
  id: bigint;
  status?: TicketStatus;
  expectedUpdatedAt: Timestamp;
  urgent?: boolean;
  assigneeUserId?: bigint;
  clearAssignee?: boolean;
  component?: string;
  diagnosis?: string;
  warrantyStatus?: WarrantyStatus;
  resolution?: TicketResolution;
  resolutionNote?: string;
  repairLocation?: RepairLocation;
  partsSelection?: PartSelection[];
  affectedBoards?: Omit<TicketAssetSelection, "$typeName">[];
  notes?: string;
  rmaVendor?: string;
  rmaTracking?: string;
  rmaEta?: Date;
  clearRmaEta?: boolean;
};

export type TicketVersion = { ticketId: bigint; updatedAt: Timestamp };

export type BulkTicketMutation =
  | { case: "assignToUserId"; value: bigint }
  | { case: "setStatus"; value: TicketStatus }
  | { case: "markUrgent"; value: boolean }
  | {
      case: "bulkClose";
      value: {
        resolution: TicketResolution;
        repairLocation: RepairLocation;
        resolutionNote: string;
      };
    }
  | { case: undefined };

const defaultFilter = (filter?: Partial<TicketFilter>) => ({
  statuses: filter?.statuses ?? [],
  categories: filter?.categories ?? [],
  siteIds: filter?.siteIds ?? [],
  buildingIds: filter?.buildingIds ?? [],
  rackIds: filter?.rackIds ?? [],
  groupLabels: filter?.groupLabels ?? [],
  assigneeUserId: filter?.assigneeUserId,
  urgentOnly: filter?.urgentOnly ?? false,
  searchQuery: filter?.searchQuery ?? "",
  excludeCompleted: filter?.excludeCompleted ?? false,
  overdueOnly: filter?.overdueOnly ?? false,
});

export const useMaintenanceApi = () => {
  const { handleAuthErrors } = useAuthErrors();
  const report = useCallback(
    (error: unknown, signal: AbortSignal | undefined, onError?: (message: string) => void) => {
      if (isAbortError(error, signal)) return;
      handleAuthErrors({ error, onError: (value: unknown) => onError?.(getErrorMessage(value)) });
    },
    [handleAuthErrors],
  );

  const listTickets = useCallback(
    async ({
      signal,
      onSuccess,
      onError,
      onFinally,
      filter,
      sortField = TicketSortField.UNSPECIFIED,
      sortDirection = SortDirection.UNSPECIFIED,
      pageSize = 0,
      pageToken = "",
    }: ListTicketsProps) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.listRepairTickets(
          { filter: defaultFilter(filter), sortField, sortDirection, pageSize, pageToken },
          { signal },
        );
        if (!signal?.aborted)
          onSuccess?.({
            tickets: response.tickets,
            nextPageToken: response.nextPageToken,
            totalCount: response.totalCount,
          });
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const getTicket = useCallback(
    async ({
      id,
      signal,
      onSuccess,
      onError,
      onFinally,
      onNotFound,
    }: Callbacks<RepairTicketDetail | undefined> & { id: bigint; onNotFound?: () => void }) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.getRepairTicket({ id }, { signal });
        if (!signal?.aborted) onSuccess?.(response.detail);
      } catch (error) {
        if (!signal?.aborted && error instanceof ConnectError && error.code === Code.NotFound) onNotFound?.();
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const createTicket = useCallback(
    async ({
      signal,
      onSuccess,
      onError,
      onFinally,
      category,
      component,
      description,
      targets = [],
      clientAccountIds = [],
      shareWithClients = false,
      idempotencyKey,
      diagnosis = "",
      urgent = false,
      minerIdentifier,
      alertId,
      assigneeUserId,
      warrantyStatus = WarrantyStatus.UNSPECIFIED,
      siteId,
      buildingId,
      zone = "",
      rackId,
      rackLabel = "",
      groupLabel = "",
      notes = "",
    }: CreateTicketProps) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.createRepairTicket(
          {
            category,
            component,
            description,
            targets,
            clientAccountIds,
            shareWithClients,
            idempotencyKey,
            diagnosis,
            urgent,
            minerIdentifier,
            alertId,
            assigneeUserId,
            warrantyStatus,
            siteId,
            buildingId,
            zone,
            rackId,
            rackLabel,
            groupLabel,
            notes,
          },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.ticket);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const updateTicket = useCallback(
    async ({
      signal,
      onSuccess,
      onError,
      onFinally,
      id,
      partsSelection,
      clearAssignee = false,
      ...fields
    }: UpdateTicketProps) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.updateRepairTicket(
          {
            id,
            clearAssignee,
            ...fields,
            rmaEta: fields.rmaEta ? timestampFromDate(fields.rmaEta) : undefined,
            partsSelection: partsSelection === undefined ? undefined : { parts: partsSelection },
          },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.ticket);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const bulkUpdate = useCallback(
    async ({
      ticketIds,
      mutation,
      clearAssignee = false,
      expectedVersions,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<number> & {
      ticketIds: bigint[];
      mutation: BulkTicketMutation;
      clearAssignee?: boolean;
      expectedVersions: TicketVersion[];
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.bulkUpdateRepairTickets(
          {
            ticketIds,
            mutation,
            clearAssignee,
            expectedVersions,
          },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.updatedCount);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const getStats = useCallback(
    async ({
      filter,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<Awaited<ReturnType<typeof maintenanceClient.getTicketStats>>> & {
      filter?: Partial<TicketFilter>;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.getTicketStats({ filter: defaultFilter(filter) }, { signal });
        if (!signal?.aborted) onSuccess?.(response);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const listAssignees = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: Callbacks<Assignee[]>) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.listAssignees({}, { signal });
        if (!signal?.aborted) onSuccess?.(response.assignees);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const listCompleted = useCallback(
    async ({
      componentFilter,
      assigneeUserIdFilter,
      sortField = TicketSortField.UNSPECIFIED,
      sortDirection = SortDirection.UNSPECIFIED,
      pageSize = 0,
      pageToken = "",
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<{
      tickets: RepairTicketSummary[];
      nextPageToken: string;
      totalCount: number;
      assigneeFacets: Assignee[];
    }> & {
      componentFilter?: string;
      assigneeUserIdFilter?: bigint;
      sortField?: TicketSortField;
      sortDirection?: SortDirection;
      pageSize?: number;
      pageToken?: string;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.listCompletedTickets(
          { componentFilter, assigneeUserIdFilter, sortField, sortDirection, pageSize, pageToken },
          { signal },
        );
        if (!signal?.aborted)
          onSuccess?.({
            tickets: response.tickets,
            nextPageToken: response.nextPageToken,
            totalCount: response.totalCount,
            assigneeFacets: response.assigneeFacets,
          });
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const createComment = useCallback(
    async ({
      ticketId,
      text,
      idempotencyKey,
      internal = true,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<TicketComment | undefined> & {
      ticketId: bigint;
      text: string;
      idempotencyKey: string;
      internal?: boolean;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await maintenanceClient.createTicketComment(
          { ticketId, text, idempotencyKey, internal },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.comment);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const deleteComment = useCallback(
    async ({ commentId, signal, onSuccess, onError, onFinally }: Callbacks<void> & { commentId: bigint }) => {
      try {
        if (signal?.aborted) return;
        await maintenanceClient.deleteTicketComment({ id: commentId }, { signal });
        if (!signal?.aborted) onSuccess?.();
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const listTicketAssets = useCallback(
    async ({
      siteIds = [],
      ticketId,
      kind,
      search = "",
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<TicketAsset[]> & {
      siteIds?: bigint[];
      ticketId?: bigint;
      kind?: TicketCategory;
      search?: string;
    }) => {
      try {
        const response = await maintenanceClient.listTicketAssetOptions(
          { siteIds, ticketId, kind, search },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.assets);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const updateTarget = useCallback(
    async ({
      signal,
      onSuccess,
      onError,
      onFinally,
      ...input
    }: Callbacks<RepairTicket | undefined> & Omit<UpdateTicketTargetRequest, "$typeName">) => {
      try {
        const response = await maintenanceClient.updateTicketTarget(input, { signal });
        if (!signal?.aborted) onSuccess?.(response.ticket);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const shareTicket = useCallback(
    async ({
      ticketId,
      expectedUpdatedAt,
      signal,
      onSuccess,
      onError,
    }: Callbacks<void> & { ticketId: bigint; expectedUpdatedAt: Timestamp }) => {
      try {
        await maintenanceClient.shareRepairTicket({ ticketId, expectedUpdatedAt }, { signal });
        onSuccess?.();
      } catch (error) {
        report(error, signal, onError);
      }
    },
    [report],
  );
  const setCommentVisibility = useCallback(
    async ({
      ticketId,
      commentId,
      internal,
      expectedUpdatedAt,
      signal,
      onSuccess,
      onError,
    }: Callbacks<void> & { ticketId: bigint; commentId: bigint; internal: boolean; expectedUpdatedAt: Timestamp }) => {
      try {
        await maintenanceClient.setTicketCommentVisibility(
          { ticketId, commentId, internal, expectedUpdatedAt },
          { signal },
        );
        onSuccess?.();
      } catch (error) {
        report(error, signal, onError);
      }
    },
    [report],
  );
  return {
    shareTicket,
    setCommentVisibility,
    listTickets,
    listTicketAssets,
    updateTarget,
    getTicket,
    createTicket,
    updateTicket,
    bulkUpdate,
    getStats,
    listAssignees,
    listCompleted,
    createComment,
    deleteComment,
  };
};
