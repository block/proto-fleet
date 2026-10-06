// Counter math shared by the bulk create forms (buildings at a site, racks in a
// building). Both generate a run of names from a prefix plus an incrementing,
// zero-padded counter, and both feed the same string list to their preview and
// their RPC — so the math lives in one place rather than being reimplemented per
// feature, where the padding and clamping edges could drift apart.
//
// The prefix + counter-start + counter-scale triple is deliberately the same
// shape the bulk-rename flow uses (see bulkRenamePreview.ts): scale is a
// zero-pad WIDTH, not a multiplier, so start 1 at scale 3 reads "001". An
// operator who has used bulk rename already knows what these three fields do.

import {
  counterScaleMaximum,
  counterScaleMinimum,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/RenameOptionsModals/constants";

export interface BulkNameOptions {
  namePrefix: string;
  counterStart: number;
  counterScale: number;
}

// Zero-pads to `scale` digits. A number already wider than the scale is left
// alone rather than truncated — silently renaming building 1000 to "000" would
// be worse than an inconsistent width.
export const formatBulkCounter = (value: number, scale: number): string =>
  String(value).padStart(clampScale(scale), "0");

const clampScale = (scale: number): number => {
  if (!Number.isFinite(scale)) return counterScaleMinimum;
  return Math.min(Math.max(Math.trunc(scale), counterScaleMinimum), counterScaleMaximum);
};

// The names the batch will submit, in order. This is the single source of truth
// for both the on-screen preview and the request payload — the preview cannot
// show one thing and the RPC create another.
//
// The prefix is used exactly as typed: "Building - " has to reach the counter
// with its trailing space intact, since that spacing is the separator the
// operator chose. (Bulk rename trims its prefix, but there the sections are
// joined by an explicit "-", so interior spacing isn't theirs to control.)
// Space around the *finished* name is still the server's to strip.
export const buildBulkNameSeries = (count: number, options: BulkNameOptions, maxRows: number): string[] => {
  const rows = Math.min(Math.max(Math.trunc(count) || 0, 0), maxRows);
  const start = Number.isFinite(options.counterStart) ? Math.trunc(options.counterStart) : 1;
  return Array.from(
    { length: rows },
    (_, i) => `${options.namePrefix}${formatBulkCounter(start + i, options.counterScale)}`,
  );
};

// Generated names the server would refuse for length. Checked on the finished
// name, not the prefix: the counter is part of what gets stored, so a prefix
// that fits on its own still overflows once the zero-padding widens it (or the
// counter outgrows the padding). Length is compared untrimmed because that is
// what buf.validate measures.
export const overlongNameIndexes = (names: string[], maxLength: number): number[] =>
  names.reduce<number[]>((acc, name, i) => {
    if (name.length > maxLength) acc.push(i);
    return acc;
  }, []);

// Generated names that collide with one already in use. Compared on the trimmed
// value because that is what the server stores.
export const takenNameIndexes = (names: string[], existingNames: string[]): number[] => {
  const taken = new Set(existingNames.map((n) => n.trim()));
  return names.reduce<number[]>((acc, name, i) => {
    if (taken.has(name.trim())) acc.push(i);
    return acc;
  }, []);
};
