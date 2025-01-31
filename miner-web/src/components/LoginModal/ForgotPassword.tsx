import Button, { sizes, variants } from "components/Button";
import Divider from "components/Divider";
import Row from "components/Row";

import { Alert, ArrowRight, Success } from "icons";

interface ForgotPasswordProps {
  onDismiss: () => void;
}

const ForgotPassword = ({ onDismiss }: ForgotPasswordProps) => {
  return (
    <div className="space-y-4"
      data-testid="forgot-password-instructions"
    >
      <Button
        size={sizes.compact}
        variant={variants.secondary}
        onClick={onDismiss}
        prefixIcon={<ArrowRight className="rotate-180 text-text-primary-70" />}
        testId="forgot-password-back"
      />
      <div>
        <div className="text-heading-200 text-text-primary">
          Forgot your password?
        </div>
        <div className="text-300 text-text-primary-70 mt-1">
          To reset your password, you’ll need to reset your miner back to it’s
          default settings.
        </div>
      </div>
      <Divider />
      <div>
        <div className="text-heading-100 text-text-primary mb-2">
          What happens if I reset my miner?
        </div>
        <Row
          compact
          prefixIcon={<Alert className="text-intent-warning-fill" />}
        >
          You will lose your miner logs
        </Row>
        <Row
          compact
          prefixIcon={<Alert className="text-intent-warning-fill" />}
        >
          You will lose your mining pool settings
        </Row>
        <Row
          compact
          prefixIcon={<Success className="text-intent-success-fill" />}
          divider={false}
        >
          You{" "}
          <span className="underline decoration-dotted decoration-text-primary-30">
            will not lose
          </span>{" "}
          any mining rewards
        </Row>
      </div>
      <Divider />
      <div>
        <div className="text-heading-100 text-text-primary mb-2">
          How do I reset my miner?
        </div>
        <div className="text-300 text-text-primary-70 mt-1">
          Unplug the miner, remove the microSD card, and re-flash the firmware
          with the latest version.
        </div>
      </div>
      <Divider />
      <div className="text-300 text-text-primary-70">
        Still need help?{" "}
        <span className="underline underline-offset-[3px]">
          <a href="mailto:mining.support@block.xyz" target="_blank">
            Contact support {"->"}
          </a>
        </span>
      </div>
    </div>
  );
};

export default ForgotPassword;
