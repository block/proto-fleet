import { useState } from "react";
import Row from "@/shared/components/Row";
import {
  TemperatureUnitsSwitcher,
  ThemeSwitcher,
  usePreferences,
} from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const General = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] =
    useState(false);
  const { theme, temperatureUnits } = usePreferences();

  return (
    <>
      <div className="mx-auto max-w-xl">
        <h3 className="mb-2 text-heading-100">Preferences</h3>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Theme</h4>
          <a
            href="#"
            onClick={(e) => {
              e.preventDefault();
              setShowThemeSwitcher(true);
            }}
            className="text-300 text-intent-warning-fill hover:underline"
          >
            {convertToSentenceCase(theme)}
          </a>
          {showThemeSwitcher && (
            <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} />
          )}
        </Row>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Temperature</h4>
          <a
            href="#"
            onClick={(e) => {
              e.preventDefault();
              setShowTemperatureUnitsSwitcher(true);
            }}
            className="text-300 text-intent-warning-fill hover:underline"
          >
            {convertToSentenceCase(temperatureUnits)}
          </a>
          {showTemperatureUnitsSwitcher && (
            <TemperatureUnitsSwitcher
              onClickDone={() => setShowTemperatureUnitsSwitcher(false)}
            />
          )}
        </Row>
      </div>
    </>
  );
};

export default General;
