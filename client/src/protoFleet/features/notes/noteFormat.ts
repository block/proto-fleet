import { timestampDate } from "@bufbuild/protobuf/wkt";

import { type Note } from "@/protoFleet/api/generated/notes/v1/notes_pb";

// Categorical fills from the shared extended palette, used to give each
// author a stable avatar color. Full literal class names so Tailwind
// sees them at build time.
const AVATAR_FILLS = [
  "bg-extended-sky-fill",
  "bg-extended-teal-fill",
  "bg-extended-purple-fill",
  "bg-extended-forest-fill",
  "bg-extended-pink-fill",
  "bg-extended-navy-fill",
  "bg-extended-taupe-fill",
  "bg-extended-dark-red-fill",
] as const;

// Stable per-author avatar color: same username, same fill, on every
// client — no coordination needed because it's a pure hash.
export const authorAvatarClass = (username: string): string => {
  let hash = 0;
  for (let i = 0; i < username.length; i++) {
    hash = (hash * 31 + username.charCodeAt(i)) | 0;
  }
  return AVATAR_FILLS[Math.abs(hash) % AVATAR_FILLS.length];
};

export const authorInitial = (username: string): string => username.trim().charAt(0).toUpperCase() || "?";

const noteCreatedAt = (note: Note): Date | null => (note.createdAt ? timestampDate(note.createdAt) : null);

// Time-of-day only — the surrounding day group header carries the date.
export const noteTimeLabel = (note: Note): string => {
  const date = noteCreatedAt(note);
  if (!date) return "";
  return date.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
};

// Full timestamp for the hover tooltip on the compact time label.
export const noteFullTimestamp = (note: Note): string => {
  const date = noteCreatedAt(note);
  return date ? date.toLocaleString() : "";
};

const startOfDay = (date: Date): number => new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();

const noteDayLabel = (date: Date, now: Date = new Date()): string => {
  const dayDiff = Math.round((startOfDay(now) - startOfDay(date)) / 86_400_000);
  if (dayDiff === 0) return "Today";
  if (dayDiff === 1) return "Yesterday";
  if (date.getFullYear() === now.getFullYear()) {
    return date.toLocaleDateString(undefined, { month: "long", day: "numeric" });
  }
  return date.toLocaleDateString(undefined, { month: "long", day: "numeric", year: "numeric" });
};

export interface NoteDayGroup {
  label: string;
  notes: Note[];
}

// Groups a newest-first feed into contiguous day buckets, preserving
// order. Notes without a timestamp (shouldn't happen on the wire) fold
// into the neighboring group rather than crashing the feed.
export const groupNotesByDay = (notes: Note[], now: Date = new Date()): NoteDayGroup[] => {
  const groups: NoteDayGroup[] = [];
  for (const note of notes) {
    const date = noteCreatedAt(note);
    const label = date ? noteDayLabel(date, now) : (groups[groups.length - 1]?.label ?? "Today");
    const last = groups[groups.length - 1];
    if (last && last.label === label) {
      last.notes.push(note);
    } else {
      groups.push({ label, notes: [note] });
    }
  }
  return groups;
};
