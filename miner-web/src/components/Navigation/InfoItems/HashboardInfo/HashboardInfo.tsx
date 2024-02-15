import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { useClickOutside } from "common/hooks/useClickOutside";
import { getSerialNumbersDisplay } from "common/utils/stringUtils";

import InfoItem from "../InfoItem";

export interface HashboardInfoProps {
  loading?: boolean;
  hashboard_serials?: string[];
}

const HashboardInfo = ({
  loading,
  hashboard_serials = [],
}: HashboardInfoProps) => {
  const [selectedHashboard, setSelectedHashboard] = useState<string>();
  const [hashboardDropdownOpen, setHashboardDropdownOpen] = useState(false);
  const hashboardDropdownRef = useRef<HTMLDivElement>(null);
  const serials = useMemo(
    () => getSerialNumbersDisplay(hashboard_serials) || [],
    [hashboard_serials]
  );

  const toggleHashboardDropdown = useCallback(() => {
    setHashboardDropdownOpen(!hashboardDropdownOpen);
  }, [hashboardDropdownOpen]);

  const selectHashboard = useCallback((serial: string) => {
    setSelectedHashboard(serial);
    setHashboardDropdownOpen(false);
  }, []);

  useEffect(() => {
    if (!selectedHashboard && serials.length) {
      setSelectedHashboard(serials[0]);
    }
  }, [serials, selectedHashboard]);

  const selectedHashboardLabel = useMemo(() => {
    if (selectedHashboard) {
      return serials.findIndex((serial) => serial === selectedHashboard) + 1;
    }
    return "";
  }, [serials, selectedHashboard]);

  const shouldShowHashboardDropdown = useMemo(() => {
    return serials.length > 1;
  }, [serials]);

  const onClickOutside = useCallback(() => {
    setHashboardDropdownOpen(false);
  }, []);

  useClickOutside({ ref: hashboardDropdownRef, onClickOutside });
  return (
    <div className="relative">
      <InfoItem
        caret={shouldShowHashboardDropdown}
        handleClick={
          shouldShowHashboardDropdown ? toggleHashboardDropdown : undefined
        }
        label={`Hashboard ${selectedHashboardLabel} Serial`}
        value={selectedHashboard}
        loading={loading}
      />

      {hashboardDropdownOpen && (
        <div
          ref={hashboardDropdownRef}
          className="w-[232px] bg-surface-5 p-4 rounded-md shadow-lg absolute z-10 top-5 -left-1 text-200"
        >
          {serials.map((serial, index) => (
            <div
              className={clsx(
                "hover:cursor-pointer rounded-md px-2 h-[33px] flex flex-col relative justify-center",
                {
                  "bg-core-accent-fill/10": serial === selectedHashboard,
                  "hover:bg-core-accent-fill/5": serial !== selectedHashboard,
                }
              )}
              key={serial}
              onClick={() => serial && selectHashboard(serial)}
            >
              Hashboard {index + 1}
              {/* only add this bottom border if not selected item and not one before the selected item */}
              {serial !== selectedHashboard &&
                selectedHashboard !== serials[index + 1] && (
                  <div className="border-b border-border-primary/5 w-full absolute bottom-0 left-0" />
                )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default HashboardInfo;
