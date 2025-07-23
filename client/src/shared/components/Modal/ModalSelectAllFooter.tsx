import Button, { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";

interface ModalSelectAllFooterProps {
  label?: string;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
}

const ModalSelectAllFooter = ({
  label,
  onSelectAll,
  onSelectNone,
}: ModalSelectAllFooterProps) => {
  return (
    <>
      <Divider className="-mx-6 mt-2 !w-[calc(100%+3rem)]" />
      <div className="flex items-center justify-between pt-5">
        <div className="text-emphasis-300">{label}</div>
        <div className="flex items-center gap-3">
          {onSelectAll && (
            <Button
              variant={variants.textOnly}
              className="pr-0"
              onClick={onSelectAll}
            >
              Select all
            </Button>
          )}
          {onSelectNone && (
            <Button
              variant={variants.textOnly}
              className="pl-0"
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
