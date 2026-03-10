const formatMinerCount = (count: number): string => `${count} miner${count === 1 ? "" : "s"}`;

export const getBulkRenameLoadingMessage = (selectionCount: number): string =>
  selectionCount === 1 ? "Renaming miner" : "Renaming miners";

export const getBulkRenameSuccessMessage = (renamedCount: number, unchangedCount: number): string => {
  if (unchangedCount === 0) {
    return `Renamed ${formatMinerCount(renamedCount)}`;
  }

  if (renamedCount === 0) {
    return `${formatMinerCount(unchangedCount)} unchanged`;
  }

  return `Renamed ${formatMinerCount(renamedCount)}; ${formatMinerCount(unchangedCount)} unchanged`;
};

export const getBulkRenameFailureMessage = (failedCount: number): string =>
  `Failed to rename ${formatMinerCount(failedCount)}`;

export const getBulkRenameRequestFailureMessage = (selectionCount: number): string =>
  selectionCount === 1 ? "Failed to rename miner" : "Failed to rename miners";
