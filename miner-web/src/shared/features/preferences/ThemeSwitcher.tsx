import { useCallback } from "react";

import { THEMES } from "./constants";
import usePreferences from "./hooks/usePreferences";
import { Themes } from "./types";
import { ThemeDark, ThemeLight, ThemeSystem } from "@/shared/assets/icons";

import { variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import { popoverSizes } from "@/shared/components/Popover";
import PopoverContent from "@/shared/components/Popover/PopoverContent.tsx";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

interface ThemeSwitcherProps {
  onClickDone: () => void;
}

const ThemeSwitcher = ({ onClickDone }: ThemeSwitcherProps) => {
  const { theme, setTheme } = usePreferences();

  const handleChange = useCallback(
    (id: string, isSelected: boolean) => {
      const theme = id as Themes;
      if (isSelected) {
        setTheme(theme);
      }
    },
    [setTheme],
  );

  // TODO should be modal instead of Popover
  return (
    <PageOverlay show>
      <PopoverContent
        closePopover={onClickDone}
        title="Theme"
        buttons={[
          {
            text: "Done",
            onClick: onClickDone,
            variant: variants.secondary,
          },
        ]}
        titleSize="text-heading-100"
        size={popoverSizes.medium}
      >
        <div className="-mt-3">
          <SelectRow
            id={THEMES.system}
            text="System"
            isSelected={theme === THEMES.system}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={theme}
              >
                <ThemeSystem className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
          <SelectRow
            id={THEMES.light}
            text="Light"
            isSelected={theme === THEMES.light}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={THEMES.light}
              >
                <ThemeLight className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
          <SelectRow
            id={THEMES.dark}
            text="Dark"
            isSelected={theme === THEMES.dark}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={THEMES.dark}
              >
                <ThemeDark className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
        </div>
      </PopoverContent>
    </PageOverlay>
  );
};

export default ThemeSwitcher;
