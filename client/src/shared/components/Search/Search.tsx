import { RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";

import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

interface SearchProps {
  className?: string;
  compact?: boolean;
  initValue?: string;
  onChange: (value: string, id: string) => void;
  shouldFocus?: boolean;
  id?: string;
  label?: string;
  /** Forwarded to the underlying Input. Normalization that should reach the
   * displayed text has to run there, before the internal update and `onChange`,
   * so the visible text and the emitted value cannot disagree. */
  sanitize?: (value: string) => string;
}

const defaultId = "search";

const Search = ({
  className,
  compact,
  onChange,
  initValue,
  shouldFocus,
  id = defaultId,
  label = "Search",
  sanitize,
}: SearchProps) => {
  const [value, setValue] = useState(initValue);
  const [prevInitValue, setPrevInitValue] = useState(initValue);
  if (initValue !== prevInitValue) {
    setPrevInitValue(initValue);
    setValue(initValue);
  }
  const inputRef = useRef<HTMLInputElement>(null) as RefObject<HTMLInputElement>;

  const focusSearch = (event: KeyboardEvent) => {
    // event.metaKey - pressed Command key on Macs
    // event.ctrlKey - pressed Control key on Linux or Windows
    if (!compact && (event.metaKey || event.ctrlKey) && event.code === "KeyK") {
      event.preventDefault();
      inputRef.current?.focus();
    }
  };

  useKeyDown({ onKeyDown: focusSearch });

  const clearValueOnEscape = useCallback(
    (key: string) => {
      if (key === "Escape") {
        setValue("");
        onChange("", id);
      }
    },
    [id, onChange],
  );

  const handleChange = useCallback(
    (value: string) => {
      setValue(value);
      onChange(value, id);
    },
    [id, onChange],
  );

  const cmdOrCtrl = useMemo(() => (window.navigator.platform.match(/^Mac/) ? "⌘" : "Ctrl"), []);

  useEffect(() => {
    if (shouldFocus) {
      inputRef.current?.focus();
    }
  }, [shouldFocus]);

  return (
    <div className="w-24 tablet:w-80">
      <Input
        id={id}
        className={className}
        label={label}
        onChange={handleChange}
        hideLabelOnFocus
        dismiss
        keyboardShortcuts={compact ? undefined : [cmdOrCtrl, "K"]}
        inputRef={inputRef}
        initValue={value}
        onKeyDown={clearValueOnEscape}
        compact={compact}
        sanitize={sanitize}
      />
    </div>
  );
};

export default Search;
