import { randomUUID } from "crypto";

export function generateRandomText(prefix: string): string {
  // Same 7-char suffix length as the previous Math.random() implementation,
  // but from a CSPRNG since these names feed real account creation in RBAC specs.
  const randomCode = randomUUID().replaceAll("-", "").substring(0, 7);
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
