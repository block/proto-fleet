import { type ClipboardEvent, Fragment, type KeyboardEvent, useRef } from "react";
import clsx from "clsx";
import {
  INVITATION_CODE_GROUP_COUNT as GROUP_COUNT,
  INVITATION_CODE_GROUP_SIZE as GROUP_SIZE,
  INVITATION_CODE_LENGTH,
  isCompleteInvitationCode,
  isInvitationCodeCharacter,
  normalizeInvitationCode,
} from "../invitationCode";

interface InvitationCodeInputProps {
  value: string[];
  onChange: (value: string[]) => void;
  onSubmit?: () => void;
  autoFocus?: boolean;
}

export const InvitationCodeInput = ({ value, onChange, onSubmit, autoFocus }: InvitationCodeInputProps) => {
  const inputs = useRef<(HTMLInputElement | null)[]>([]);
  const focus = (index: number) => {
    const input = inputs.current[Math.max(0, Math.min(index, INVITATION_CODE_LENGTH - 1))];
    input?.focus();
    input?.select();
  };
  const fill = (start: number, text: string) => {
    const characters = [...normalizeInvitationCode(text)].slice(0, INVITATION_CODE_LENGTH - start);
    if (characters.length === 0) return;
    const next = [...value];
    characters.forEach((character, offset) => {
      next[start + offset] = character;
    });
    onChange(next);
    focus(start + characters.length);
  };
  const handleInput = (index: number, raw: string) => {
    const characters = normalizeInvitationCode(raw);
    if (characters.length === 0) {
      const next = [...value];
      next[index] = "";
      onChange(next);
    } else if (characters.length === 2 && value[index] && characters.includes(value[index])) {
      // Typing into a filled box whose selection was lost keeps the new character.
      fill(index, characters[0] === value[index] ? characters[1] : characters[0]);
    } else {
      fill(index, characters);
    }
  };
  const handleKeyDown = (index: number, event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "-" || event.key === " ") {
      event.preventDefault();
    } else if (event.key === "Backspace" && !value[index] && index > 0) {
      event.preventDefault();
      const next = [...value];
      next[index - 1] = "";
      onChange(next);
      focus(index - 1);
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      focus(index - 1);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      focus(index + 1);
    } else if (event.key === "Enter" && isCompleteInvitationCode(value)) {
      event.preventDefault();
      onSubmit?.();
    }
  };
  const handlePaste = (index: number, event: ClipboardEvent<HTMLInputElement>) => {
    event.preventDefault();
    fill(index, event.clipboardData.getData("text"));
  };
  const invalidAt = (index: number) => Boolean(value[index]) && !isInvitationCodeCharacter(value[index]);
  const hasInvalidCharacter = value.some((_, index) => invalidAt(index));

  const box = (index: number) => (
    <input
      key={index}
      ref={(element) => {
        inputs.current[index] = element;
      }}
      aria-label={`Invitation code character ${index + 1}`}
      aria-invalid={invalidAt(index) || undefined}
      value={value[index] ?? ""}
      placeholder="X"
      autoFocus={index === 0 ? autoFocus : false}
      autoComplete="off"
      autoCapitalize="characters"
      autoCorrect="off"
      spellCheck={false}
      onFocus={(event) => event.target.select()}
      onChange={(event) => handleInput(index, event.target.value)}
      onKeyDown={(event) => handleKeyDown(index, event)}
      onPaste={(event) => handlePaste(index, event)}
      className={clsx(
        // 16px text avoids iOS zooming into the focused box.
        "h-12 w-full min-w-0 rounded-lg border bg-surface-base text-center font-mono text-400 text-text-primary uppercase outline-hidden",
        "transition duration-200 ease-in-out placeholder:text-text-primary-30 focus:placeholder:text-transparent",
        invalidAt(index)
          ? "border-intent-critical-50 focus:ring-4 focus:ring-intent-critical-20"
          : "border-border-5 focus:border-border-20 focus:ring-4 focus:ring-core-primary-5",
      )}
    />
  );
  const group = (groupIndex: number) => (
    <div className="flex flex-1 gap-1">
      {Array.from({ length: GROUP_SIZE }, (_, offset) => box(groupIndex * GROUP_SIZE + offset))}
    </div>
  );
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="sr-only">Invitation code</legend>
      <div className="flex items-center gap-2">
        {Array.from({ length: GROUP_COUNT }, (_, groupIndex) => (
          <Fragment key={groupIndex}>
            {groupIndex > 0 ? (
              <span aria-hidden className="text-400 text-text-primary-30">
                -
              </span>
            ) : null}
            {group(groupIndex)}
          </Fragment>
        ))}
      </div>
      {hasInvalidCharacter ? (
        <p className="text-200 text-text-critical" role="alert">
          Codes use letters and the digits 2–9. They never contain 0, 1, I or O.
        </p>
      ) : null}
    </fieldset>
  );
};
