import { useEffect, useRef, useState } from "react";
import { Edit } from "@/shared/assets/icons";

interface ChannelEditableCellProps {
  value: string;
  placeholder: string;
  ariaLabel: string;
  onSave: (next: string) => void;
  readOnly?: boolean;
}

const ChannelEditableCell = ({ value, placeholder, ariaLabel, onSave, readOnly = false }: ChannelEditableCellProps) => {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [editing]);

  const commit = () => {
    const next = draft.trim();
    if (next && next !== value) onSave(next);
    setEditing(false);
  };

  if (readOnly) {
    return <span className="truncate">{value || placeholder}</span>;
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        type="text"
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel}
        className="bg-surface-1 w-full rounded-md border border-border-5 px-2 py-1 text-300 text-text-primary outline-none focus:border-core-primary-fill"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            setDraft(value);
            setEditing(false);
          }
        }}
      />
    );
  }

  return (
    <span className="group flex items-center gap-2">
      <span className="truncate">{value}</span>
      <button
        type="button"
        className="text-text-primary-50 opacity-0 transition-opacity group-hover:opacity-100 hover:text-text-primary"
        title={`Edit ${ariaLabel}`}
        aria-label={`Edit ${ariaLabel}`}
        onClick={() => {
          setDraft(value);
          setEditing(true);
        }}
      >
        <Edit />
      </button>
    </span>
  );
};

export default ChannelEditableCell;
