import type { PickerOption } from "@/protoFleet/features/notifications/components/SinglePickerField";

// Scopes a silence can target. Mirrors prototype's SILENCE_SCOPE_OPTIONS.
export const SILENCE_SCOPE_OPTIONS: PickerOption[] = [
  { id: "rule", label: "A rule" },
  { id: "group", label: "A group" },
  { id: "site", label: "A site" },
  { id: "device", label: "Specific devices" },
];

export interface QuickWindowOption extends PickerOption {
  hours: number;
}

export const SILENCE_QUICK_OPTIONS: QuickWindowOption[] = [
  { id: "1h", label: "1 hour", hours: 1 },
  { id: "4h", label: "4 hours", hours: 4 },
  { id: "8h", label: "8 hours", hours: 8 },
  { id: "24h", label: "1 day", hours: 24 },
  { id: "72h", label: "3 days", hours: 72 },
];

// Convert a Date to a string usable in <input type="datetime-local">.
// Format: YYYY-MM-DDTHH:mm (local time, no timezone offset).
export const toLocalDatetimeValue = (date: Date): string => {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
};
