import { createPortal } from "react-dom";
import ActionBar from "@/shared/components/ActionBar";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";

const ModuleActionBar = ({
  selected,
  clear,
  actions,
  hidden,
}: {
  selected: string[];
  clear: () => void;
  actions: RowAction[];
  hidden: boolean;
}) =>
  createPortal(
    <ActionBar
      className={`fixed right-0 bottom-4 left-0 z-30 laptop:left-50 ${hidden ? "invisible" : ""}`}
      selectedItems={selected}
      itemNoun={{ singular: "module", plural: "modules" }}
      onClose={clear}
      renderActions={() => (
        <RowActionsMenu
          actions={actions}
          ariaLabel="Actions for selected modules"
          triggerLabel="Actions"
          triggerVariant="secondary"
          triggerClassName="bg-grayscale-white-10! text-grayscale-white-90!"
        />
      )}
    />,
    document.body,
  );

export default ModuleActionBar;
