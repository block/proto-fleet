import { useState } from "react";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useSetTemperatureUnit, useSetTheme, useTemperatureUnit, useTheme } from "@/protoFleet/store";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import { TemperatureUnitsSwitcher, ThemeSwitcher } from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";
import { buildVersionInfo } from "@/shared/utils/version";

const PREFERENCES_PAGE_DESCRIPTION = "Manage display and temperature preferences for your account.";

const Preferences = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] = useState(false);
  const theme = useTheme();
  const setTheme = useSetTheme();
  const temperatureUnit = useTemperatureUnit();
  const setTemperatureUnit = useSetTemperatureUnit();

  return (
    <>
      <div className="flex flex-col gap-6">
        <SettingsPageHeader title="Preferences" description={PREFERENCES_PAGE_DESCRIPTION} />
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Display" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Theme</div>
                <Button
                  variant="textOnly"
                  onClick={() => setShowThemeSwitcher(true)}
                  textColor="text-intent-warning-fill"
                  text={convertToSentenceCase(theme)}
                />
                {showThemeSwitcher ? (
                  <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} theme={theme} setTheme={setTheme} />
                ) : null}
              </Row>
              <Row className="flex justify-between" divider={false}>
                <div className="text-300">Temperature</div>
                <Button
                  variant="textOnly"
                  testId="temperature-button"
                  onClick={() => setShowTemperatureUnitsSwitcher(true)}
                  textColor="text-intent-warning-fill"
                  text={temperatureUnit === "C" ? "Celsius" : "Fahrenheit"}
                />
                {showTemperatureUnitsSwitcher ? (
                  <TemperatureUnitsSwitcher
                    onClickDone={() => setShowTemperatureUnitsSwitcher(false)}
                    temperatureUnit={temperatureUnit}
                    setTemperatureUnit={setTemperatureUnit}
                  />
                ) : null}
              </Row>
            </div>
          </div>
        </div>
        <p className="text-300 text-text-primary-50">Proto Fleet {buildVersionInfo.version}</p>
      </div>
    </>
  );
};

export default Preferences;
