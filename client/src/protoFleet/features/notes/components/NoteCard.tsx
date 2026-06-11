import { memo, type ReactElement, useState } from "react";

import { type Note } from "@/protoFleet/api/generated/notes/v1/notes_pb";
import { MAX_NOTE_CONTENT_LENGTH, useNotes } from "@/protoFleet/api/notes";
import {
  authorAvatarClass,
  authorInitial,
  noteFullTimestamp,
  noteTimeLabel,
} from "@/protoFleet/features/notes/noteFormat";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Textarea from "@/shared/components/Textarea";

interface NoteCardProps {
  note: Note;
  // Own-note affordances key off the username match for display; the
  // server enforces real authorship from the session either way.
  isOwn: boolean;
  canModerate: boolean;
  onChanged: () => void;
}

// updated_at > created_at is the wire-level "edited" signal: content
// edits are the only mutation a live note receives.
const isEdited = (note: Note): boolean => {
  const created = note.createdAt;
  const updated = note.updatedAt;
  if (!created || !updated) return false;
  if (updated.seconds !== created.seconds) return updated.seconds > created.seconds;
  return updated.nanos > created.nanos;
};

const NoteCard = ({ note, isOwn, canModerate, onChanged }: NoteCardProps): ReactElement => {
  const { updateNote, deleteNote } = useNotes();
  const [isEditing, setIsEditing] = useState(false);
  const [draft, setDraft] = useState(note.content);
  const [isConfirmingDelete, setIsConfirmingDelete] = useState(false);
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const trimmedDraft = draft.trim();
  const canSave = !isPending && trimmedDraft !== "" && trimmedDraft.length <= MAX_NOTE_CONTENT_LENGTH;

  const startEditing = () => {
    setDraft(note.content);
    setError(null);
    setIsEditing(true);
  };

  const saveEdit = () => {
    if (!canSave) return;
    setIsPending(true);
    void updateNote({
      id: note.id,
      content: trimmedDraft,
      onSuccess: () => {
        setIsEditing(false);
        onChanged();
      },
      onError: (message) => setError(message),
      onFinally: () => setIsPending(false),
    });
  };

  const confirmDelete = () => {
    setIsPending(true);
    void deleteNote({
      id: note.id,
      onSuccess: () => {
        setIsConfirmingDelete(false);
        onChanged();
      },
      onError: (message) => {
        setIsConfirmingDelete(false);
        setError(message);
      },
      onFinally: () => setIsPending(false),
    });
  };

  return (
    <div className="group flex gap-3 rounded-lg px-3 py-2.5 hover:bg-core-primary-2" data-testid="note-card">
      <div
        aria-hidden="true"
        className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-emphasis-200 text-white select-none ${authorAvatarClass(note.authorUsername)}`}
      >
        {authorInitial(note.authorUsername)}
      </div>

      <div className="min-w-0 flex-1">
        {/* Hierarchy: content is the largest text in the card; the
            author rides on weight (the avatar already carries identity);
            the timestamp is the quietest element — small, mono,
            right-justified. */}
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-emphasis-200 text-text-primary">
            {note.authorUsername}
            {isOwn ? <span className="ml-1 text-200 text-text-primary-50">(you)</span> : null}
          </span>
          <span
            className="shrink-0 text-right font-mono text-[11px] text-text-primary-50"
            title={noteFullTimestamp(note)}
          >
            {noteTimeLabel(note)}
            {isEdited(note) ? <span className="italic"> (edited)</span> : null}
          </span>
        </div>

        {isEditing ? (
          <div className="mt-2 flex flex-col gap-2">
            <Textarea
              id={`note-edit-${note.id}`}
              label="Edit note"
              initValue={note.content}
              maxLength={MAX_NOTE_CONTENT_LENGTH}
              rows={3}
              disabled={isPending}
              onChange={(value) => setDraft(value)}
            />
            <div className="flex justify-end gap-2">
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Cancel"
                disabled={isPending}
                onClick={() => setIsEditing(false)}
              />
              <Button
                variant={variants.primary}
                size={sizes.compact}
                text="Save"
                disabled={!canSave}
                loading={isPending}
                onClick={saveEdit}
                testId="note-save"
              />
            </div>
          </div>
        ) : (
          <p className="mt-0.5 text-300 break-words whitespace-pre-wrap text-text-primary">{note.content}</p>
        )}

        {error ? <p className="mt-1 text-200 text-text-critical">{error}</p> : null}

        {!isEditing && (isOwn || canModerate) ? (
          // Quiet by default; surfaces on hover/focus so the feed reads
          // as content first. Stays reachable on touch via focus.
          <div className="mt-1 flex gap-3 opacity-0 transition-opacity duration-100 group-hover:opacity-100 focus-within:opacity-100 phone:opacity-100">
            {isOwn ? (
              <Button
                variant={variants.textOnly}
                text="Edit"
                textColor="text-text-primary-50"
                onClick={startEditing}
                testId="note-edit"
              />
            ) : null}
            <Button
              variant={variants.textOnly}
              text="Delete"
              textColor="text-text-critical"
              onClick={() => setIsConfirmingDelete(true)}
              testId="note-delete"
            />
          </div>
        ) : null}
      </div>

      <Dialog
        open={isConfirmingDelete}
        title="Delete note?"
        subtitle="This removes the note from the team notepad for everyone."
        onDismiss={() => setIsConfirmingDelete(false)}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            disabled: isPending,
            onClick: () => setIsConfirmingDelete(false),
          },
          {
            text: "Delete",
            variant: variants.danger,
            loading: isPending,
            onClick: confirmDelete,
            testId: "note-delete-confirm",
          },
        ]}
      />
    </div>
  );
};

// Memoized so a poll tick that changes nothing (mergeHeadPage returns
// the same array, but a parent re-render can still occur from other
// state) doesn't re-render every card; all props are stable or
// primitive.
export default memo(NoteCard);
