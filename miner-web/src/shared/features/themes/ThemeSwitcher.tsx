import { useCallback, useState } from "react";

import { themes } from "./constants";
import { useThemeContext } from "./hooks/useThemeContext";
import { Themes } from "./types";
import { ThemeDark, ThemeLight, ThemeSystem } from "@/shared/assets/icons";

import { variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import Popover, { popoverSizes } from "@/shared/components/Popover";
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

  return (
    <PageOverlay show>
      <Popover
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
                className="bg-surface-5 p-[6px] rounded-lg"
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
                className="bg-surface-5 p-[6px] rounded-lg"
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
                className="bg-surface-5 p-[6px] rounded-lg"
                data-theme={deviceTheme}
              >
                <ThemeSystem className="text-text-primary-70" />
              </div>
            }
            type={selectTypes.radio}
          />
        </div>
      </Popover>
    </PageOverlay>
  );
};

export default ThemeSwitcher;
