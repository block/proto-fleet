import { useState } from "react";
import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { useSetTemperatureUnit, useSetTheme, useTemperatureUnit, useTheme } from "@/protoFleet/store";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { TemperatureUnitsSwitcher, ThemeSwitcher } from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";
import { buildVersionInfo } from "@/shared/utils/version";

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
      <div className="flex flex-col gap-6">
        <Header title="General" titleSize="text-heading-300" />
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Network details" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Subnet mask</div>
                <div className="text-300">{networkInfo?.subnet ?? SkeletonLoader}</div>
              </Row>
              <Row className="flex justify-between" divider={false}>
                <div className="text-300">Gateway</div>
                <div className="text-300">{networkInfo?.gateway ?? SkeletonLoader}</div>
              </Row>
            </div>
          </div>
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Preferences" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Theme</div>
                <Button
                  variant="textOnly"
                  onClick={() => setShowThemeSwitcher(true)}
                  textColor="text-intent-warning-fill"
                  text={convertToSentenceCase(theme)}
                />
                {showThemeSwitcher && (
                  <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} theme={theme} setTheme={setTheme} />
                )}
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
        </div>
        <p className="text-300 text-text-primary-50">Proto Fleet {buildVersionInfo.version}</p>
      </div>
    </>
  );
};

export default General;
