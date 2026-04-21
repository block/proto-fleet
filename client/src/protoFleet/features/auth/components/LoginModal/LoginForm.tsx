import { useCallback, useState } from "react";
import clsx from "clsx";

import { create } from "@bufbuild/protobuf";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { useLogin } from "@/protoFleet/api/useLogin";
import { ids, initValues, type Values } from "@/protoFleet/features/auth/components/LoginModal";
import { useSetTemporaryPassword } from "@/protoFleet/store";

import { Alert, Logo } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import ButtonGroup, { ButtonProps, groupVariants, sizes } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

import { deepClone } from "@/shared/utils/utility";

interface LoginFormProps {
  onDismiss?: () => void;
  onSuccess: (requiresPasswordChange: boolean) => void;
}

const LoginForm = ({ onDismiss, onSuccess }: LoginFormProps) => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [apiError, setApiError] = useState<string | null>(null);
  const login = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const setTemporaryPassword = useSetTemporaryPassword();

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
      onSuccess: (requiresPasswordChange: boolean) => {
        if (requiresPasswordChange) {
          setTemporaryPassword(values.password);
        }
        onSuccess(requiresPasswordChange);
      },
      onError: () => setApiError("Invalid credentials entered."),
      onFinally: () => setIsSubmitting(false),
    });
  }, [login, values.username, values.password, onSuccess, setTemporaryPassword]);

  const handleEnter = useCallback(() => {
    if (isSubmitting) {
      return;
    }

    handleContinue();
  }, [isSubmitting, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div data-testid="login-form" className="flex flex-col gap-10 phone:h-full phone:justify-between">
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
              <Callout intent="danger" prefixIcon={<Alert />} title="Invalid credentials entered." />
            </div>

            <Input
              id={ids.username}
              label="Proto Fleet username"
              initValue={values.username}
              onChange={handleChange}
              testId="username"
              autoFocus
            />

            <Input
              id={ids.password}
              label="Proto Fleet password"
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
                text: "Log in",
                onClick: handleContinue,
                variant: variants.primary,
                disabled: isSubmitting,
                testId: "login-button",
              },
            ].filter((button) => !!button.text) as ButtonProps[]
          }
        />
      </div>
      <div className="flex flex-col gap-2">
        <div className="text-300 text-text-primary-70">Powerful mining tools. Built for decentralization.</div>
        <div className="text-300 text-text-primary-50">&copy; {new Date().getFullYear()} Block, Inc.</div>
      </div>
    </div>
  );
};

export default LoginForm;
