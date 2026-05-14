export const targetSelectPlaceholderLabel = "Select";

export function getTargetButtonLabel(count: number, singular: string): string {
  if (count === 0) {
    return targetSelectPlaceholderLabel;
  }

  const noun = count === 1 ? singular : `${singular}s`;
  return `${count} ${noun}`;
}
