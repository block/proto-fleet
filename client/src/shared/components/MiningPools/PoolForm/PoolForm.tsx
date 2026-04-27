import { useCallback, useEffect, useMemo, useState } from "react";

import { poolInfoAttributes } from "../constants";
import { PoolConnectionTestProps, PoolIndex, PoolInfo } from "../types";
import { urlValidationErrors, validateURLScheme } from "./constants";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { deepClone } from "@/shared/utils/utility";

interface PoolFormProps {
  onChangePools: (pools: PoolInfo[]) => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  setShouldTestConnection: (shouldTestConnection: boolean) => void;
  shouldTestConnection: boolean;
  isTestingConnection: boolean;
  testConnection: (args: PoolConnectionTestProps) => void;
  onFocus?: () => void;
  onBlur?: () => void;
}

const PoolForm = ({
  onChangePools,
  poolIndex,
  pools,
  setShouldTestConnection,
  shouldTestConnection,
  isTestingConnection,
  testConnection,
  onFocus,
  onBlur,
}: PoolFormProps) => {
  const [showCallout, setShowCallout] = useState(false);
  const [error, setError] = useState(false);
  const [validationErrors, setValidationErrors] = useState<Partial<Record<keyof typeof poolInfoAttributes, string>>>(
    {},
  );

  const showNotConnectedCallout = useMemo(
    () => showCallout && !isTestingConnection && error,
    [showCallout, error, isTestingConnection],
  );

  useEffect(() => {
    if (shouldTestConnection && !isTestingConnection) {
      setShouldTestConnection(false);
      if (!pools[poolIndex].url.trim()) {
        // eslint-disable-next-line react-hooks/set-state-in-effect -- surface validation error synchronously before dispatching the async test
        setValidationErrors({
          ...validationErrors,
          url: urlValidationErrors.required,
        });
        return;
      }
      setError(false);
      testConnection({
        poolInfo: pools[poolIndex],
        onError: () => {
          setError(true);
        },
        onSuccess: () => {
          pushToast({
            message: "Mining pool connection successful",
            status: TOAST_STATUSES.success,
          });
        },
        onFinally: () => setShowCallout(true),
      });
    }
  }, [
    isTestingConnection,
    poolIndex,
    pools,
    setShouldTestConnection,
    shouldTestConnection,
    testConnection,
    validationErrors,
  ]);

  const onPoolChange = useCallback(
    (value: string, id: string) => {
      setShowCallout(false);
      // in order to avoid multiple instances of the same id in the form,
      // the id is in the format of "poolKey poolIndex"
      // e.g. "username 0"
      const infoKey = id.split(" ")[0];
      if (infoKey === poolInfoAttributes.url) {
        const trimmed = value.trim();
        let urlError: string | undefined;
        if (!trimmed) {
          urlError = urlValidationErrors.required;
        } else {
          urlError = validateURLScheme(trimmed);
        }
        setValidationErrors({
          ...validationErrors,
          url: urlError,
        });
      }
      const poolsInfo = deepClone(pools);
      poolsInfo[poolIndex][infoKey] = value;
      onChangePools(poolsInfo);
    },
    [pools, poolIndex, onChangePools, validationErrors],
  );

  const onKeyDown = useCallback(
    (key: string) => {
      if (key === "Enter") {
        setShouldTestConnection(true);
      }
    },
    [setShouldTestConnection],
  );

  return (
    <>
      <DismissibleCalloutWrapper
        icon={<Info width={iconSizes.xLarge} />}
        intent={intents.warning}
        onDismiss={() => setShowCallout(false)}
        show={showNotConnectedCallout}
        title={
          <>
            We could not connect with your pool.
            <br />
            Review your pool details and try again.
          </>
        }
        testId="pool-not-connected-callout"
      />
      <div className="space-y-4">
        <Input
          id={`${poolInfoAttributes.url} ${poolIndex}`}
          label="Pool URL"
          maxLength={2083}
          onChangeBlur={onPoolChange}
          onKeyDown={onKeyDown}
          initValue={pools[poolIndex].url}
          testId={`${poolInfoAttributes.url}-${poolIndex}-input`}
          tooltip={{
            header: "Mining Pool URL",
            body: "Enter the mining pool URL. Protocol is determined by the URL scheme: stratum+tcp:// / stratum+ssl:// / stratum+ws:// are Stratum V1; stratum2+tcp:// is Stratum V2 (only miners with native V2 firmware can use V2 pools today).",
          }}
          error={validationErrors.url}
          onFocus={onFocus}
          onBlur={onBlur}
        />
        <Input
          id={`${poolInfoAttributes.username} ${poolIndex}`}
          label="Username"
          onChangeBlur={onPoolChange}
          onKeyDown={onKeyDown}
          initValue={pools[poolIndex].username}
          tooltip={{
            header: "Username",
            body: "Use the username that you created when setting up your mining pool.",
          }}
          onFocus={onFocus}
          onBlur={onBlur}
        />
      </div>
    </>
  );
};

export default PoolForm;
