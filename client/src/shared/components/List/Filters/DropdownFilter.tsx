import { MouseEvent, useMemo, useRef, useState } from "react";
import { ChevronUpDown, Dismiss } from "@/shared/assets/icons";
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
  allSelectedTitle: string;
  options: DropdownOption[];
  selectedOptions: string[] | undefined;
  size?: keyof typeof sizes;
  onSelect: (value: string) => void;
  onSelectAll: (value: boolean) => void;
};

const DropdownFilter = ({
  title,
  allSelectedTitle = "All selected",
  options,
  selectedOptions,
  size = sizes.compact,
  onSelect,
  onSelectAll,
}: DropdownFilterProps) => {
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const popoverRef = useRef(null);
  const { triggerRef } = usePopover();

  useClickOutside({
    ref: popoverRef,
    onClickOutside: () => setShowPopover(false),
  });

  const selectedLabel = useMemo(() => {
    const selected = options.find((option) =>
      selectedOptions?.includes(option.id),
    );
    return options.length === selectedOptions?.length
      ? allSelectedTitle
      : selected?.label || title;
  }, [options, selectedOptions, allSelectedTitle, title]);

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
  };

  return (
    <>
      {selectedOptions ? (
        <div ref={triggerRef} className="relative min-w-32">
          <div className="flex flex-row items-center gap-2">
            <Button
              textColor="text-text-primary"
              className="min-w-32 text-emphasis-300"
              variant="ghost"
              size={size}
              suffixIcon={<ChevronUpDown />}
              onClick={handleButtonClick}
            >
              {selectedLabel}
            </Button>
            {selectedOptions.length !== options.length &&
              selectedOptions.map((option) => {
                return (
                  <Button
                    variant="accent"
                    prefixIcon={<Dismiss />}
                    key={option}
                    size={size}
                    onClick={(e) => handleOptionClick(option, e)}
                  >
                    {option}
                  </Button>
                );
              })}
          </div>

          {showPopover && (
            <Popover className="!space-y-0 px-0 py-0">
              <div
                ref={popoverRef}
                className="popover-content px-6 py-2"
                onClick={(e) => e.stopPropagation()}
              >
                <div
                  key="all"
                  onClick={() =>
                    onSelectAll(options.length !== selectedOptions.length)
                  }
                >
                  <SelectRow
                    id="all"
                    isSelected={selectedOptions.length === options.length}
                    onChange={() => {}} // Handle in parent onClick instead
                    text={allSelectedTitle}
                    type={selectTypes.checkbox}
                    partiallySelected={
                      selectedOptions.length > 0 &&
                      selectedOptions.length < options.length
                    }
                  />
                </div>
                {options.map((option, index) => (
                  <div
                    key={option.id}
                    onClick={(e) => handleOptionClick(option.id, e)}
                  >
                    <SelectRow
                      id={option.id}
                      isSelected={selectedOptions.includes(option.id)}
                      onChange={() => {}} // Handle in parent onClick instead
                      text={option.label}
                      type={selectTypes.checkbox}
                      divider={index !== options.length - 1}
                    />
                  </div>
                ))}
              </div>
            </Popover>
          )}
        </div>
      ) : (
        <div></div>
      )}
    </>
  );
};

export default DropdownFilter;
