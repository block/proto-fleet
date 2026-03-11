import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import BulkActionsWidget from "./BulkActionsWidget";
import { type BulkAction } from "./types";
import { deviceActions } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import Button, { variants } from "@/shared/components/Button";
import { PopoverProvider } from "@/shared/components/Popover";

describe("BulkActionsWidget", () => {
  test("shows confirmation dialog when a confirmation-requiring quick action is clicked", () => {
    const WidgetHarness = () => {
      const [currentAction, setCurrentAction] = useState<typeof deviceActions.reboot | null>(null);

      const actions: BulkAction<typeof deviceActions.reboot>[] = [
        {
          action: deviceActions.reboot,
          title: "Reboot",
          icon: null,
          actionHandler: () => setCurrentAction(deviceActions.reboot),
          requiresConfirmation: true,
          confirmation: {
            title: "Reboot miners?",
            subtitle: "These miners will reboot.",
            confirmAction: {
              title: "Reboot",
              variant: variants.primary,
            },
            testId: "reboot-confirm-button",
          },
        },
      ];

      return (
        <PopoverProvider>
          <BulkActionsWidget<typeof deviceActions.reboot>
            buttonTitle="More"
            actions={actions}
            currentAction={currentAction}
            onCancel={vi.fn()}
            renderQuickActions={(onAction) => (
              <Button variant={variants.secondary} testId="quick-reboot" onClick={() => onAction(actions[0])}>
                Reboot
              </Button>
            )}
            renderPopover={() => null}
            testId="actions-menu"
          />
        </PopoverProvider>
      );
    };

    render(<WidgetHarness />);

    fireEvent.click(screen.getByTestId("quick-reboot"));

    expect(screen.getByText("Reboot miners?")).toBeInTheDocument();
    expect(screen.getByTestId("reboot-confirm-button")).toBeInTheDocument();
  });
});
