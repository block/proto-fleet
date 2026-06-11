import { type ReactElement, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { type Note } from "@/protoFleet/api/generated/notes/v1/notes_pb";
import { MAX_NOTE_CONTENT_LENGTH, useNotes } from "@/protoFleet/api/notes";
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

const formatCreatedAt = (note: Note): string => (note.createdAt ? timestampDate(note.createdAt).toLocaleString() : "");

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
    <div className="border-b border-border-5 px-4 py-3" data-testid="note-card">
      <div className="flex items-baseline justify-between gap-2">
        <span className="truncate text-emphasis-300 text-text-primary">{note.authorUsername}</span>
        <span className="text-emphasis-100 shrink-0 text-text-primary-50">
          {formatCreatedAt(note)}
          {isEdited(note) ? " (edited)" : null}
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
        <p className="mt-1 text-emphasis-200 break-words whitespace-pre-wrap text-text-primary">{note.content}</p>
      )}

      {error ? <p className="text-emphasis-100 mt-1 text-text-critical">{error}</p> : null}

      {!isEditing && (isOwn || canModerate) ? (
        <div className="mt-1 flex gap-3">
          {isOwn ? <Button variant={variants.textOnly} text="Edit" onClick={startEditing} testId="note-edit" /> : null}
          <Button
            variant={variants.textOnly}
            text="Delete"
            textColor="text-text-critical"
            onClick={() => setIsConfirmingDelete(true)}
            testId="note-delete"
          />
        </div>
      ) : null}

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

export default NoteCard;
