import { motion } from "motion/react";
import { useEffect, useState } from "react";
import { useHashboards, useSystemReboot } from "@/protoOS/api";
import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { useFirmwareUpdate } from "@/protoOS/features/firmwareUpdate/";
import { SettingsSolid } from "@/shared/assets/icons";
import R1Image from "@/shared/assets/images/R1.png";
import R2Image from "@/shared/assets/images/R2.png";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Picture from "@/shared/components/Picture";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import {
  TemperatureUnitsSwitcher,
  ThemeSwitcher,
} from "@/shared/features/preferences";
import usePreferences from "@/shared/features/preferences/hooks/usePreferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";
import { updateStatusToLabel } from "@/shared/utils/utility";

const CHECK_FOR_UPDATES_DELAY = 400; // ms

const General = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] =
    useState(false);
  const [isR2, setIsR2] = useState<boolean>();
  const { theme, temperatureUnits } = usePreferences();
  const { rebootSystem } = useSystemReboot();

  const {
    data: systemInfo,
    reload: reloadSystemInfo,
    pending: systemInfoPending,
  } = useSystemContext();
  const { data: hashboards, pending } = useHashboards();
  const { updateFirmware, status, message, installing } = useFirmwareUpdate();

  const [delayedSystemInfoPending, setDelayedSystemInfoPending] =
    useState(systemInfoPending);

  useEffect(() => {
    let timeout: ReturnType<typeof setTimeout> | undefined;

    if (systemInfoPending) {
      setDelayedSystemInfoPending(true);
    } else {
      // Add a synthetic delay since api returns quickly but we want to show a loading state
      timeout = setTimeout(() => {
        setDelayedSystemInfoPending(false);
      }, CHECK_FOR_UPDATES_DELAY);
    }

    return () => {
      if (timeout) clearTimeout(timeout);
    };
  }, [systemInfoPending]);

  useEffect(() => {
    if (pending || !hashboards || !hashboards.length) {
      return;
    }

    // TODO: Swap this logic with model API when available
    if (hashboards.length > 3) {
      setIsR2(true);
    } else {
      setIsR2(false);
    }
  }, [hashboards, pending]);

  const checkForUpdates = () => {
    reloadSystemInfo();
  };

  const model = systemInfo?.product_name ?? "Proto Rig";

  return (
    <>
      <h2 className="mb-10 text-heading-300">General</h2>
      <div className="mb-10 flex h-68 w-full items-center justify-center rounded-2xl bg-core-primary-5">
        {isR2 !== undefined ? (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.3 }}
          >
            <Picture image={isR2 ? R2Image : R1Image} alt={model} />
            <div className="mt-2 text-center text-heading-100 text-text-primary-50">
              {model}
            </div>
          </motion.div>
        ) : (
          <ProgressCircular indeterminate />
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Miner Details</h3>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Model</h4>
          <div className="text-300">
            {model || <SkeletonBar className="w-20" />}
          </div>
        </Row>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Serial number</h4>
          <div className="text-300">
            {systemInfo?.cb_sn || <SkeletonBar className="w-20" />}
          </div>
        </Row>
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Firmware</h3>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Version</h4>
          <div className="text-300">
            {systemInfo?.os?.version || <SkeletonBar className="w-20" />}
          </div>
        </Row>
        <div className="mt-6 flex justify-center">
          {installing || status === "available" || status === "installed" ? (
            <Header
              title={updateStatusToLabel(status)}
              description={message}
              icon={<SettingsSolid />}
              titleSize="text-emphasis-300"
              inline
              className="w-full items-center rounded-xl bg-surface-base p-3 shadow-100"
              buttons={[
                {
                  text: "Install",
                  variant: "secondary",
                  className: status === "installed" ? "hidden" : "",
                  loading: delayedSystemInfoPending,
                  onClick: () => {
                    updateFirmware();
                  },
                },
                {
                  text: "Reboot",
                  variant: "primary",
                  className: status === "installed" ? "" : "hidden",
                  loading: delayedSystemInfoPending,
                  onClick: () => {
                    rebootSystem();
                  },
                },
              ]}
            />
          ) : (
            <Button
              variant="secondary"
              size="compact"
              loading={delayedSystemInfoPending}
              onClick={() => checkForUpdates()}
            >
              Check for updates
            </Button>
          )}
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
