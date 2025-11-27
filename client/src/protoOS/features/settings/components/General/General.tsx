import { motion } from "motion/react";
import { useState } from "react";
import CheckForUpdate from "@/protoOS/features/firmwareUpdate/components/CheckForUpdate";
import {
  useIsProtoRig,
  useSetTemperatureUnit,
  useSetTheme,
  useSystemInfo,
  useTemperatureUnit,
  useTheme,
} from "@/protoOS/store";
import ProtoRigImage from "@/shared/assets/images/ProtoRig.png";
import Picture from "@/shared/components/Picture";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { TemperatureUnitsSwitcher, ThemeSwitcher } from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const General = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] = useState(false);
  const theme = useTheme();
  const setTheme = useSetTheme();
  const temperatureUnit = useTemperatureUnit();
  const setTemperatureUnit = useSetTemperatureUnit();

  const systemInfo = useSystemInfo();
  const isProtoRig = useIsProtoRig();

  return (
    <>
      <h2 className="mb-10 text-heading-300">General</h2>
      <div className="mb-10 flex h-68 w-full items-center justify-center rounded-2xl bg-core-primary-5">
        {isProtoRig && (
          <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.3 }}>
            <Picture image={ProtoRigImage} alt={systemInfo?.product_name} />
            <div className="mt-2 text-center text-heading-100 text-text-primary-50">{systemInfo?.product_name}</div>
          </motion.div>
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Miner Details</h3>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Model</h4>
          <div className="text-300">{systemInfo?.product_name || <SkeletonBar className="w-20" />}</div>
        </Row>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Serial number</h4>
          <div className="text-300">{systemInfo?.cb_sn || <SkeletonBar className="w-20" />}</div>
        </Row>
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Firmware</h3>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Version</h4>
          <div className="text-300">{systemInfo?.os?.version || <SkeletonBar className="w-20" />}</div>
        </Row>
        <div className="mt-6 flex justify-center">
          <CheckForUpdate />
        </div>
      </div>
      <div className="mb-10">
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
    </>
  );
};

export default General;
