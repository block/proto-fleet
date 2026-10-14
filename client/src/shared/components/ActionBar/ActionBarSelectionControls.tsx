import Button, { sizes, variants } from "@/shared/components/Button";

interface ActionBarSelectionControlsProps {
  onSelectAll?: () => void;
  onSelectNone?: () => void;
  visibleOnly?: boolean;
  selectAllTestId?: string;
  selectNoneTestId?: string;
}

const ActionBarSelectionControls = ({
  onSelectAll,
  onSelectNone,
  visibleOnly = false,
  selectAllTestId,
  selectNoneTestId,
}: ActionBarSelectionControlsProps) => (
  <>
    {onSelectAll ? (
      <Button
        className="py-1"
        size={sizes.textOnly}
        variant={variants.textOnly}
        textColor="text-core-accent-fill"
        textOnlyUnderlineOnHover={false}
        testId={selectAllTestId}
        onClick={onSelectAll}
      >
        {visibleOnly ? "Select all visible" : "Select all"}
      </Button>
    ) : null}
    {onSelectNone ? (
      <Button
        className="py-1"
        size={sizes.textOnly}
        variant={variants.textOnly}
        textColor="text-core-accent-fill"
        textOnlyUnderlineOnHover={false}
        testId={selectNoneTestId}
        onClick={onSelectNone}
      >
        Select none
      </Button>
    ) : null}
  </>
);

export default ActionBarSelectionControls;
