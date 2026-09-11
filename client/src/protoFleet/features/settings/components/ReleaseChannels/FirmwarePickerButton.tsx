import { useEffect, useState } from "react";
import clsx from "clsx";

import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Radio from "@/shared/components/Radio";
import { positions } from "@/shared/constants";

interface FirmwareOption {
  value: string;
  label: string;
  description?: string;
}

interface FirmwarePickerButtonProps {
  // Accessible name for the trigger, e.g. "Firmware for Rig".
  label: string;
  options: FirmwareOption[];
  value: string;
  onChange: (value: string) => void;
  testId: string;
}

// Button-styled firmware selector for the model group header: the trigger
// shows the currently selected version and opens a listbox of the available
// versions. The form-field Select is too heavy for this spot.
const FirmwarePickerContent = ({ label, options, value, onChange, testId }: FirmwarePickerButtonProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  // Portal to body so the listbox escapes the channel table's overflow.
  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  const selected = options.find((option) => option.value === value);

  return (
    <div className="relative">
      <div ref={triggerRef}>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text={selected?.label ?? "No firmware"}
          suffixIcon={
            <ChevronDown width="w-3" className={clsx("shrink-0 transition-transform", open && "rotate-180")} />
          }
          ariaLabel={label}
          ariaHasPopup="listbox"
          ariaExpanded={open}
          testId={testId}
          onClick={() => setOpen((current) => !current)}
        />
      </div>
      {open ? (
        <Popover
          position={positions["bottom right"]}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeIgnoreSelectors={[`[data-testid='${testId}']`]}
        >
          <div
            role="listbox"
            aria-label={`${label} options`}
            className="max-h-80 min-w-64 overflow-y-auto overscroll-contain p-1.5"
          >
            {options.map((option) => (
              <div
                key={option.value}
                role="option"
                aria-selected={value === option.value ? "true" : "false"}
                className={clsx(
                  "flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left select-none",
                  "text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5",
                )}
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
              >
                <Radio selected={value === option.value} />
                <div className="min-w-0 grow">
                  <div className="truncate text-emphasis-300">{option.label}</div>
                  {option.description ? (
                    <div className="text-200 text-text-primary-70">{option.description}</div>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

const FirmwarePickerButton = (props: FirmwarePickerButtonProps) => (
  <PopoverProvider>
    <FirmwarePickerContent {...props} />
  </PopoverProvider>
);

export default FirmwarePickerButton;
export type { FirmwareOption };
