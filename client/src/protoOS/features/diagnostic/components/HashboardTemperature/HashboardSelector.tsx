import { useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ChevronUpDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Popover, { usePopover } from "@/shared/components/Popover";
import SelectRow from "@/shared/components/SelectRow";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type HashboardSelectorProps = {
  hashboardList: { serial: string; name: string }[];
  currentHashboard?: string;
};

const HashboardSelector = ({ hashboardList, currentHashboard }: HashboardSelectorProps) => {
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const popoverRef = useRef(null);
  const { triggerRef } = usePopover();
  const navigate = useNavigate();
  useClickOutside({
    ref: popoverRef,
    onClickOutside: () => setShowPopover(false),
  });

  const currentHashboardName = useMemo(() => {
    const current = hashboardList.find((hashboard) => hashboard.serial === currentHashboard);
    return current?.name;
  }, [currentHashboard, hashboardList]);

  return (
    <div ref={triggerRef}>
      <Button
        textColor="text-text-primary"
        className="!p-0 !text-heading-300 font-medium"
        variant="textOnly"
        suffixIcon={currentHashboardName ? <ChevronUpDown /> : undefined}
        onClick={() => setShowPopover((prev) => !prev)}
      >
        {currentHashboardName || <SkeletonBar className="w-25 text-heading-300" />}
      </Button>

      {showPopover && (
        <Popover className="!space-y-0 px-0 py-0">
          <div ref={popoverRef} className="px-6 py-2">
            {hashboardList.map((hashboard, index) => (
              <SelectRow
                key={hashboard.serial}
                id={hashboard.serial}
                isSelected={currentHashboard === hashboard.serial}
                onChange={() => {
                  navigate(`../${hashboard.serial}`, {
                    replace: true,
                    relative: "path",
                  });
                  setShowPopover(false);
                }}
                text={hashboard.name}
                type="radio"
                divider={index !== hashboardList.length - 1}
              />
            ))}
          </div>
        </Popover>
      )}
    </div>
  );
};

export default HashboardSelector;
