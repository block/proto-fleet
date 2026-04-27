import { ReactNode, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { CreateAdminLoginRequestSchema } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { useAuth } from "@/protoFleet/api/useAuth";
import { useLogin } from "@/protoFleet/api/useLogin";
import { FleetWordmark, Logo, Plus } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import AnimatedDotsBackground from "@/shared/components/Animation";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { Authentication } from "@/shared/components/Setup";

const WELCOME_SCREEN_TIMEOUT = 3000; // how long the welcome screen should be visible for

type WelcomePanelProps = { children: ReactNode };

const WelcomePanel = ({ children }: WelcomePanelProps) => {
  const PlusIcon = <Plus className="font-bold text-text-emphasis" width={iconSizes.small} />;

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
  const { setPassword } = useAuth();
  const login = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | undefined>();
  const [showWelcome, setShowWelcome] = useState(false);
  const navigationTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Clear navigation timeout on unmount to prevent memory leaks
  useEffect(() => {
    return () => {
      if (navigationTimeoutRef.current) {
        clearTimeout(navigationTimeoutRef.current);
      }
    };
  }, []);

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
            // Clear any existing timeout before setting a new one
            if (navigationTimeoutRef.current) {
              clearTimeout(navigationTimeoutRef.current);
            }
            navigationTimeoutRef.current = setTimeout(() => {
              navigate("/onboarding/miners");
            }, WELCOME_SCREEN_TIMEOUT);
          },
        });
      },
      onError: (error: unknown) => {
        if (error instanceof ConnectError) {
          if (error.code === Code.AlreadyExists) {
            setSubmitError("Proto Fleet instance already onboarded. Please sign in.");
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
        <div className="flex min-h-screen items-center justify-center bg-surface-base">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <div className="flex h-screen flex-1 bg-surface-base">
          <div className="flex h-screen w-full laptop:w-1/2">
            <div className="mx-auto h-full px-6 py-10 laptop:px-0">
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
                </div>

                <div>
                  <div className="text-300 text-text-primary">
                    Powerful mining tools. Built for decentralization.
                    <div className="text-text-primary-50">© {new Date().getFullYear()} Block, Inc. Privacy Notice</div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div className="hidden h-screen w-1/2 py-10 pr-10 laptop:block">
            <div className="flex h-full flex-col justify-between rounded-3xl bg-landing-page">
              <div className="h-1/4 pt-10" />
              <div className="px-20">
                <WelcomePanel>
                  <div className="text-text-primary-90 mx-10 my-13 flex flex-col gap-4">
                    <FleetWordmark width="w-[162px]" />
                    <div className="text-heading-300">Mining software. Evolved.</div>
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
