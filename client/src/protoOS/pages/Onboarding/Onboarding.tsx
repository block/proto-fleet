import { useCallback, useEffect, useState } from "react";

import { WarnBackupPoolDialog } from "./WarnBackupPoolDialog";
import { WarnDefaultPoolCallout } from "./WarnDefaultPoolCallout";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import {
  NetworkInfoNetworkinfo,
  SystemInfoSysteminfo,
} from "@/protoOS/api/types";

import AppLayout from "@/protoOS/components/AppLayout";
import MiningPools, {
  getEmptyPoolsInfo,
  isValidPool,
  PoolInfo,
} from "@/protoOS/components/MiningPools";
import { navigationMenuTypes } from "@/protoOS/components/NavigationMenu";
import OnboardingHeader from "@/protoOS/components/OnboardingHeader";
import SettingUp from "@/protoOS/components/OnboardingSettingUp";
import { useAccessToken, useAuthContext } from "@/protoOS/contexts/AuthContext";
import Button, { sizes, variants } from "@/shared/components/Button";

interface OnboardingProps {
  networkInfo?: NetworkInfoNetworkinfo;
  onChangeSettingUpMiner: (settingUpMiner: boolean) => void;
  pendingNetworkInfo: boolean;
  pendingSystemInfo: boolean;
  settingUpMiner: boolean;
  systemInfo?: SystemInfoSysteminfo;
}

const Onboarding = ({
  networkInfo,
  onChangeSettingUpMiner,
  pendingNetworkInfo,
  pendingSystemInfo,
  settingUpMiner,
  systemInfo,
}: OnboardingProps) => {
  const [pools, setPools] = useState<PoolInfo[]>(getEmptyPoolsInfo());

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);

  const {
    dismissedLoginModal,
    setDismissedLoginModal,
    showLoginModal,
    setShowLoginModal,
  } = useAuthContext();
  const [pausedAction, setPausedAction] = useState(false);

  const [createPoolsError, setCreatePoolsError] = useState<ErrorProps>();
  const { checkAccess, hasAccess, setHasAccess } = useAccessToken(pausedAction);

  useEffect(() => {
    if (hasAccess && pausedAction) {
      setPausedAction(false);
      onChangeSettingUpMiner(true);
    }
  }, [hasAccess, pausedAction, onChangeSettingUpMiner]);

  useEffect(() => {
    if (settingUpMiner && createPoolsError?.status === 401) {
      setHasAccess(false);
      onChangeSettingUpMiner(false);
      setPausedAction(true);
    }
  }, [
    setHasAccess,
    settingUpMiner,
    createPoolsError?.status,
    onChangeSettingUpMiner,
  ]);

  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAction(false);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal]);

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
        const noValidBackupPool =
          !isValidPool(pools[1]) && !isValidPool(pools[2]);
        if (noValidBackupPool) {
          setWarnBackupPool(true);
          return;
        }
      }
      setPausedAction(true);
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
              onChangeSettingUpMiner={onChangeSettingUpMiner}
            />
          </div>
        </div>
      </div>
    );
  }

  return (
    <AppLayout
      customButtons={
        <Button
          onClick={() => onContinue()}
          size={sizes.compact}
          variant={variants.accent}
          testId="finish-setup-button"
          text="Finish setup"
        />
      }
      networkInfo={networkInfo}
      onSuccessLogin={() => setShowLoginModal(false)}
      pendingNetworkInfo={pendingNetworkInfo}
      pendingSystemInfo={pendingSystemInfo}
      showLoginModal={showLoginModal}
      systemInfo={systemInfo}
      title="Miner setup"
      type={navigationMenuTypes.onboarding}
    >
      <WarnBackupPoolDialog
        onAddBackupPool={() => setWarnBackupPool(false)}
        onContinueWithoutBackup={onContinueWithoutBackup}
        show={warnBackupPool}
      />

      <MiningPools
        title="Add your mining pool"
        onChange={onChangePools}
        pools={pools}
      >
        <WarnDefaultPoolCallout
          onDismiss={() => setWarnDefaultPool(false)}
          show={warnDefaultPool}
        />
      </MiningPools>
    </AppLayout>
  );
};

export default Onboarding;
