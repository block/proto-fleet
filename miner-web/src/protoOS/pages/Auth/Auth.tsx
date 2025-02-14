import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { ids, initValues, minPasswordLength } from "./constants";
import { Values } from "./types";
import { useLogin, usePassword } from "@/protoOS/api";

import { Logo } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import { useNavigate } from "@/shared/hooks/useNavigate";
import { deepClone } from "@/shared/utils/utility";

import "./style.css";

interface ApiError {
  error: string | null;
  show: boolean;
}

const Auth = () => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [apiError, setApiError] = useState<ApiError>({
    error: null,
    show: false,
  });
  const [isSubmitting, setIsSubmitting] = useState(false);
  const { setPassword } = usePassword();
  const { login } = useLogin();
  const navigate = useNavigate();

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initValues);
    if (values.password.length < minPasswordLength) {
      newErrors.password = "Min. 8 characters required";
    }
    if (values.password !== values.confirmPassword) {
      newErrors.confirmPassword = "Passwords don't match";
    }
    setErrors(newErrors);
    return Object.values(newErrors).some((err) => err.length > 0);
  }, [values]);

  const handleContinue = useCallback(() => {
    const hasValidationErrors = validate();
    if (!hasValidationErrors) {
      setIsSubmitting(true);
      setPassword({
        password: values.password,
        onSuccess: () => {
          login({
            password: values.password,
            onFinally: () => {
              navigate("/onboarding");
            },
          });
        },
        onError: (error) => {
          setApiError({ error, show: true });
        },
        onFinally: () => setIsSubmitting(false),
      });
    }
  }, [validate, setPassword, values.password, navigate, login]);

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear error if the user starts typing
      setErrors(deepClone(initValues));
      if (apiError.error && apiError.show) {
        setApiError({ ...apiError, show: false });
        // allow the error to animate out
        setTimeout(() => {
          setApiError({ error: null, show: false });
        }, 300);
      }
    },
    [apiError, values]
  );

  const hasErrors = useMemo(
    () => Object.values(errors).some((err) => err.length > 0),
    [errors]
  );

  const disableContinue = useMemo(() => {
    return (
      !values.password.length ||
      !values.confirmPassword.length ||
      hasErrors ||
      isSubmitting
    );
  }, [
    hasErrors,
    values.confirmPassword.length,
    values.password.length,
    isSubmitting,
  ]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue();
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div className="auth-wrapper w-full h-screen flex justify-center items-center bg-surface-base">
      <div className="w-[402px] p-6 rounded-3xl shadow-200 bg-surface-elevated-base space-y-4">
        <div className="flex items-center">
          <div className="grow">
            <Logo className="w-[78px] text-text-primary" />
          </div>
          <div className="uppercase text-text-primary-50 text-300 leading-4 font-semibold rounded-lg border border-border-5 py-1 px-2">
            MDK Beta
          </div>
        </div>

        <Divider />

        <div className="space-y-1">
          <div className="text-heading-200 text-text-primary">
            Create a login for this miner
          </div>
          <div className="text-300 text-text-primary-70">
            Make sure to store your password somewhere safe. If you lose it
            you'll need to reset the miner, which will wipe all settings and
            logs.
          </div>
        </div>

        <div>
          <div
            className={clsx("transition-[max-height,margin] ease-in-out", {
              "max-h-0 overflow-hidden duration-300": !apiError.show,
              "max-h-96 mb-4 duration-500": apiError.show,
            })}
          >
            <div className="bg-intent-critical-10 text-intent-critical-text text-emphasis-300 leading-5 px-3 py-2 rounded-lg">
              {apiError.error}
            </div>
          </div>

          <div className="bg-surface-elevated-base rounded-lg relative z-10">
            <Input
              id={ids.username}
              label="Username"
              initValue="admin"
              disabled
            />
          </div>
        </div>
        <Input
          id={ids.password}
          label="Password"
          onChange={handleChange}
          type="password"
          initValue={values.password}
          error={errors.password}
          autoFocus
        />
        <Input
          id={ids.confirmPassword}
          label="Confirm password"
          onChange={handleChange}
          type="password"
          initValue={values.confirmPassword}
          error={errors.confirmPassword}
        />

        <Button
          variant={variants.accent}
          size={sizes.base}
          className="w-full py-3"
          disabled={disableContinue}
          onClick={handleContinue}
        >
          Continue
        </Button>
      </div>
    </div>
  );
};

export default Auth;
