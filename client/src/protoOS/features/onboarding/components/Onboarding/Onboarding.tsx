import { useCallback, useState } from "react";
import clsx from "clsx";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import AppLayout from "@/protoOS/components/AppLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import MiningPools, { getEmptyPoolsInfo, isValidPool, PoolInfo } from "@/protoOS/components/MiningPools";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import OnboardingHeader from "@/protoOS/components/OnboardingHeader";
import SettingUp from "@/protoOS/components/OnboardingSettingUp";
import { useAccessToken } from "@/protoOS/store";
import { useDismissedLoginModal, useSetDismissedLoginModal } from "@/protoOS/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import { WarnBackupPoolDialog } from "@/shared/components/MiningPools/WarnBackupPoolDialog";
import { WarnDefaultPoolCallout } from "@/shared/components/MiningPools/WarnDefaultPoolCallout";

const Onboarding = () => {
  const [settingUpMiner, setSettingUpMiner] = useState(false);
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);

  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const [pausedAction, setPausedAction] = useState(false);
  const [waitingForAuth, setWaitingForAuth] = useState(false);

  const [createPoolsError, setCreatePoolsError] = useState<ErrorProps>();
  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(pausedAction);

  // Resume paused onboarding action once auth becomes available
  const [prevHasAccess, setPrevHasAccess] = useState(hasAccess);
  const [prevPausedAction, setPrevPausedAction] = useState(pausedAction);
  const [prevWaitingForAuth, setPrevWaitingForAuth] = useState(waitingForAuth);
  if (prevHasAccess !== hasAccess || prevPausedAction !== pausedAction || prevWaitingForAuth !== waitingForAuth) {
    setPrevHasAccess(hasAccess);
    setPrevPausedAction(pausedAction);
    setPrevWaitingForAuth(waitingForAuth);
    if (hasAccess && pausedAction && waitingForAuth) {
      setPausedAction(false);
      // have to reset the error here, otherwise it would cause an infinite cycle
      setCreatePoolsError(undefined);
      setSettingUpMiner(true);
    }
  }

  // Pause setup flow and surface login modal when backend responds with auth error
  const errorStatus = createPoolsError?.status;
  const [prevErrorStatus, setPrevErrorStatus] = useState(errorStatus);
  const [prevSettingUpMiner, setPrevSettingUpMiner] = useState(settingUpMiner);
  if (prevErrorStatus !== errorStatus || prevSettingUpMiner !== settingUpMiner) {
    setPrevErrorStatus(errorStatus);
    setPrevSettingUpMiner(settingUpMiner);
    if (settingUpMiner && (errorStatus === 401 || errorStatus === 422)) {
      if (errorStatus === 401) {
        setHasAccess(false);
      }
      setSettingUpMiner(false);
      setPausedAction(true);
    }
    setWaitingForAuth(false);
  }

  // Abandon paused action when user dismisses login modal
  const [prevDismissedLoginModal, setPrevDismissedLoginModal] = useState(dismissedLoginModal);
  if (prevDismissedLoginModal !== dismissedLoginModal) {
    setPrevDismissedLoginModal(dismissedLoginModal);
    if (dismissedLoginModal) {
      setPausedAction(false);
      setDismissedLoginModal(false);
    }
  }

  const onContinue = useCallback(
    (ignoreBackupPools?: boolean) => {
      // check if default pool has been entered
      const noValidDefaultPool = !isValidPool(pools[0]);
      if (noValidDefaultPool) {
        setWarnDefaultPool(true);
        return;
      }
      // ignore backup pools if indicated by the user
      if (!ignoreBackupPools) {
        // check if at least one backup pool has been entered
        const noValidBackupPool = !isValidPool(pools[1]) && !isValidPool(pools[2]);
        if (noValidBackupPool) {
          setWarnBackupPool(true);
          return;
        }
      }
      setPausedAction(true);
      setWaitingForAuth(true);
      checkAccess();
    },
    [pools, checkAccess],
  );

  const onContinueWithoutBackup = useCallback(() => {
    setWarnBackupPool(false);
    onContinue(true);
  }, [onContinue]);

  const onChangePools = useCallback((newPools: PoolInfo[]) => {
    setPools(newPools);
    if (isValidPool(newPools[0])) {
      setWarnDefaultPool(false);
    }
  }, []);

  if (settingUpMiner) {
    return (
      <div className="bg-surface-base">
        <OnboardingHeader />
        <div className="flex h-screen items-center justify-center">
          <div className="w-[600px]">
            <SettingUp
              pools={pools}
              setCreatePoolsError={setCreatePoolsError}
              onChangeSettingUpMiner={setSettingUpMiner}
            />
          </div>
        </div>
      </div>
    );
  }

  return (
    <AppLayout
      customHeaderButtons={
        <Button
          onClick={() => onContinue()}
          size={sizes.compact}
          variant={variants.primary}
          testId="finish-setup-button"
          text="Finish setup"
        />
      }
      title="Miner setup"
      type={navigationMenuTypes.onboarding}
      ContentLayout={SettingsContentLayout}
    >
      <WarnBackupPoolDialog
        open={warnBackupPool}
        onAddBackupPool={() => setWarnBackupPool(false)}
        onContinueWithoutBackup={onContinueWithoutBackup}
      />
      <MiningPools title="Add your mining pool" onChange={onChangePools} pools={pools}>
        <WarnDefaultPoolCallout onDismiss={() => setWarnDefaultPool(false)} show={warnDefaultPool} />
        <DismissibleCalloutWrapper
          className={clsx({
            "mb-10!": createPoolsError?.error?.message !== undefined,
          })}
          icon={<Alert />}
          // TODO intent here has no effect, because callout doesn't have a header
          intent={intents.danger}
          show={createPoolsError?.error?.message !== undefined}
          title={createPoolsError?.error?.message ?? "An error occurred"}
          onDismiss={() => setCreatePoolsError(undefined)}
        />
      </MiningPools>
    </AppLayout>
  );
};

export default Onboarding;
