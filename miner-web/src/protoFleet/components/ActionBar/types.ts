import { ReactNode } from "react";
import { variants } from "@/shared/components/Button";

export type BulkAction<ActionType> = {
  action: ActionType;
  title: string;
  icon: ReactNode;
  actionHandler: () => void;
  requiresConfirmation: boolean;
  confirmation?: ActionWarnDialogOptions;
};

export type ActionWarnDialogOptions = {
  title: string;
  subtitle: string;
  confirmAction: {
    title: string;
    variant: keyof typeof variants;
  };
  testId: string;
};
