import { ReactNode, useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { CreateAdminLoginRequestSchema } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { useLogin } from "@/protoFleet/api/useLogin";
import { usePassword } from "@/protoFleet/api/usePassword";
import { Logo, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import AnimatedDotsBackground from "@/shared/components/Animation";
import Button, { variants } from "@/shared/components/Button";
import { Authentication } from "@/shared/components/Setup";
import { Splash } from "@/shared/components/Splash";

const WELCOME_SCREEN_TIMEOUT = 3000; // how long the welcome screen should be visible for

type WelcomePanelProps = { children: ReactNode };

const WelcomePanel = ({ children }: WelcomePanelProps) => {
  const PlusIcon = (
    <Plus className="font-bold text-text-emphasis" width={iconSizes.small} />
  );

  return (
    <>
      <div className="flex justify-between">
        {PlusIcon}
        {PlusIcon}
      </div>
      {children}
      <div className="flex justify-between">
        {PlusIcon}
        {PlusIcon}
      </div>
    </>
  );
};

const WelcomePage = () => {
  const navigate = useNavigate();
  const { setPassword } = usePassword();
  const login = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | undefined>();
  const [showWelcome, setShowWelcome] = useState(false);

  const handleContinue = (password: string, username: string) => {
    const credentials = {
      username,
      password,
    };
    setSubmitError(undefined);
    setPassword({
      setPasswordRequest: create(CreateAdminLoginRequestSchema, credentials),
      onSuccess: () => {
        login({
          loginRequest: create(AuthenticateRequestSchema, credentials),
          onFinally: () => {
            setShowWelcome(true);
            setTimeout(() => {
              navigate("/onboarding/miners");
              setShowWelcome(false);
            }, WELCOME_SCREEN_TIMEOUT);
          },
        });
      },
      onError: (error: unknown) => {
        if (error instanceof ConnectError) {
          if (error.code === Code.AlreadyExists) {
            setSubmitError(
              "Proto Fleet instance already onboarded. Please sign in.",
            );
            return;
          }
        }
        if (error instanceof Error && error.message) {
          setSubmitError(error.message);
        } else if (typeof error === "string" && error) {
          setSubmitError(error);
        } else {
          setSubmitError("Something went wrong, please try again");
        }
      },
    });
  };

  return (
    <>
      {showWelcome ? (
        <Splash />
      ) : (
        <div className="flex h-screen flex-1 bg-surface-base">
          <div className="flex h-screen w-1/2 phone:w-full tablet:w-full">
            <div className="mx-auto h-full py-10 phone:px-6 tablet:px-6">
              <div className="flex h-full flex-col justify-center space-y-10">
                <Logo className="text-text-primary" width="w-[86px]" />

                <div className="min-w-80">
                  <Authentication
                    headline={
                      <>
                        Create your username <br />
                        and password
                      </>
                    }
                    inputPrefix="Proto Fleet"
                    requirePasswordConfirmation={false}
                    buttonClassName="w-full"
                    submit={handleContinue}
                    isSubmitting={isSubmitting}
                    setIsSubmitting={setIsSubmitting}
                    submitError={submitError}
                  />
                  <div className="mt-4 flex flex-row">
                    <div className="text-300 text-text-primary-50">
                      Already using Proto Fleet?
                    </div>
                    <Button
                      variant={variants.textOnly}
                      className="!py-0 !pl-1"
                      onClick={() => navigate("/auth")}
                    >
                      Sign in
                    </Button>
                  </div>
                </div>

                <div>
                  <div className="text-300 text-text-primary">
                    Powerful mining tools. Built for decentralization.
                    <div className="text-text-primary-50">
                      © {new Date().getFullYear()} Block, Inc. Privacy Notice
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div className="h-screen w-1/2 py-10 pr-10 phone:hidden tablet:hidden">
            <div className="flex h-full flex-col justify-between rounded-3xl bg-landing-page">
              <div className="h-1/4 pt-10" />
              <div className="px-20">
                <WelcomePanel>
                  <div className="mx-10 my-13 text-display-200">
                    Proto Fleet
                    <div className="text-text-primary-30">
                      Mining software.
                      <br />
                      Evolved.
                    </div>
                  </div>
                </WelcomePanel>
              </div>
              <div className="h-1/4 px-10 pb-8">
                <AnimatedDotsBackground padding={0} />
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default WelcomePage;
