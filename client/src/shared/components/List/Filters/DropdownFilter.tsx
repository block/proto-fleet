import { MouseEvent, useMemo, useRef, useState } from "react";
import { ChevronUpDown } from "@/shared/assets/icons";
import Button, { sizes } from "@/shared/components/Button";
import Popover, { usePopover } from "@/shared/components/Popover";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

export type DropdownOption = {
  id: string;
  label: string;
};

type DropdownFilterProps = {
  title: string;
  options: DropdownOption[];
  selectedOption?: string;
  size?: keyof typeof sizes;
  onSelect: (value: string) => void;
};

const DropdownFilter = ({
  title,
  options,
  selectedOption,
  size = sizes.compact,
  onSelect,
}: DropdownFilterProps) => {
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const popoverRef = useRef(null);
  const { triggerRef } = usePopover();

  useClickOutside({
    ref: popoverRef,
    onClickOutside: () => setShowPopover(false),
  });

  const selectedLabel = useMemo(() => {
    const selected = options.find((option) => option.id === selectedOption);
    return selected?.label || title;
  }, [selectedOption, options, title]);

  // Prevent event bubbling to parent components
  const handleButtonClick = (e: MouseEvent) => {
    e.stopPropagation();
    setShowPopover((prev) => !prev);
  };

  // Prevent click from bubbling up
  const handleOptionClick = (optionId: string, e: MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    onSelect(optionId);
    setShowPopover(false);
  };

  return (
    <div ref={triggerRef} className="relative min-w-32">
      <Button
        textColor="text-text-primary"
        className="mr-2 min-w-32 text-emphasis-300"
        variant="ghost"
        size={size}
        suffixIcon={<ChevronUpDown />}
        onClick={handleButtonClick}
      >
        {selectedLabel}
      </Button>

      {showPopover && (
        <Popover className="!space-y-0 px-0 py-0">
          <div
            ref={popoverRef}
            className="popover-content px-6 py-2"
            onClick={(e) => e.stopPropagation()}
          >
            {options.map((option, index) => (
              <div
                key={option.id}
                onClick={(e) => handleOptionClick(option.id, e)}
              >
                <SelectRow
                  id={option.id}
                  isSelected={selectedOption === option.id}
                  onChange={() => {}} // Handle in parent onClick instead
                  text={option.label}
                  type={selectTypes.radio}
                  divider={index !== options.length - 1}
                />
              </div>
            ))}
          </div>
        </Popover>
      )}
    </div>
  );
};

export default DropdownFilter;
