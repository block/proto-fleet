import { useEffect } from "react";

import { useSystemStatus } from "api";

import { useNavigate } from "common/hooks/useNavigate";

import Spinner from "components/Spinner";

import Auth from "./Auth";

const AuthWrapper = () => {
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();
  const navigate = useNavigate();

  // navigate to home page if miner has already set password and been onboarded
  // navigate to onboarding if miner has only set password
  useEffect(() => {
    if (systemStatus?.password_set) {
      if (systemStatus.onboarded) {
        navigate("/");
      } else {
        navigate("/onboarding");
      }
    }
  }, [navigate, systemStatus]);

  return (
    <>
      {pendingSystemStatus && systemStatus?.password_set === undefined ? (
        <div className="min-h-screen flex items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <Auth />
      )}
    </>
  );
};

export default AuthWrapper;
