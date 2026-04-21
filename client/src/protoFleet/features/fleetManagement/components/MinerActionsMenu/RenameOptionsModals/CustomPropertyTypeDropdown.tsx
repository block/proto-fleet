import { useState } from "react";
import clsx from "clsx";

import { type CustomPropertyType, customPropertyTypeLabels } from "./types";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";

const propertyTypeOptions = Object.entries(customPropertyTypeLabels) as [CustomPropertyType, string][];

interface TypeDropdownContentProps {
  selectedType: CustomPropertyType;
  onChange: (nextType: CustomPropertyType) => void;
}

const TypeDropdownContent = ({ selectedType, onChange }: TypeDropdownContentProps) => {
  const [showTypeOptions, setShowTypeOptions] = useState(false);
  const { triggerRef } = usePopover();

  const closeOptions = () => {
    setShowTypeOptions(false);
  };

  const selectType = (nextType: CustomPropertyType) => {
    onChange(nextType);
    closeOptions();
  };

  return (
    <div ref={triggerRef} className="relative">
      <Button
        ariaLabel="Custom property type"
        ariaHasPopup="listbox"
        ariaExpanded={showTypeOptions}
        variant={variants.secondary}
        className={clsx(
          "!h-14 !w-full !justify-between !rounded-xl border border-border-5 !bg-surface-base !px-4 !py-1 text-left shadow-none",
          {
            "!border-border-20 !ring-4 !ring-core-primary-5": showTypeOptions,
          },
        )}
        onClick={() => setShowTypeOptions((previousValue) => !previousValue)}
        suffixIcon={
          <ChevronDown
            width="w-3"
            className={clsx("text-text-primary-70 transition-transform", {
              "rotate-180": showTypeOptions,
            })}
          />
        }
        testId="custom-property-type-button"
      >
        <div className="flex flex-col items-start">
          <span className="text-200 text-text-primary-50">Type</span>
          <span
            className={clsx("text-300", {
              "text-text-primary-50": showTypeOptions,
              "text-text-primary": !showTypeOptions,
            })}
          >
            {customPropertyTypeLabels[selectedType]}
          </span>
        </div>
      </Button>
      {showTypeOptions ? (
        <Popover
          position={positions["bottom right"]}
          className="!w-[240px] !space-y-0 !rounded-3xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={closeOptions}
          closeIgnoreSelectors={["[data-testid='custom-property-type-button']"]}
        >
          <div className="px-6 py-2" role="listbox" aria-label="Custom property type options">
            {propertyTypeOptions.map(([optionValue, optionLabel], index) => {
              const isLastOption = index === propertyTypeOptions.length - 1;
              const isSelected = selectedType === optionValue;

              return (
                <Row
                  key={optionValue}
                  onClick={() => selectType(optionValue)}
                  divider={!isLastOption}
                  testId={`custom-property-type-option-${optionValue}`}
                  attributes={{ role: "option", "aria-selected": isSelected ? "true" : "false" }}
                  compact
                  className="text-300 text-text-primary"
                >
                  {optionLabel}
                </Row>
              );
            })}
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

interface CustomPropertyTypeDropdownProps {
  selectedType: CustomPropertyType;
  onChange: (nextType: CustomPropertyType) => void;
}

const CustomPropertyTypeDropdown = ({ selectedType, onChange }: CustomPropertyTypeDropdownProps) => {
  return (
    <PopoverProvider>
      <TypeDropdownContent selectedType={selectedType} onChange={onChange} />
    </PopoverProvider>
  );
};

export default CustomPropertyTypeDropdown;
