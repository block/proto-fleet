export type MiningSelectionKey = `miner:${string}` | `module:${bigint}`;

export const minerIdentifierFromSelectionKey = (key: MiningSelectionKey): string | undefined =>
  key.startsWith("miner:") ? key.slice("miner:".length) : undefined;

export const selectedMinerIdentifiers = (keys: MiningSelectionKey[]): string[] =>
  keys.flatMap((key) => {
    const identifier = minerIdentifierFromSelectionKey(key);
    return identifier === undefined ? [] : [identifier];
  });
