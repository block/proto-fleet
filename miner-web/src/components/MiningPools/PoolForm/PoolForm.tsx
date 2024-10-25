import { useCallback, useEffect, useMemo, useState } from "react";

import { TestConnectionProps } from "api";

import { deepClone } from "common/utils/utility";

import Input from "components/Input";

import { PoolConnectedCallout, PoolNotConnectedCallout } from "../Callouts";
import { info } from "../constants";
import { PoolIndex, PoolInfo } from "../types";
import { urlValidationErrors } from "./constants";

interface PoolFormProps {
  isTestingConnection: boolean;
  onChangePools: (pools: PoolInfo[]) => void;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  setShouldTestConnection: (shouldTestConnection: boolean) => void;
  shouldTestConnection: boolean;
  testConnection: (args: TestConnectionProps) => void;
}

const PoolForm = ({
  isTestingConnection,
  onChangePools,
  poolIndex,
  pools,
  setShouldTestConnection,
  shouldTestConnection,
  testConnection,
}: PoolFormProps) => {
  const [showCallout, setShowCallout] = useState(false);
  const [error, setError] = useState(false);
  const [validationErrors, setValidationErrors] = useState<
    Partial<Record<keyof typeof info, string>>
  >({});

  const showConnectedCallout = useMemo(
    () => showCallout && !isTestingConnection && !error,
    [showCallout, error, isTestingConnection]
  );

  const showNotConnectedCallout = useMemo(
    () => showCallout && !isTestingConnection && error,
    [showCallout, error, isTestingConnection]
  );

  useEffect(() => {
    if (shouldTestConnection && !isTestingConnection) {
      setShouldTestConnection(false);
      if (!pools[poolIndex].url.trim()) {
        setValidationErrors({
          ...validationErrors,
          url: urlValidationErrors.required,
        });
        return;
      }
      setError(false);
      testConnection({
        poolInfo: pools[poolIndex],
        onError: () => setError(true),
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

  const onChange = useCallback(
    (value: string, id: string) => {
      setShowCallout(false);
      // in order to avoid multiple instances of the same id in the form,
      // the id is in the format of "poolKey poolIndex"
      // e.g. "username 0"
      const infoKey = id.split(" ")[0];
      if (infoKey === info.url) {
        setValidationErrors({
          ...validationErrors,
          url: value.trim() ? undefined : urlValidationErrors.required,
        });
      }
      const poolsInfo = deepClone(pools);
      poolsInfo[poolIndex][infoKey] = value;
      onChangePools(poolsInfo);
    },
    [pools, poolIndex, onChangePools, validationErrors]
  );

  const onKeyDown = useCallback((key: string) => {
    if (key === "Enter") {
      setShouldTestConnection(true);
    }
  }, [setShouldTestConnection]);

  return (
    <>
      <PoolConnectedCallout
        onDismiss={() => setShowCallout(false)}
        show={showConnectedCallout}
      />
      <PoolNotConnectedCallout
        currentPoolIndex={poolIndex}
        onDismiss={() => setShowCallout(false)}
        show={showNotConnectedCallout}
      />
      <div className="space-y-4">
        <Input
          id={`${info.url} ${poolIndex}`}
          label="Pool URL"
          maxLength={2083}
          onChange={onChange}
          onKeyDown={onKeyDown}
          initValue={pools[poolIndex].url}
          testId={`${info.url}-${poolIndex}-input`}
          tooltip={{
            header: "Mining Pool URL",
            body: "Enter the mining pool URL you want this miner to connect with. A mining pool URL allows this miner to communicate with the pool's server.",
          }}
          error={validationErrors.url}
          autoFocus
        />
        <Input
          id={`${info.username} ${poolIndex}`}
          label="Username"
          onChange={onChange}
          onKeyDown={onKeyDown}
          initValue={pools[poolIndex].username}
          tooltip={{
            header: "Username",
            body: "Use the username that you created when setting up your mining pool.",
          }}
        />
        <div>
          <Input
            id={`${info.password} ${poolIndex}`}
            label="Password"
            type="password"
            onChange={onChange}
            onKeyDown={onKeyDown}
            initValue={pools[poolIndex].password}
            tooltip={{
              header: "Password",
              body: "Depending on the mining pool you’re trying to connect to, you may need to enter the password you use to log in to that pool.",
            }}
          />
          <div className="text-200 text-text-primary-50 mt-2">
            A password might be required depending on the pool you’re connecting
            to.
          </div>
        </div>
      </div>
    </>
  );
};

export default PoolForm;
