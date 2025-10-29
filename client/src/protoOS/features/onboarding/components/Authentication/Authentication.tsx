import { useEffect, useState } from "react";
import { useLogin, usePassword } from "@/protoOS/api";
import { usePasswordSet } from "@/protoOS/store";
import { Authentication, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const navigate = useNavigate();
  const { setPassword } = usePassword();
  const login = useLogin();
  const isPasswordSet = usePasswordSet();
  const [submitError, setSubmitError] = useState<string | undefined>();
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (isPasswordSet !== undefined && isPasswordSet) {
      navigate("/onboarding/mining-pool");
    }
  }, [navigate, isPasswordSet]);

  function submit(password: string) {
    setIsSubmitting(true);
    setSubmitError(undefined);
    setPassword({
      password: password,
      onSuccess: () => {
        login({
          password: password,
          onFinally: () => {
            navigate("/onboarding/mining-pool");
          },
        });
      },
      onError: (message) => {
        setSubmitError(message);
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }

  return (
    <OnboardingLayout>
      <Authentication
        submit={submit}
        submitError={submitError}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        headline="Create an admin login for your miner"
        description="This password is required to modify performance settings or mining pool configurations for this miner."
        initUsername="admin"
      />
    </OnboardingLayout>
  );
};

export default AuthenticationPage;
