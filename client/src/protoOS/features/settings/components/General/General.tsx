import { motion } from "motion/react";
import { useCallback, useEffect, useState } from "react";
import MinerSystemTagEditModal from "./MinerSystemTagEditModal";
import { useSystemTag } from "@/protoOS/api";
import CheckForUpdate from "@/protoOS/features/firmwareUpdate/components/CheckForUpdate";
import {
  useAccessToken,
  useDismissedLoginModal,
  useIsProtoRig,
  usePausedAuthAction,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
  useSetTemperatureUnit,
  useSetTheme,
  useSystemInfo,
  useTemperatureUnit,
  useTheme,
} from "@/protoOS/store";
import { AUTH_ACTIONS } from "@/protoOS/store/types";
import ProtoRigImage from "@/shared/assets/images/ProtoRig.png";
import Button, { sizes, variants } from "@/shared/components/Button";
import Picture from "@/shared/components/Picture";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { TemperatureUnitsSwitcher, ThemeSwitcher } from "@/shared/features/preferences";
import { convertToSentenceCase } from "@/shared/utils/stringUtils";

const General = () => {
  const [showThemeSwitcher, setShowThemeSwitcher] = useState(false);
  const [showTemperatureUnitsSwitcher, setShowTemperatureUnitsSwitcher] = useState(false);
  const [showMinerSystemTagEditModal, setShowMinerSystemTagEditModal] = useState(false);
  const [minerTag, setMinerTag] = useState<string | null>(null);
  const theme = useTheme();
  const setTheme = useSetTheme();
  const temperatureUnit = useTemperatureUnit();
  const setTemperatureUnit = useSetTemperatureUnit();

  const systemInfo = useSystemInfo();
  const isProtoRig = useIsProtoRig();
  const { getSystemTag } = useSystemTag();

  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();
  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const { checkAccess, hasAccess } = useAccessToken(
    pausedAuthAction === AUTH_ACTIONS.systemTag && !dismissedLoginModal,
  );

  useEffect(() => {
    getSystemTag({
      onSuccess: (tag) => setMinerTag(tag),
    });
  }, [getSystemTag]);

  const handleMinerIdClick = useCallback(() => {
    setPausedAuthAction(AUTH_ACTIONS.systemTag);
    checkAccess();
  }, [setPausedAuthAction, checkAccess]);

  // useAccessToken doesn't support callbacks, so we must watch hasAccess to
  // know when auth succeeds (same pattern used by PowerTarget).
  useEffect(() => {
    if (hasAccess && pausedAuthAction === AUTH_ACTIONS.systemTag) {
      setPausedAuthAction(null);
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setShowMinerSystemTagEditModal(true);
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal, setPausedAuthAction]);

  const handleTagSaved = useCallback((tag: string) => {
    setMinerTag(tag);
    setShowMinerSystemTagEditModal(false);
  }, []);

  const hasTag = minerTag !== null && minerTag !== "";

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
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-heading-100">Details</h3>
          {hasTag && (
            <Button
              text="Edit"
              variant={variants.secondary}
              size="compact"
              onClick={handleMinerIdClick}
              testId="edit-details-button"
            />
          )}
        </div>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Model</h4>
          <div className="text-300">{systemInfo?.product_name || <SkeletonBar className="w-20" />}</div>
        </Row>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Miner ID</h4>
          {hasTag ? (
            <div className="text-300">{minerTag}</div>
          ) : (
            <Button
              text="Add"
              textColor="text-text-emphasis"
              variant={variants.textOnly}
              size={sizes.textOnly}
              onClick={handleMinerIdClick}
              testId="add-miner-id"
            />
          )}
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
          <Button
            variant={variants.textOnly}
            onClick={() => setShowThemeSwitcher(true)}
            textColor="text-intent-warning-fill"
            text={convertToSentenceCase(theme)}
          />
          {showThemeSwitcher && (
            <ThemeSwitcher onClickDone={() => setShowThemeSwitcher(false)} theme={theme} setTheme={setTheme} />
          )}
        </Row>
        <Row className="flex justify-between">
          <h4 className="text-emphasis-300">Temperature</h4>
          <Button
            variant={variants.textOnly}
            onClick={() => setShowTemperatureUnitsSwitcher(true)}
            textColor="text-intent-warning-fill"
            text={temperatureUnit === "C" ? "Celsius" : "Fahrenheit"}
            testId="temperature-button"
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
      <MinerSystemTagEditModal
        open={showMinerSystemTagEditModal}
        currentTag={minerTag || ""}
        onDismiss={() => setShowMinerSystemTagEditModal(false)}
        onSaved={handleTagSaved}
      />
    </>
  );
};

export default General;
