import { useCallback } from "react";
import { type Code, ConnectError } from "@connectrpc/connect";

import { notesClient } from "@/protoFleet/api/clients";
import { type Note } from "@/protoFleet/api/generated/notes/v1/notes_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

// Content cap mirrors the proto's max_len and the server's post-trim
// recheck; the composer uses it to disable submit before a round trip.
export const MAX_NOTE_CONTENT_LENGTH = 4096;

interface CreateNoteProps {
  content: string;
  signal?: AbortSignal;
  onSuccess?: (note: Note) => void;
  onError?: (message: string, code?: Code) => void;
  onFinally?: () => void;
}

interface UpdateNoteProps {
  id: bigint;
  content: string;
  signal?: AbortSignal;
  onSuccess?: (note: Note) => void;
  onError?: (message: string, code?: Code) => void;
  onFinally?: () => void;
}

interface DeleteNoteProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: () => void;
  onError?: (message: string, code?: Code) => void;
  onFinally?: () => void;
}

// Mutation hooks for the shared team notepad. Feed reads live in
// useNotesFeed, which owns pagination + poll merging; these callbacks
// follow the sites.ts shape so composer/card components can stay
// presentation-only.
export const useNotes = () => {
  const { handleAuthErrors } = useAuthErrors();

  const fail = useCallback(
    (err: unknown, onError?: (message: string, code?: Code) => void) => {
      handleAuthErrors({
        error: err,
        onError: (error) => {
          const code = error instanceof ConnectError ? error.code : undefined;
          onError?.(getErrorMessage(error), code);
        },
      });
    },
    [handleAuthErrors],
  );

  const createNote = useCallback(
    async ({ content, signal, onSuccess, onError, onFinally }: CreateNoteProps) => {
      try {
        const response = await notesClient.createNote({ content }, { signal });
        if (response.note) onSuccess?.(response.note);
      } catch (err) {
        fail(err, onError);
      } finally {
        onFinally?.();
      }
    },
    [fail],
  );

  const updateNote = useCallback(
    async ({ id, content, signal, onSuccess, onError, onFinally }: UpdateNoteProps) => {
      try {
        const response = await notesClient.updateNote({ id, content }, { signal });
        if (response.note) onSuccess?.(response.note);
      } catch (err) {
        fail(err, onError);
      } finally {
        onFinally?.();
      }
    },
    [fail],
  );

  const deleteNote = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: DeleteNoteProps) => {
      try {
        await notesClient.deleteNote({ id }, { signal });
        onSuccess?.();
      } catch (err) {
        fail(err, onError);
      } finally {
        onFinally?.();
      }
    },
    [fail],
  );

  return { createNote, updateNote, deleteNote };
};
