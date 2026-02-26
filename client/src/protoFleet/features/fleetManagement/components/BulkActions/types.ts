import { ReactNode } from "react";
import { UnsupportedMinerGroup } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { type ButtonVariant } from "@/shared/components/Button";

export type BulkAction<ActionType> = {
  action: ActionType;
  title: string;
  icon: ReactNode;
  actionHandler: () => void;
  requiresConfirmation: boolean;
  confirmation?: ActionWarnDialogOptions;
  /** Shows a thicker divider after this action to separate groups */
  showGroupDivider?: boolean;
};

export type ActionWarnDialogOptions = {
  title: string;
  subtitle: string;
  confirmAction: {
    title: string;
    variant: ButtonVariant;
  };
  testId: string;
};

export type UnsupportedMinersInfo = {
  visible: boolean;
  unsupportedGroups: UnsupportedMinerGroup[];
  totalUnsupportedCount: number;
  noneSupported: boolean;
  supportedDeviceIdentifiers: string[];
};
