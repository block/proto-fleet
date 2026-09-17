import ActionBarComponent from ".";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

export const ActionBar = ({ numberOfItems }: { numberOfItems: number }) => (
  <ActionBarComponent
    className="fixed right-0 bottom-4 left-0 z-20"
    selectedItems={Array.from({ length: numberOfItems }, (_, index) => `item-${index}`)}
    itemNoun={{ singular: "item", plural: "items" }}
    renderActions={(setHidden) => (
      <RowActionsMenu
        ariaLabel="Selected item actions"
        triggerLabel="Actions"
        triggerVariant="secondary"
        actions={[{ label: "Run action", onClick: () => setHidden(true) }]}
      />
    )}
  />
);

export default {
  title: "Shared/Action Bar",
  args: { numberOfItems: 1 },
  argTypes: { numberOfItems: { control: { type: "range", min: 1, max: 25, step: 1 } } },
};
