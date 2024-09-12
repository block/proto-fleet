import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useKeyDown } from "common/hooks/useKeyDown";

import Input from "components/Input";

interface SearchProps {
  compact?: boolean;
  initValue?: string;
  onChange: (value: string, id: string) => void;
  shouldFocus?: boolean;
}

const id = "search";

const Search = ({ compact, onChange, initValue, shouldFocus }: SearchProps) => {
  const [value, setValue] = useState(initValue);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setValue(initValue);
  }, [initValue]);

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
    [onChange]
  );

  const handleChange = useCallback(
    (value: string) => {
      setValue(value);
      onChange(value, id);
    },
    [onChange]
  );

  const cmdOrCtrl = useMemo(
    () => (window.navigator.platform.match(/^Mac/) ? "⌘" : "Ctrl"),
    []
  );

  useEffect(() => {
    if (shouldFocus) {
      inputRef.current?.focus();
    }
  }, [shouldFocus]);

  return (
    <div className="w-80 phone:w-24">
      <Input
        id={id}
        label="Search"
        onChange={handleChange}
        hideLabelOnFocus
        dismiss
        keyboardShortcuts={compact ? undefined : [cmdOrCtrl, "K"]}
        inputRef={inputRef}
        initValue={value}
        onKeyDown={clearValueOnEscape}
        compact={compact}
      />
    </div>
  );
};

export default Search;
