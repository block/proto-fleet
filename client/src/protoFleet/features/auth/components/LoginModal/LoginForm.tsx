import { useCallback, useState } from "react";
import clsx from "clsx";

import { create } from "@bufbuild/protobuf";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { useLogin } from "@/protoFleet/api/useLogin";
import {
  ids,
  initValues,
  type Values,
} from "@/protoFleet/features/auth/components/LoginModal";

import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

import { deepClone } from "@/shared/utils/utility";

interface LoginFormProps {
  onClickForgotPassword: () => void;
  onClickCreateAccount?: () => void;
  onDismiss?: () => void;
  onSuccess: () => void;
}

const LoginForm = ({
  onClickForgotPassword,
  onClickCreateAccount,
  onDismiss,
  onSuccess,
}: LoginFormProps) => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [apiError, setApiError] = useState<string | null>(null);
  const login = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear errors if the user starts typing
      setErrors(deepClone(initValues));
      setApiError(null);
    },
    [values],
  );

  const handleContinue = useCallback(() => {
    setIsSubmitting(true);
    login({
      loginRequest: create(AuthenticateRequestSchema, {
        username: values.username,
        password: values.password,
      }),
      onSuccess,
      onError: () => setApiError("Invalid credentials entered."),
      onFinally: () => setIsSubmitting(false),
    });
  }, [login, values.username, values.password, onSuccess]);

  const handleEnter = useCallback(() => {
    if (isSubmitting) {
      return;
    }

    handleContinue();
  }, [isSubmitting, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div data-testid="login-form" className="flex flex-col gap-4">
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <div className="text-heading-200 text-text-primary">
            Login required
          </div>
          <div className="text-300 text-text-primary-70">
            Contact your system administrator if you need access.
          </div>
        </div>

        <div className="flex flex-col gap-4">
          <div
            className={clsx("transition-[max-height,margin] ease-in-out", {
              "max-h-0 overflow-hidden duration-300": !apiError,
              "max-h-96 duration-500": apiError,
            })}
            data-testid="error"
          >
            <div className="rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
              Invalid credentials entered.
            </div>
          </div>

          <Input
            id={ids.username}
            label="Username"
            initValue={values.username}
            onChange={handleChange}
            testId="username"
          />

          <Input
            id={ids.password}
            label="Password"
            onChange={handleChange}
            type="password"
            initValue={values.password}
            error={errors.password}
            testId="password"
            autoFocus
          />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <button
          className="flex text-200 text-text-primary-50 hover:cursor-pointer"
          onClick={onClickForgotPassword}
          data-testid="forgot-password"
        >
          {"Forgot password ->"}
        </button>
        <button
          className="flex text-200 text-text-primary-50 hover:cursor-pointer"
          onClick={onClickCreateAccount}
          data-testid="create-account"
        >
          {"Create an account ->"}
        </button>
      </div>

      <ButtonGroup
        variant={groupVariants.fill}
        size={sizes.base}
        buttons={
          [
            {
              ...(onDismiss && {
                text: "Cancel",
                onClick: onDismiss,
                variant: variants.secondary,
              }),
            },
            {
              text: "Continue",
              onClick: handleContinue,
              variant: variants.primary,
              disabled: isSubmitting,
              testId: "login-button",
            },
          ].filter((button) => !!button.text) as ButtonProps[]
        }
      />
    </div>
  );
};

export default LoginForm;
