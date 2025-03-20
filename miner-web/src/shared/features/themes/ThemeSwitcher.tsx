import { useCallback, useState } from "react";

import { themes } from "./constants";
import { useThemeContext } from "./hooks/useThemeContext";
import { Themes } from "./types";
import { ThemeDark, ThemeLight, ThemeSystem } from "@/shared/assets/icons";

import { variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import { popoverSizes } from "@/shared/components/Popover";
import PopoverContent from "@/shared/components/Popover/PopoverContent.tsx";
import SelectRow, { selectTypes } from "@/shared/components/SelectRow";

interface ThemeSwitcherProps {
  onClickDone: () => void;
}

const ThemeSwitcher = ({ onClickDone }: ThemeSwitcherProps) => {
  const { deviceTheme, getUserSelectedTheme, setUserSelectedTheme } =
    useThemeContext();
  const [selectedTheme, setSelectedTheme] = useState<Themes>(
    getUserSelectedTheme(),
  );

  const handleChange = useCallback(
    (id: string, isSelected: boolean) => {
      const theme = id as Themes;
      if (isSelected) {
        setSelectedTheme(theme);
        setUserSelectedTheme(theme);
      }
    },
    [setUserSelectedTheme],
  );

  // TODO should be modal instead of Popover
  return (
    <PageOverlay show>
      <PopoverContent
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
            id={themes.light}
            text="Light"
            isSelected={selectedTheme === themes.light}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={themes.light}
              >
                <ThemeLight className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
          <SelectRow
            id={themes.dark}
            text="Dark"
            isSelected={selectedTheme === themes.dark}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={themes.dark}
              >
                <ThemeDark className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
          <SelectRow
            id={themes.system}
            text="Device default"
            isSelected={selectedTheme === themes.system}
            onChange={handleChange}
            prefixIcon={
              <div
                className="rounded-lg bg-surface-5 p-[6px]"
                data-theme={deviceTheme}
              >
                <ThemeSystem className="text-text-primary-70" />
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
