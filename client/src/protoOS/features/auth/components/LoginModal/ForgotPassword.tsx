import { Alert, ArrowRight, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Row from "@/shared/components/Row";

interface ForgotPasswordProps {
  onDismiss: () => void;
}

const ForgotPassword = ({ onDismiss }: ForgotPasswordProps) => {
  return (
    <div className="space-y-4" data-testid="forgot-password-instructions">
      <Button
        size={sizes.compact}
        variant={variants.secondary}
        onClick={onDismiss}
        prefixIcon={<ArrowRight className="rotate-180 text-text-primary-70" />}
        testId="forgot-password-back"
      />
      <div>
        <div className="text-heading-200 text-text-primary">Forgot your password?</div>
        <div className="mt-1 text-300 text-text-primary-70">
          To reset your password, you'll need to reset your miner back to its default settings.
        </div>
      </div>
      <Divider />
      <div>
        <div className="mb-2 text-heading-100 text-text-primary">What happens if I reset my miner?</div>
        <Row compact prefixIcon={<Alert className="text-intent-warning-fill" />}>
          You will lose your miner logs
        </Row>
        <Row compact prefixIcon={<Alert className="text-intent-warning-fill" />}>
          You will lose your mining pool settings
        </Row>
        <Row compact prefixIcon={<Success className="text-intent-success-fill" />} divider={false}>
          You <span className="underline decoration-text-primary-30 decoration-dotted">will not lose</span> any mining
          rewards
        </Row>
      </div>
      <Divider />
      <div>
        <div className="mb-2 text-heading-100 text-text-primary">How do I reset my miner?</div>
        <div className="mt-1 flex flex-col gap-2 text-300 text-text-primary-70">
          <p>
            <strong>Unit ON:</strong> Hold the power button for <code>20+ seconds</code> (first <code>10s</code> to
            force reboot, next <code>10s</code> to reset).
          </p>
          <p>
            <strong>Unit OFF:</strong> Hold the power button for <code>10+ seconds</code>.
          </p>
          <p>
            The LED display will count down from <code>9 ⟶ 0</code>, then your miner will reset to factory defaults.
          </p>
        </div>
      </div>
    </div>
  );
};

export default ForgotPassword;
