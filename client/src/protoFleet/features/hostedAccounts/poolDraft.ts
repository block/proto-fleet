import type { PoolRevision } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export type SlotDraft = {
  url: string;
  username: string;
  workerTemplate: string;
  password: string;
  preservePassword: boolean;
};
export const validWorkerTemplate = (value: string) => !/[{}]/.test(value.replace(/\{miner_id\}|\{miner_name\}/g, ""));
export const canPreservePassword = (slot: SlotDraft, original: PoolRevision | undefined, priority: number) =>
  original?.slots[priority]?.url === slot.url && original?.slots[priority]?.username === slot.username;
export const poolSlotInput = ({ url, username, workerTemplate, password, preservePassword }: SlotDraft) => ({
  url,
  username,
  workerTemplate,
  ...(preservePassword ? {} : { password }),
});
export const blankSlot = (): SlotDraft => ({
  url: "",
  username: "",
  workerTemplate: "{miner_name}",
  password: "",
  preservePassword: false,
});
