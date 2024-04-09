import { useCallback, useState } from "react";

import { deepClone } from "common/utils/utility";

import { variants } from "components/Button";

import { WarnDefaultPoolCallout } from "./Callouts";
import { emptyPoolInfo, fanModes, tabs } from "./constants";
import ContentHeader from "./ContentHeader";
import Cooling from "./Cooling";
import { WarnBackupPoolDialog } from "./Dialogs";
import OnboardingHeader from "./OnboardingHeader";
import OnboardingNavigation from "./OnboardingNavigation";
import Pools from "./Pools";
import SettingUp from "./SettingUp";
import { FanMode, PoolInfo, Tabs } from "./types";
import { isValidPool } from "./utility";

const Onboarding = () => {
  // pools is an array of 3 PoolInfo objects
  // index 0 is the default pool, then backups 1 and 2
  // [{url: "", username: "", password: ""}, x3]
  const [pools, setPools] = useState<PoolInfo[]>(
    Array(3).fill(deepClone(emptyPoolInfo))
  );
  const [finalizedPoolUrls, setFinalizedPoolUrls] = useState<string[]>();

  const [warnDefaultPool, setWarnDefaultPool] = useState(false);
  const [warnBackupPool, setWarnBackupPool] = useState(false);

  const [fanMode, setFanMode] = useState<FanMode>(fanModes.auto);

  const [activeTab, setActiveTab] = useState<Tabs>(tabs.pools);
  const [settingUpMiner, setSettingUpMiner] = useState(false);

  const [isMenuOpen, setIsMenuOpen] = useState(false);

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
      // move on to next step
      setActiveTab(tabs.cooling);
      setFinalizedPoolUrls(pools.map((pool) => pool.url));
    },
    [pools]
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

  const onChangeFanMode = useCallback((id: string, isSelected: boolean) => {
    if (isSelected) {
      setFanMode(id as FanMode);
    }
  }, []);

  return (
    <div className="h-screen flex flex-col">
      {settingUpMiner ? (
        <>
          <OnboardingHeader openMenu={() => setIsMenuOpen(true)} />
          <div className="h-screen flex justify-center items-center">
            <div className="w-[600px]">
              <SettingUp
                fanMode={fanMode}
                pools={pools}
              />
            </div>
          </div>
        </>
      ) : (
        <>
          <OnboardingHeader
            button={
              activeTab === tabs.pools
                ? {
                    text: "Continue",
                    onClick: () => onContinue(),
                    variant: variants.primary,
                    testId: "continue-button",
                  }
                : {
                    text: "Finish setup",
                    onClick: () => setSettingUpMiner(true),
                    variant: variants.accent,
                    testId: "finish-setup-button",
                  }
            }
            openMenu={() => setIsMenuOpen(true)}
          />
          <WarnBackupPoolDialog
            onAddBackupPool={() => setWarnBackupPool(false)}
            onContinueWithoutBackup={onContinueWithoutBackup}
            show={warnBackupPool}
          />
          <div className="mt-[66px]">
            <OnboardingNavigation
              isVisible={isMenuOpen}
              closeMenu={() => setIsMenuOpen(false)}
              poolUrls={finalizedPoolUrls}
              activeTab={activeTab}
              onChangeActiveTab={setActiveTab}
            />
            <div className="desktop:ml-80 laptop:ml-80">
              <div className="m-14 tablet:m-6 phone:m-6 flex justify-center">
                <div className="w-[640px]">
                  {activeTab === tabs.pools && (
                    <>
                      <ContentHeader
                        title="Mining pool"
                        subtitle="Enter your mining pool details below."
                        testId="mining-pool-title"
                      />
                      <WarnDefaultPoolCallout
                        onDismiss={() => setWarnDefaultPool(false)}
                        show={warnDefaultPool}
                      />
                      <Pools pools={pools} onChangePools={onChangePools} />
                    </>
                  )}
                  {activeTab === tabs.cooling && (
                    <>
                      <ContentHeader
                        title="Cooling"
                        subtitle="Choose how you want to cool your device. This can be changed at any time."
                        testId="cooling-title"
                      />
                      <Cooling fanMode={fanMode} onChange={onChangeFanMode} />
                    </>
                  )}
                </div>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default Onboarding;
