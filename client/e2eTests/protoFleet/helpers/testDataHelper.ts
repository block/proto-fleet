export function generateRandomText(prefix: string): string {
  const randomCode = Math.random().toString(36).substring(2, 9);
  return `${prefix}_${randomCode}`;
}

export function generateRandomUsername(): string {
  return generateRandomText("username");
}

// Issue icon IDs for miner issues column
export const IssueIcon = {
  CONTROL_BOARD: "control-board-icon",
  HASH_BOARD: "hashboard-icon",
  PSU: "lightning-alt-icon",
  FAN: "fan-icon",
  GENERAL_ALERT: "alert-icon",
} as const;

export type IssueIconId = (typeof IssueIcon)[keyof typeof IssueIcon];
