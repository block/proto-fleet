import { useMemo, useState } from "react";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Textarea from "@/shared/components/Textarea";

const DEFAULT_MAX_LINES = 1024;

type TextareaListModalProps = {
  open: boolean;
  categoryKey: string;
  label: string;
  validate: (line: string) => string | null;
  normalize?: (line: string) => string;
  placeholder?: string;
  maxLines?: number;
  initialValue: string[];
  onApply: (value: string[]) => void;
  onClose: () => void;
};

type ParsedLine = { lineNumber: number; trimmed: string; error: string | null };

const parseLines = (
  text: string,
  validate: (line: string) => string | null,
  maxLines: number,
): { parsed: ParsedLine[]; truncated: boolean } => {
  const allLines = text.split("\n");
  const truncated = allLines.length > maxLines;
  const slice = truncated ? allLines.slice(0, maxLines) : allLines;
  const parsed = slice.map((raw, idx) => {
    const trimmed = raw.trim();
    if (trimmed === "") {
      return { lineNumber: idx + 1, trimmed, error: null };
    }
    return { lineNumber: idx + 1, trimmed, error: validate(trimmed) };
  });
  return { parsed, truncated };
};

const TextareaListModal = (props: TextareaListModalProps) => {
  // Re-key on open so the draft state hydrates fresh each time the parent
  // opens the modal for a different category, without needing useEffect.
  return props.open ? <TextareaListModalContent key={props.categoryKey} {...props} /> : null;
};

const TextareaListModalContent = ({
  categoryKey,
  label,
  validate,
  normalize,
  placeholder,
  maxLines = DEFAULT_MAX_LINES,
  initialValue,
  onApply,
  onClose,
}: TextareaListModalProps) => {
  const [draft, setDraft] = useState(initialValue.join("\n"));

  const { parsed, truncated } = useMemo(() => parseLines(draft, validate, maxLines), [draft, validate, maxLines]);

  const errorEntries = parsed.filter((p) => p.error !== null);
  const isValid = errorEntries.length === 0;

  const handleApply = () => {
    const acceptedRaw = parsed
      .filter((p) => p.trimmed !== "" && p.error === null)
      .map((p) => (normalize ? normalize(p.trimmed) : p.trimmed));
    const seen = new Set<string>();
    const unique: string[] = [];
    for (const v of acceptedRaw) {
      if (!seen.has(v)) {
        seen.add(v);
        unique.push(v);
      }
    }
    onApply(unique);
    onClose();
  };

  const textareaId = `textarea-list-${categoryKey}`;

  return (
    <Modal
      open
      title={label}
      onDismiss={onClose}
      size="standard"
      testId={`textarea-list-modal-${categoryKey}`}
      buttons={[
        {
          text: "Apply",
          onClick: handleApply,
          variant: variants.primary,
          disabled: !isValid,
        },
      ]}
    >
      <div className="mt-4 flex flex-col gap-3">
        {placeholder ? (
          <div className="text-200 text-text-primary-70" data-testid={`textarea-list-${categoryKey}-hint`}>
            One per line. Example: <span className="font-mono">{placeholder.split("\n")[0]}</span>
          </div>
        ) : null}
        <Textarea
          id={textareaId}
          label={label}
          initValue={draft}
          onChange={(value) => setDraft(value)}
          rows={8}
          // Boolean error tints the border red; per-line messages render below
          // (they're easier to scan as a list than as one joined string).
          error={errorEntries.length > 0}
          testId={textareaId}
        />
        {errorEntries.length > 0 ? (
          <div
            className="space-y-1 text-200 text-intent-critical-fill"
            data-testid={`textarea-list-${categoryKey}-errors`}
          >
            {errorEntries.map((e) => (
              <div key={e.lineNumber}>
                Line {e.lineNumber}: {e.error}
              </div>
            ))}
          </div>
        ) : null}
        {truncated ? (
          <div className="text-200 text-text-primary-70" data-testid={`textarea-list-${categoryKey}-truncation-notice`}>
            Showing first {maxLines}; additional lines ignored.
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default TextareaListModal;
