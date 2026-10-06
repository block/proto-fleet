// Label generation for the bulk (Multiple) rack-create form. The counter math
// is shared with the bulk building-create form — see utils/bulkNameSeries.ts for
// how prefix / start / scale compose.
//
// Note what is deliberately absent: a takenLabelIndexes pre-check. Rack labels
// are unique per (org, type), not per building or site, so no list this form can
// hold is authoritative — the rack a label collides with may sit in another site
// entirely. The server owns that check and reports it per row (see CreateRacks),
// and this form renders those rows from the response.

import { buildBulkNameSeries, type BulkNameOptions, overlongNameIndexes } from "@/protoFleet/utils/bulkNameSeries";

// Cap matches the buf.validate max_items on CreateRacksRequest.racks. A typo
// guard, not a capacity limit — the building's grid enforces the real one.
export const bulkRackCountMaximum = 500;

// Matches NewRack.label's buf.validate max_len (and RackInfo.label's). Exceeding
// it fails the whole batch with a generic InvalidArgument, so the form has to
// catch it before the call.
export const rackLabelMaxLength = 100;

export type { BulkNameOptions };

export const buildBulkRackLabels = (count: number, options: BulkNameOptions): string[] =>
  buildBulkNameSeries(count, options, bulkRackCountMaximum);

// Rows the server would reject on length alone. The prefix field can't guard
// this by itself: the counter's width is part of the label, and raising the
// scale (or the start value) widens every row after the prefix was typed.
export const overlongRackLabelIndexes = (labels: string[]): number[] => overlongNameIndexes(labels, rackLabelMaxLength);
