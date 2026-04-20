import { type ReactNode } from "react";

import {
  Alert,
  Edit,
  Fan,
  Groups,
  type IconProps,
  Info,
  LEDIndicator,
  Lock,
  Logs,
  MiningPools,
  Minus,
  Plus,
  Power,
  Racks,
  Reboot,
  Settings,
  Speedometer,
  Trash,
  Unpair,
} from "@/shared/assets/icons";

const iconMap: Record<string, (props: IconProps) => ReactNode> = {
  login: Lock,
  login_failed: Alert,
  logout: Lock,
  update_password: Lock,
  update_username: Lock,
  create_user: Lock,
  deactivate_user: Trash,
  reset_password: Lock,
  create_admin_user: Lock,

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

  create_collection: Groups,
  update_collection: Groups,
  delete_collection: Trash,
  add_devices: Plus,
  remove_devices: Minus,
  set_rack_slot: Racks,
  clear_rack_slot: Racks,
  save_rack: Racks,

  create_pool: MiningPools,
  update_pool: MiningPools,
  delete_pool: Trash,
};

export function getActivityIcon(eventType: string): (props: IconProps) => ReactNode {
  return iconMap[eventType] ?? Info;
}
