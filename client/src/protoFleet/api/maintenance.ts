import { useCallback } from "react";

// import { maintenanceClient } from "@/protoFleet/api/clients";
// import type {
//   RepairTicket,
//   RepairTicketDetail,
//   RepairTicketSummary,
//   TicketComment,
//   TicketFilter,
//   TicketSortField,
//   SortDirection,
// } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListTicketsProps {
  filter?: Record<string, unknown>;
  sortField?: number;
  sortDirection?: number;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
  onSuccess?: (tickets: unknown[], nextPageToken: string, totalCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetTicketProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (detail: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateTicketProps {
  category: number;
  component: string;
  diagnosis: string;
  urgent?: boolean;
  minerIdentifier?: string;
  alertId?: string;
  assigneeUserId?: bigint;
  warrantyStatus?: number;
  siteId?: bigint;
  buildingId?: bigint;
  zone?: string;
  rackId?: bigint;
  rackLabel?: string;
  groupLabel?: string;
  notes?: string;
  signal?: AbortSignal;
  onSuccess?: (ticket: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateTicketProps {
  id: bigint;
  status?: number;
  urgent?: boolean;
  assigneeUserId?: bigint;
  clearAssignee?: boolean;
  resolution?: number;
  repairLocation?: number;
  partsUsed?: Array<{ partName: string; quantity: number }>;
  notes?: string;
  rmaVendor?: string;
  rmaTracking?: string;
  signal?: AbortSignal;
  onSuccess?: (ticket: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface BulkUpdateProps {
  ticketIds: bigint[];
  mutation: Record<string, unknown>;
  signal?: AbortSignal;
  onSuccess?: (updatedCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetTicketStatsProps {
  signal?: AbortSignal;
  onSuccess?: (stats: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CommentProps {
  ticketId: bigint;
  text?: string;
  commentId?: bigint;
  signal?: AbortSignal;
  onSuccess?: (result: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

export const useMaintenanceApi = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listTickets = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListTicketsProps) => {
      try {
        // TODO: wire to maintenanceClient.listRepairTickets
        if (signal?.aborted) return;
        onSuccess?.([], "", 0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getTicket = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: GetTicketProps) => {
      try {
        // TODO: wire to maintenanceClient.getRepairTicket
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createTicket = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: CreateTicketProps) => {
      try {
        // TODO: wire to maintenanceClient.createRepairTicket
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateTicket = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: UpdateTicketProps) => {
      try {
        // TODO: wire to maintenanceClient.updateRepairTicket
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const bulkUpdate = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: BulkUpdateProps) => {
      try {
        // TODO: wire to maintenanceClient.bulkUpdateRepairTickets
        if (signal?.aborted) return;
        onSuccess?.(0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getStats = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: GetTicketStatsProps) => {
      try {
        // TODO: wire to maintenanceClient.getTicketStats
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createComment = useCallback(
    async ({ ticketId, text, signal, onSuccess, onError, onFinally }: CommentProps) => {
      try {
        // TODO: wire to maintenanceClient.createTicketComment
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const deleteComment = useCallback(
    async ({ commentId, signal, onSuccess, onError, onFinally }: CommentProps) => {
      try {
        // TODO: wire to maintenanceClient.deleteTicketComment
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return {
    listTickets,
    getTicket,
    createTicket,
    updateTicket,
    bulkUpdate,
    getStats,
    createComment,
    deleteComment,
  };
};
