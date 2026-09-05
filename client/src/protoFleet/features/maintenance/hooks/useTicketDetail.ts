import { useCallback, useEffect, useRef, useState } from "react";
import { toTicketDetail } from "../mappers";
import type { TicketDetail } from "../types";
import { type UpdateTicketProps, useMaintenanceApi } from "@/protoFleet/api/maintenance";

const POLL_INTERVAL_MS = 15_000;

export const useTicketDetail = (ticketId: string | null) => {
  const { getTicket, updateTicket, createComment, deleteComment } = useMaintenanceApi();
  const [data, setData] = useState<TicketDetail | null>(null);
  const [loading, setLoading] = useState(Boolean(ticketId));
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const refresh = useCallback(async () => {
    if (!ticketId) {
      setData(null);
      setLoading(false);
      return;
    }
    setData((currentData) => (currentData?.id === ticketId ? currentData : null));
    controller.current?.abort();
    const current = new AbortController();
    controller.current = current;
    const request = ++sequence.current;
    setLoading(true);
    setError(null);
    await getTicket({
      id: BigInt(ticketId),
      signal: current.signal,
      onSuccess: (value) => {
        if (request === sequence.current && value) setData(toTicketDetail(value));
      },
      onNotFound: () => {
        if (request === sequence.current) setData(null);
      },
      onError: (message) => {
        if (request === sequence.current) setError(message);
      },
    });
    if (request === sequence.current) setLoading(false);
  }, [getTicket, ticketId]);
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void refresh();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [refresh]);
  useEffect(() => {
    if (!ticketId) return;
    let active = true;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;
    const scheduleNext = () => {
      timeoutId = setTimeout(async () => {
        if (!active) return;
        try {
          await refresh();
        } catch {
          // RPC adapters report request failures through refresh's onError callback.
        } finally {
          if (active) scheduleNext();
        }
      }, POLL_INTERVAL_MS);
    };
    scheduleNext();
    return () => {
      active = false;
      if (timeoutId !== undefined) clearTimeout(timeoutId);
    };
  }, [refresh, ticketId]);
  const update = useCallback(
    async (input: Omit<UpdateTicketProps, "id">) => {
      if (!ticketId) return false;
      let ok = false;
      await updateTicket({
        ...input,
        id: BigInt(ticketId),
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refresh();
      return ok;
    },
    [refresh, ticketId, updateTicket],
  );
  const addComment = useCallback(
    async (text: string) => {
      if (!ticketId) return false;
      let ok = false;
      await createComment({
        ticketId: BigInt(ticketId),
        text,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refresh();
      return ok;
    },
    [createComment, refresh, ticketId],
  );
  const removeComment = useCallback(
    async (commentId: string) => {
      let ok = false;
      await deleteComment({
        commentId: BigInt(commentId),
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refresh();
      return ok;
    },
    [deleteComment, refresh],
  );
  return {
    data: data?.id === ticketId ? data : null,
    loading,
    error,
    refresh,
    update,
    addComment,
    removeComment,
  };
};
