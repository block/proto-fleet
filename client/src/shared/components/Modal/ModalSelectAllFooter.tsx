import Button, { sizes, variants } from "@/shared/components/Button";

interface ModalSelectAllFooterProps {
  label?: string;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
}

const ModalSelectAllFooter = ({ label, onSelectAll, onSelectNone }: ModalSelectAllFooterProps) => {
  return (
    <div className="-mx-6 -mb-6 shrink-0 border-t border-border-5 bg-surface-elevated-base px-6 pt-5 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0 truncate text-emphasis-300">{label}</div>
        <div className="flex shrink-0 items-center gap-2">
          {onSelectAll ? (
            <Button
              className="py-1"
              size={sizes.textOnly}
              variant={variants.textOnly}
              textColor="text-core-accent-fill"
              textOnlyUnderlineOnHover={false}
              onClick={onSelectAll}
            >
              Select all
            </Button>
          ) : null}
          {onSelectNone ? (
            <Button
              className="py-1"
              size={sizes.textOnly}
              variant={variants.textOnly}
              textColor="text-core-accent-fill"
              textOnlyUnderlineOnHover={false}
              onClick={onSelectNone}
            >
              Select none
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  );
};

export default ModalSelectAllFooter;
