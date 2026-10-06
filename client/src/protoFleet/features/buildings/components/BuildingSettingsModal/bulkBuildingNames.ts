// Name generation for the bulk (Multiple) building-create form. The counter
// math itself is shared with the bulk rack-create form — see
// utils/bulkNameSeries.ts for how prefix / start / scale compose.

import {
  buildBulkNameSeries,
  type BulkNameOptions,
  overlongNameIndexes,
  takenNameIndexes,
} from "@/protoFleet/utils/bulkNameSeries";

// Cap matches the buf.validate max_items on CreateBuildingsRequest.buildings.
// A typo guard, not a capacity limit.
export const bulkBuildingCountMaximum = 500;

// Matches NewBuilding.name's buf.validate max_len. Exceeding it fails the whole
// batch with a generic InvalidArgument, so the form has to catch it first.
export const buildingNameMaxLength = 255;

export type { BulkNameOptions };

export const buildBulkBuildingNames = (count: number, options: BulkNameOptions): string[] =>
  buildBulkNameSeries(count, options, bulkBuildingCountMaximum);

// Rows the server would reject on length alone. The prefix field can't guard
// this by itself: the counter's width is part of the name, and raising the scale
// (or the start value) widens every row after the prefix was typed.
export const overlongBuildingNameIndexes = (names: string[]): number[] =>
  overlongNameIndexes(names, buildingNameMaxLength);

// Names that collide with a building already at the site.
//
// This is the only collision the form can pre-check: the server's other
// rejection reason (duplicate *within* the batch) is unreachable from generated
// names, since a shared prefix plus a strictly incrementing counter can't repeat
// itself. The handler still enforces it for hand-built requests.
export { takenNameIndexes };
