import { type ReactElement, useState } from "react";

import { MAX_NOTE_CONTENT_LENGTH, useNotes } from "@/protoFleet/api/notes";
import Button, { sizes, variants } from "@/shared/components/Button";
import Textarea from "@/shared/components/Textarea";

interface NoteComposerProps {
  onCreated: () => void;
}

const NoteComposer = ({ onCreated }: NoteComposerProps): ReactElement => {
  const { createNote } = useNotes();
  const [draft, setDraft] = useState("");
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Textarea seeds from initValue and owns its value after that, so a
  // successful post clears the field by remounting it with a new key.
  const [composerKey, setComposerKey] = useState(0);

  const trimmed = draft.trim();
  const canSubmit = !isPending && trimmed !== "" && trimmed.length <= MAX_NOTE_CONTENT_LENGTH;

  const submit = () => {
    if (!canSubmit) return;
    setIsPending(true);
    setError(null);
    void createNote({
      content: trimmed,
      onSuccess: () => {
        setDraft("");
        setComposerKey((key) => key + 1);
        onCreated();
      },
      onError: (message) => setError(message),
      onFinally: () => setIsPending(false),
    });
  };

  return (
    <div className="flex flex-col gap-2 border-b border-border-10 px-4 py-3" data-testid="note-composer">
      <Textarea
        key={composerKey}
        id="note-composer"
        label="Add a note"
        maxLength={MAX_NOTE_CONTENT_LENGTH}
        rows={3}
        disabled={isPending}
        onChange={(value) => setDraft(value)}
      />
      {error ? <p className="text-emphasis-100 text-text-critical">{error}</p> : null}
      <div className="flex justify-end">
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Add note"
          disabled={!canSubmit}
          loading={isPending}
          onClick={submit}
          testId="note-submit"
        />
      </div>
    </div>
  );
};

export default NoteComposer;
