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

import { Logo } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
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
    <div
      data-testid="login-form"
      className="flex flex-col gap-10 phone:h-full phone:justify-between"
    >
      <Logo width="w-[86px]" />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-6">
          <div className="flex flex-col gap-2">
            <div className="text-heading-300 text-text-primary">Log in</div>
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
              label="ProtoFleet username"
              initValue={values.username}
              onChange={handleChange}
              testId="username"
              autoFocus
            />

            <Input
              id={ids.password}
              label="ProtoFleet password"
              onChange={handleChange}
              type="password"
              initValue={values.password}
              error={errors.password}
              testId="password"
            />
          </div>
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
                text: "Sign in",
                onClick: handleContinue,
                variant: variants.primary,
                disabled: isSubmitting,
                testId: "login-button",
              },
            ].filter((button) => !!button.text) as ButtonProps[]
          }
        />

        <div className="flex items-center">
          <span className="text-300 text-text-primary-50">
            New to Proto Fleet?
          </span>
          <Button
            variant={variants.textOnly}
            className="!py-0 !pl-1"
            onClick={onClickCreateAccount}
          >
            Create an account
          </Button>
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <div className="text-300 text-text-primary-70">
          Powerful mining tools. Built for decentralization.
        </div>
        <div className="text-300 text-text-primary-50">
          &copy; {new Date().getFullYear()} Block, Inc.
        </div>
      </div>
    </div>
  );
};

export default LoginForm;
