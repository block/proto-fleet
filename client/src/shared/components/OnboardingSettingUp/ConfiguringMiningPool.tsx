import { useMemo } from "react";

import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { statuses } from "@/shared/constants/statuses";

interface ConfiguringMiningPoolProps {
  onClickReconfigure: () => void;
  onClickRetry: () => void;
  status: keyof typeof statuses;
}

const ConfiguringMiningPool = ({ onClickReconfigure, onClickRetry, status }: ConfiguringMiningPoolProps) => {
  const isLoading = useMemo(() => status === statuses.fetch || status === statuses.pending, [status]);

  const isError = useMemo(() => status === statuses.error, [status]);

  const isSuccess = useMemo(() => status === statuses.success, [status]);

  const prefixIcon = useMemo(() => {
    if (isLoading) return <ProgressCircular className="opacity-30" indeterminate />;
    if (isSuccess) return <Success className="text-text-success" />;
    if (isError) return <Alert className="text-text-warning" />;
  }, [isError, isLoading, isSuccess]);

  return (
    <Row className="flex" divider={false} prefixIcon={prefixIcon}>
      <div className="grow">
        <div className="text-emphasis-300">
          {isError ? "Configuring your mining pool" : "Testing your mining pool connections"}
        </div>
        {isError ? (
          <div className="text-200 text-text-primary-70">
            <div>We’re having trouble connecting to your mining pools.</div>
            <div>
              Reconfigure your mining pools or{" "}
              <button className="underline" onClick={onClickRetry}>
                test the connection again
              </button>
              .
            </div>
          </div>
        ) : null}
      </div>
      {isError ? (
        <div className="flex items-center">
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Reconfigure mining pools"
            onClick={onClickReconfigure}
          />
        </div>
      ) : null}
    </Row>
  );
};

export default ConfiguringMiningPool;
