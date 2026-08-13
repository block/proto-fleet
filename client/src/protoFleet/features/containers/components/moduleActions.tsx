import type { ReactNode } from "react";

import { ArrowRight, LEDIndicator, Power, Reboot } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

/**
 * The module-level actions surfaced from a tank module tile (and, later, the
 * module-detail header). Deliberately a small set: a navigation action plus the
 * three device actions that already exist in the fleet action model.
 *
 * - `view` opens the module detail.
 * - `blink` / `reboot` / `sleep` map 1:1 onto the existing fleet
 *   `deviceActions` (`blink-leds` / `reboot` / `shutdown`). "Sleep" is the
 *   product name for the shutdown/stop-mining action (its copy already reads
 *   "Put to sleep"), which is why there is no net-new `isolate` action or icon.
 */
export type ModuleActionType = "view" | "blink" | "reboot" | "sleep";

export interface ModuleAction {
  type: ModuleActionType;
  label: string;
  icon: ReactNode;
  onClick: () => void;
  /** Thick divider rendered below the row; suppressed on the last row. */
  showGroupDivider?: boolean;
  testId?: string;
}

/**
 * Build the ordered module action menu. `View` leads and is separated from the
 * device actions by a group divider — matching the shared RowActionsMenu
 * convention of grouping a navigation action away from device commands. Icons
 * reuse the exact glyphs the fleet miner-actions menu uses for the same
 * actions (ArrowRight for view, LEDIndicator for blink, Reboot, Power for
 * sleep), so nothing bespoke is introduced.
 */
export function buildModuleActions(onAction: (type: ModuleActionType) => void): ModuleAction[] {
  return [
    {
      type: "view",
      label: "View",
      icon: <ArrowRight width={iconSizes.small} className="text-text-primary" />,
      onClick: () => onAction("view"),
      showGroupDivider: true,
      testId: "module-action-view",
    },
    {
      type: "blink",
      label: "Blink LEDs",
      icon: <LEDIndicator width={iconSizes.small} />,
      onClick: () => onAction("blink"),
      testId: "module-action-blink",
    },
    {
      type: "reboot",
      label: "Reboot",
      icon: <Reboot width={iconSizes.small} />,
      onClick: () => onAction("reboot"),
      testId: "module-action-reboot",
    },
    {
      type: "sleep",
      label: "Sleep",
      icon: <Power width={iconSizes.small} />,
      onClick: () => onAction("sleep"),
      testId: "module-action-sleep",
    },
  ];
}
