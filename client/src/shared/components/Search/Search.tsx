import { RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { DismissCircle } from "@/shared/assets/icons";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

/**
 * - `default` — the full-height field for modals and panels: bordered, 56px,
 *   floating label, and a ⌘K focus hint.
 * - `toolbar` — the same affordances at control height, for sitting beside
 *   compact buttons in a list header.
 * - `compact` — the bare baseline: no border, focus ring, or clear button. It
 *   reads as static text unless the surrounding layout supplies its own
 *   container, so prefer `toolbar` for anything a user has to find.
 */
export type SearchVariant = "compact" | "toolbar" | "default";

interface SearchProps {
  className?: string;
  variant?: SearchVariant;
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
  variant = "default",
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

  const isToolbar = variant === "toolbar";
  const isDefault = variant === "default";
  // Input's compact mode is the bare presentation. `toolbar` borrows it and
  // supplies its own container, so the field keeps control height instead of
  // the 56px default.
  const bareInput = !isDefault;

  const focusSearch = (event: KeyboardEvent) => {
    // event.metaKey - pressed Command key on Macs
    // event.ctrlKey - pressed Control key on Linux or Windows
    if (isDefault && (event.metaKey || event.ctrlKey) && event.code === "KeyK") {
      event.preventDefault();
      inputRef.current?.focus();
    }
  };

  useKeyDown({ onKeyDown: focusSearch });

  const handleChange = useCallback(
    (value: string) => {
      setValue(value);
      onChange(value, id);
    },
    [id, onChange],
  );

  const clearValue = useCallback(() => {
    handleChange("");
    // Clearing from the button would otherwise drop focus and force the user
    // back to the mouse to start a new query.
    inputRef.current?.focus();
  }, [handleChange]);

  const clearValueOnEscape = useCallback(
    (key: string) => {
      if (key === "Escape") {
        handleChange("");
      }
    },
    [handleChange],
  );

  const cmdOrCtrl = useMemo(() => (window.navigator.platform.match(/^Mac/) ? "⌘" : "Ctrl"), []);

  useEffect(() => {
    if (shouldFocus) {
      inputRef.current?.focus();
    }
  }, [shouldFocus]);

  // Input only renders its own clear button at the default height, so `toolbar`
  // supplies one to avoid a bordered field with no way out but the Escape key.
  const showToolbarClear = isToolbar && Boolean(value);

  return (
    <div className="w-full tablet:w-80">
      <div
        className={clsx({
          "relative rounded-lg border border-border-5 px-3 py-1 transition-colors focus-within:border-border-20":
            isToolbar,
        })}
      >
        <Input
          id={id}
          // `block` matters for alignment, not layout: an inline-block input
          // leaves descender space inside its wrapper, so the field sits a
          // couple of pixels above the container's true center and anything
          // centered against the container reads as misaligned.
          className={clsx(className, { block: isToolbar, "pr-6": showToolbarClear })}
          label={label}
          onChange={handleChange}
          hideLabelOnFocus
          dismiss
          keyboardShortcuts={isDefault ? [cmdOrCtrl, "K"] : undefined}
          inputRef={inputRef}
          initValue={value}
          onKeyDown={clearValueOnEscape}
          compact={bareInput}
          sanitize={sanitize}
        />
        {showToolbarClear ? (
          // Spans the container and flex-centers rather than translating off
          // top-1/2, which measures a shrink-wrapped box with the same
          // descender problem. `right-3` matches the container's horizontal
          // padding so the icon lines up with the text inset opposite it.
          <div className="absolute inset-y-0 right-3 flex items-center">
            <DismissCircle ariaLabel={`Clear ${label}`} onClick={clearValue} className="text-text-primary-70" />
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default Search;
