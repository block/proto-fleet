import { useEffect, useMemo, useState } from "react";

import Onboarding from "./Onboarding";
import { useNetworkInfo, useSystemInfo, useSystemStatus } from "@/protoOS/api";

import ProgressCircular from "@/shared/components/ProgressCircular";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
import { useNavigate } from "@/shared/hooks/useNavigate";

const OnboardingWrapper = () => {
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const navigate = useNavigate();
  const { getItem } = useLocalStorage();
  const { data: networkInfo, pending: pendingNetworkInfo } = useNetworkInfo();
  const { data: systemInfo, pending: pendingSystemInfo } = useSystemInfo();

  const isOnboarded = useMemo(() => getItem("isOnboarded"), [getItem]);

  const [settingUpMiner, setSettingUpMiner] = useState(false);

  // navigate to home page if miner has already been onboarded
  useEffect(() => {
    if (isOnboarded || systemStatus?.onboarded) {
      navigate("/");
    } else if (systemStatus?.password_set === false) {
      navigate("/auth");
    }
  }, [isOnboarded, navigate, systemStatus]);

  return (
    <>
      {(isOnboarded === undefined || isOnboarded) &&
      pendingSystemStatus &&
      systemStatus?.onboarded === undefined ? (
        <div className="flex min-h-screen items-center justify-center">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <Onboarding
          networkInfo={networkInfo}
          pendingNetworkInfo={pendingNetworkInfo}
          systemInfo={systemInfo}
          pendingSystemInfo={pendingSystemInfo}
          settingUpMiner={settingUpMiner}
          onChangeSettingUpMiner={setSettingUpMiner}
        />
      )}
    </>
  );
};

export default OnboardingWrapper;
