import { type ReactNode } from "react";

import { baseEventType } from "@/protoFleet/features/activity/utils/eventType";
import {
  Alert,
  ArrowLeftCompact,
  Edit,
  Fan,
  type IconProps,
  InfoInverted,
  LEDIndicator,
  Lock,
  Logs,
  MiningPools,
  Minus,
  MinusFilled,
  Plus,
  PlusFilled,
  Power,
  Reboot,
  Settings,
  Speedometer,
  Unpair,
} from "@/shared/assets/icons";

export type ActivityIconTone = "default" | "critical";

const alertEventTypes = new Set(["login_failed"]);

function isCreateEvent(eventType: string): boolean {
  return eventType.startsWith("create_") || eventType.split(/[._]/).includes("created");
}

function isDeleteEvent(eventType: string): boolean {
  return eventType.startsWith("delete_") || eventType.split(/[._]/).includes("deleted");
}

function isAddOrAssignEvent(eventType: string): boolean {
  const parts = eventType.split(/[._]/);
  return (
    eventType.startsWith("add_") ||
    eventType.startsWith("assign_") ||
    parts.includes("assigned") ||
    parts.includes("reassigned")
  );
}

function isSaveOrUpdateEvent(eventType: string): boolean {
  const parts = eventType.split(/[._]/);
  return (
    eventType.startsWith("edit_") ||
    eventType.startsWith("rename_") ||
    eventType.startsWith("save_") ||
    eventType.startsWith("update_") ||
    parts.includes("edited") ||
    parts.includes("renamed") ||
    parts.includes("saved") ||
    parts.includes("updated")
  );
}

function isDestructiveEvent(eventType: string): boolean {
  return (
    isDeleteEvent(eventType) ||
    eventType.startsWith("deactivate_") ||
    eventType.startsWith("remove_") ||
    eventType.startsWith("revoke_") ||
    (eventType.startsWith("clear_") && eventType !== "clear_rack_slot") ||
    eventType === "unpair" ||
    eventType === "unpair_miners"
  );
}

const iconMap: Record<string, (props: IconProps) => ReactNode> = {
  login: Lock,
  login_failed: Alert,
  logout: ArrowLeftCompact,
  update_password: Lock,
  update_username: Lock,
  deactivate_user: MinusFilled,
  reset_password: Lock,
  update_user_role: Lock,

  stop_mining: Power,
  start_mining: Power,
  reboot: Reboot,
  blink_led: LEDIndicator,
  download_logs: Logs,
  set_power_target: Speedometer,
  set_cooling_mode: Fan,
  update_mining_pools: MiningPools,
  update_miner_password: Lock,
  firmware_update: Settings,
  unpair: Unpair,

  unpair_miners: Unpair,
  rename_miners: Edit,

  delete_collection: MinusFilled,
  remove_devices: Minus,
  set_rack_slot: Edit,
  clear_rack_slot: Edit,
  save_rack: Edit,

  update_pool: MiningPools,
  delete_pool: MinusFilled,

  update_role: Edit,
  delete_role: MinusFilled,
  cohort_updated: Edit,
};

export function getActivityIcon(eventType: string, result?: string): (props: IconProps) => ReactNode {
  if (result === "failure") return Alert;

  const normalizedEventType = baseEventType(eventType);
  if (isCreateEvent(normalizedEventType)) return PlusFilled;
  if (isDeleteEvent(normalizedEventType)) return MinusFilled;
  if (isAddOrAssignEvent(normalizedEventType)) return Plus;
  if (isSaveOrUpdateEvent(normalizedEventType)) return iconMap[normalizedEventType] ?? Edit;

  return iconMap[normalizedEventType] ?? InfoInverted;
}

export function getActivityIconTone(eventType: string, result?: string): ActivityIconTone {
  if (result === "failure") return "critical";

  const normalizedEventType = baseEventType(eventType);
  return alertEventTypes.has(normalizedEventType) || isDestructiveEvent(normalizedEventType) ? "critical" : "default";
}
