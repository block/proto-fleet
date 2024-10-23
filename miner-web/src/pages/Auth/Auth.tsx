import { useCallback, useMemo, useState } from "react";

import { useLogin, usePassword } from "api";

import { useKeyDown } from "common/hooks/useKeyDown";
import { useNavigate } from "common/hooks/useNavigate";
import { deepClone } from "common/utils/utility";

import Button, { sizes, variants } from "components/Button";
import Divider from "components/Divider";
import Input from "components/Input";

import { Logo } from "icons";

import { ids, initValues, minPasswordLength } from "./constants";
import { Values } from "./types";

import "./style.css";

const Auth = () => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
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
      setPassword({
        password: values.password,
        onSuccess: () => {
          login({
            password: values.password,
            onSuccess: () => {
              navigate("/onboarding");
            },
          });
        },
      });
    }
  }, [validate, setPassword, values.password, navigate, login]);

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear error if the user starts typing
      setErrors(deepClone(initValues));
    },
    [values]
  );

  const hasErrors = useMemo(
    () => Object.values(errors).some((err) => err.length > 0),
    [errors]
  );

  const disableContinue = useMemo(() => {
    return (
      !values.password.length || !values.confirmPassword.length || hasErrors
    );
  }, [hasErrors, values.confirmPassword.length, values.password.length]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue();
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div className="auth-wrapper w-full h-screen flex justify-center items-center bg-surface-base">
      <div className="w-[402px] p-6 rounded-3xl shadow-200 bg-surface-base space-y-4">
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

        <Input id={ids.username} label="Username" initValue="admin" readonly />
        <Input
          id={ids.password}
          label="Password"
          onChange={handleChange}
          type="password"
          initValue={values.password}
          error={errors.password}
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
