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
  // null retains an assignment whose uploaded file is unavailable; "" clears it.
  value: string | null;
  // The server snapshots this label when assigning the payload; later catalog
  // metadata edits do not change either the assignment or its option label.
  assignment?: { value: string | null; label: string };
  onChange: (value: string) => void;
  testId: string;
  disabled?: boolean;
}

// Button-styled firmware selector for the model group header: the trigger
// shows the currently selected version and opens a listbox of the available
// versions. The form-field Select is too heavy for this spot.
const FirmwarePickerContent = ({
  label,
  options,
  value,
  assignment,
  onChange,
  testId,
  disabled,
}: FirmwarePickerButtonProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  // Portal to body so the listbox escapes the channel table's overflow.
  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  // Closing for a write lock must also clear the open state, otherwise the
  // old menu returns as soon as the write finishes.
  if (disabled && open) setOpen(false);

  const selected = options.find((option) => option.value === value);
  const selectedLabel = assignment?.value === value ? assignment.label : selected?.label;

  return (
    <div className="relative">
      <div ref={triggerRef}>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text={value === "" ? "No firmware" : selectedLabel || "Firmware details unavailable"}
          suffixIcon={
            <ChevronDown width="w-3" className={clsx("shrink-0 transition-transform", open && "rotate-180")} />
          }
          ariaLabel={label}
          ariaHasPopup="listbox"
          ariaExpanded={open}
          testId={testId}
          disabled={disabled}
          onClick={() => setOpen((current) => !current)}
        />
      </div>
      {open && !disabled ? (
        <Popover
          position={positions["bottom right"]}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeShouldIgnore={(event) =>
            event.target instanceof Node && Boolean(triggerRef.current?.contains(event.target))
          }
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
                  <div className="truncate text-emphasis-300">
                    {option.value !== "" && assignment?.value === option.value ? assignment.label : option.label}
                  </div>
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
