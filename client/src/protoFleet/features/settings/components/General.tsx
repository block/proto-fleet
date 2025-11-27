import { useState } from "react";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useSetTemperatureUnit, useSetTheme, useTemperatureUnit, useTheme } from "@/protoFleet/store";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { TemperatureUnitsSwitcher, ThemeSwitcher } from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;

const General = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] = useState(false);
  const theme = useTheme();
  const setTheme = useSetTheme();
  const temperatureUnit = useTemperatureUnit();
  const setTemperatureUnit = useSetTemperatureUnit();
  const { data: networkInfo } = useNetworkInfo();

  return (
    <>
      <div className="mx-auto flex max-w-xl flex-col gap-5">
        <div>
          <h3 className="mb-2 text-heading-100">Network</h3>
          <Row className="flex justify-between">
            <div>Gateway</div>
            <div>{networkInfo?.gateway ?? SkeletonLoader}</div>
          </Row>
          <Row divider={false} className="flex justify-between">
            <div>Subnet mask</div>
            <div>{networkInfo?.subnet ?? SkeletonLoader}</div>
          </Row>
        </div>
        <div>
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
              <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} theme={theme} setTheme={setTheme} />
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
              {temperatureUnit === "C" ? "Celsius" : "Fahrenheit"}
            </a>
            {showTemperatureUnitsSwitcher && (
              <TemperatureUnitsSwitcher
                onClickDone={() => setShowTemperatureUnitsSwitcher(false)}
                temperatureUnit={temperatureUnit}
                setTemperatureUnit={setTemperatureUnit}
              />
            )}
          </Row>
        </div>
      </div>
    </>
  );
};

export default General;
