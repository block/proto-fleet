import { useCallback, useEffect, useRef, useState } from "react";
import { toTicketDetail } from "../mappers";
import type { TicketDetail } from "../types";
import { type UpdateTicketProps, useMaintenanceApi } from "@/protoFleet/api/maintenance";

export const useTicketDetail = (ticketId: string | null) => {
  const { getTicket, updateTicket, createComment, deleteComment, shareTicket, setCommentVisibility } =
    useMaintenanceApi();
  const [data, setData] = useState<TicketDetail | null>(null);
  const [loading, setLoading] = useState(Boolean(ticketId));
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const currentTicketId = useRef(ticketId);
  currentTicketId.current = ticketId;
  const refresh = useCallback(async () => {
    if (currentTicketId.current !== ticketId) return null;
    if (!ticketId) {
      setData(null);
      setLoading(false);
      return null;
    }
    setData((currentData) => (currentData?.id === ticketId ? currentData : null));
    controller.current?.abort();
    const current = new AbortController();
    controller.current = current;
    const request = ++sequence.current;
    setLoading(true);
    setError(null);
    let refreshed: TicketDetail | null = null;
    await getTicket({
      id: BigInt(ticketId),
      signal: current.signal,
      onSuccess: (value) => {
        if (request === sequence.current && value) {
          refreshed = toTicketDetail(value);
          setData(refreshed);
        }
      },
      onNotFound: () => {
        if (request === sequence.current) setData(null);
      },
      onError: (message) => {
        if (request === sequence.current) setError(message);
      },
    });
    if (request === sequence.current) setLoading(false);
    return refreshed;
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
  const update = useCallback(
    async (input: Omit<UpdateTicketProps, "id">) => {
      if (!ticketId) return false;
      const mutationTicketId = ticketId;
      setError(null);
      let ok = false;
      await updateTicket({
        ...input,
        id: BigInt(ticketId),
        onSuccess: () => {
          ok = true;
        },
        onError: (message) => {
          if (currentTicketId.current === mutationTicketId) setError(message);
        },
      });
      if (ok && currentTicketId.current === mutationTicketId) await refresh();
      return ok;
    },
    [refresh, ticketId, updateTicket],
  );
  const addComment = useCallback(
    async (text: string, idempotencyKey: string, audience: { internal: boolean } = { internal: true }) => {
      if (!ticketId) return false;
      const mutationTicketId = ticketId;
      setError(null);
      let ok = false;
      await createComment({
        ticketId: BigInt(ticketId),
        text,
        idempotencyKey,
        ...audience,
        onSuccess: () => {
          ok = true;
        },
        onError: (message) => {
          if (currentTicketId.current === mutationTicketId) setError(message);
        },
      });
      if (ok && currentTicketId.current === mutationTicketId) await refresh();
      return ok;
    },
    [createComment, refresh, ticketId],
  );
  const removeComment = useCallback(
    async (commentId: string) => {
      if (!ticketId) return false;
      const mutationTicketId = ticketId;
      setError(null);
      let ok = false;
      await deleteComment({
        commentId: BigInt(commentId),
        onSuccess: () => {
          ok = true;
        },
        onError: (message) => {
          if (currentTicketId.current === mutationTicketId) setError(message);
        },
      });
      if (ok && currentTicketId.current === mutationTicketId) await refresh();
      return ok;
    },
    [deleteComment, refresh, ticketId],
  );
  const share = useCallback(
    async (expectedUpdatedAt: NonNullable<TicketDetail["updatedAtSnapshot"]>) => {
      if (!ticketId) return false;
      const mutationTicketId = ticketId;
      setError(null);
      let ok = false;
      await shareTicket({
        ticketId: BigInt(ticketId),
        expectedUpdatedAt,
        onSuccess: () => {
          ok = true;
        },
        onError: (message) => {
          if (currentTicketId.current === mutationTicketId) setError(message);
        },
      });
      if (ok && currentTicketId.current === mutationTicketId) await refresh();
      return ok;
    },
    [refresh, shareTicket, ticketId],
  );
  const setNoteVisibility = useCallback(
    async (
      commentId: string,
      internal: boolean,
      expectedUpdatedAt: NonNullable<TicketDetail["updatedAtSnapshot"]>,
      onError?: (message: string) => void,
    ) => {
      if (!ticketId) return false;
      const mutationTicketId = ticketId;
      if (!onError) setError(null);
      let ok = false;
      await setCommentVisibility({
        ticketId: BigInt(ticketId),
        commentId: BigInt(commentId),
        internal,
        expectedUpdatedAt,
        onSuccess: () => {
          ok = true;
        },
        onError: (message) => {
          if (currentTicketId.current === mutationTicketId) {
            if (onError) onError(message);
            else setError(message);
          }
        },
      });
      if (ok && currentTicketId.current === mutationTicketId) await refresh();
      return ok;
    },
    [refresh, setCommentVisibility, ticketId],
  );
  return {
    share,
    setNoteVisibility,
    data: data?.id === ticketId ? data : null,
    loading,
    error,
    refresh,
    update,
    addComment,
    removeComment,
  };
};
