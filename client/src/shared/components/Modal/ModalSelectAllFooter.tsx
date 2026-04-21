import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";

interface ModalSelectAllFooterProps {
  label?: string;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
}

const ModalSelectAllFooter = ({ label, onSelectAll, onSelectNone }: ModalSelectAllFooterProps) => {
  return (
    <>
      <Divider className="-mx-6 mt-2 !w-[calc(100%+3rem)]" />
      <div className="flex items-center justify-between pt-5">
        <div className="text-emphasis-300">{label}</div>
        <div className="flex items-center gap-2">
          {onSelectAll && (
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
          )}
          {onSelectNone && (
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
          )}
        </div>
      </div>
    </>
  );
};

export default ModalSelectAllFooter;
