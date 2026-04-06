import { useCallback, useState } from "react";
import clsx from "clsx";

import { useLogin } from "@/protoOS/api";

import { ids, initValues, type Values } from "@/protoOS/features/auth/components";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import ButtonGroup, { ButtonProps, groupVariants, sizes } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

// TODO: When implemement Auth for ProtoFleet we may want move these
// consts elsewhere because the components shouldnt be importing from pages
// leaving for now because Auth will probably be moved shared/features
import { deepClone } from "@/shared/utils/utility";

interface LoginFormProps {
  onClickForgotPassword: () => void;
  onDismiss?: () => void;
  onSuccess: () => void;
}

const LoginForm = ({ onClickForgotPassword, onDismiss, onSuccess }: LoginFormProps) => {
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
      password: values.password,
      onSuccess,
      onError: () => setApiError("Invalid credentials entered."),
      onFinally: () => setIsSubmitting(false),
    });
  }, [onSuccess, login, values.password]);

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
          <div className="text-heading-200 text-text-primary">Login required</div>
          <div className="text-300 text-text-primary-70">Contact your system administrator if you need access.</div>
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
          <Callout intent="danger" prefixIcon={<Alert />} title="Invalid credentials entered." />
        </div>

        <Input id={ids.username} label="Username" initValue="admin" disabled testId="username" />

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

      <button
        className="flex text-200 text-intent-warning-fill hover:cursor-pointer"
        onClick={onClickForgotPassword}
        data-testid="forgot-password"
      >
        Forgot password
      </button>

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
