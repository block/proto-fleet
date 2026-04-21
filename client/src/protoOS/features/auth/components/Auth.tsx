import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import { ids, initValues } from "./constants";
import { Values } from "./types";
import { useLogin, usePassword } from "@/protoOS/api";

import { Alert, Logo } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import { isPasswordTooShort, passwordErrors } from "@/shared/components/Setup/authentication.constants";
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
  const login = useLogin();
  const navigate = useNavigate();

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initValues);
    if (isPasswordTooShort(values.password)) {
      newErrors.password = passwordErrors.tooShort;
    }
    if (values.password !== values.confirmPassword) {
      newErrors.confirmPassword = passwordErrors.mismatch;
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
    [apiError, values],
  );

  const hasErrors = useMemo(() => Object.values(errors).some((err) => err.length > 0), [errors]);

  const disableContinue = useMemo(() => {
    return isPasswordTooShort(values.password) || !values.confirmPassword.length || hasErrors || isSubmitting;
  }, [hasErrors, values.confirmPassword.length, values.password, isSubmitting]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue();
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div className="auth-wrapper flex h-screen w-full items-center justify-center bg-surface-base">
      <div className="w-[402px] space-y-4 rounded-3xl bg-surface-elevated-base p-6 shadow-200">
        <div className="flex items-center">
          <div className="grow">
            <Logo className="w-[78px] text-text-primary" />
          </div>
          <div className="rounded-lg border border-border-5 px-2 py-1 text-300 leading-4 font-semibold text-text-primary-50 uppercase">
            MDK Beta
          </div>
        </div>

        <Divider />

        <div className="space-y-1">
          <div className="text-heading-200 text-text-primary">Create a login for this miner</div>
          <div className="text-300 text-text-primary-70">
            Make sure to store your password somewhere safe. If you lose it you'll need to reset the miner, which will
            wipe all settings and logs.
          </div>
        </div>

        <div>
          <div
            className={clsx("transition-[max-height,margin] ease-in-out", {
              "max-h-0 overflow-hidden duration-300": !apiError.show,
              "mb-4 max-h-96 duration-500": apiError.show,
            })}
          >
            <Callout intent="danger" prefixIcon={<Alert />} title={apiError.error} />
          </div>

          <div className="relative z-10 rounded-lg bg-surface-elevated-base">
            <Input id={ids.username} label="Username" initValue="admin" disabled />
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
          variant={variants.primary}
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
