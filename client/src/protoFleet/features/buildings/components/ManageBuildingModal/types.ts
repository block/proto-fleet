// Shared types for the ManageBuildingModal sub-components.
//
// `byName` auto-fills cells alphabetically from the assigned-racks list.
// `manual` lets the operator click a cell + rack to choose a position.
// The modal swaps render between the two via the same activeAssignments
// pattern AssignMinersModal uses: manual edits live in slotAssignments,
// byName is derived on render.
export type BuildingAssignmentMode = "byName" | "manual";

// A grid cell key — `${aisle}-${position}` — used by slotAssignments maps.
export type GridCellKey = string;

export const cellKey = (aisle: number, position: number): GridCellKey => `${aisle}-${position}`;

export const parseCellKey = (key: GridCellKey): { aisle: number; position: number } => {
  const [aisle, position] = key.split("-").map(Number);
  return { aisle, position };
};
