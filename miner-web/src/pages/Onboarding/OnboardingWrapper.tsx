import { useEffect } from "react";
import { useNavigate } from "react-router-dom";

import { useSystemInfo } from "api";

import Spinner from "components/Spinner";

import Onboarding from "./Onboarding";

const OnboardingWrapper = () => {
  const { data: systemInfo, pending: pendingSystemInfo } = useSystemInfo();
  const navigate = useNavigate();

  // navigate to home page if miner has already been onboarded
  useEffect(() => {
    if (systemInfo && "onboarded" in systemInfo && systemInfo.onboarded) {
      navigate("/");
    }
  }, [systemInfo, navigate]);

  return (
    <>
      {pendingSystemInfo && !systemInfo ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <Onboarding />
      )}
    </>
  );
};

export default OnboardingWrapper;
