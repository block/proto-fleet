import type { KeyboardEvent } from "react";

export function isInputEnterSaveEvent(event: KeyboardEvent<HTMLElement>): boolean {
  return (
    event.key === "Enter" &&
    !event.defaultPrevented &&
    !event.nativeEvent.isComposing &&
    !event.altKey &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.shiftKey &&
    event.target instanceof HTMLInputElement
  );
}
