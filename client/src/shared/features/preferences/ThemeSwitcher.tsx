import { useCallback } from "react";

import { type Theme } from "./types";
import { ThemeDark, ThemeLight, ThemeSystem } from "@/shared/assets/icons";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

interface ThemeSwitcherProps {
  onClickDone: () => void;
  theme: Theme;
  setTheme: (theme: Theme) => void;
}

const ThemeSwitcher = ({ onClickDone, theme, setTheme }: ThemeSwitcherProps) => {
  const handleChange = useCallback(
    (id: string, isSelected: boolean) => {
      const newTheme = id as Theme;
      if (isSelected) {
        setTheme(newTheme);
      }
    },
    [setTheme],
  );

  return (
    <Modal
      title="Theme"
      onDismiss={onClickDone}
      buttons={[
        {
          text: "Done",
          onClick: onClickDone,
          variant: variants.secondary,
        },
      ]}
      size="small"
    >
      <SelectRow
        id={"system"}
        text="System"
        isSelected={theme === "system"}
        onChange={handleChange}
        prefixIcon={
          <div className="rounded-lg bg-surface-5 p-[6px]" data-theme={theme}>
            <ThemeSystem className="text-text-primary-70" />
          </div>
        }
        type={selectTypes.radio}
      />
      <SelectRow
        id={"light"}
        text="Light"
        isSelected={theme === "light"}
        onChange={handleChange}
        prefixIcon={
          <div className="rounded-lg bg-surface-5 p-[6px]" data-theme={"light"}>
            <ThemeLight className="text-text-primary-70" />
          </div>
        }
        type={selectTypes.radio}
      />
      <SelectRow
        id={"dark"}
        text="Dark"
        isSelected={theme === "dark"}
        onChange={handleChange}
        prefixIcon={
          <div className="rounded-lg bg-surface-5 p-[6px]" data-theme={"dark"}>
            <ThemeDark className="text-text-primary-70" />
          </div>
        }
        type={selectTypes.radio}
      />
    </Modal>
  );
};

export default ThemeSwitcher;
