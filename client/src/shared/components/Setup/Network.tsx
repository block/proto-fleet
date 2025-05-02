import { useCallback, useMemo, useState } from "react";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Row from "@/shared/components/Row";
import { initValues } from "@/shared/components/Setup/network.constants";
import { Values } from "@/shared/components/Setup/network.types";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import { deepClone } from "@/shared/utils/utility";

type NetworkProps = {
  submit: (networkName: string) => void;
  networkName: string;
  ipRange: string;
  gateway: string;
};

const Network = ({
  submit,
  networkName = "Bathhouse Williamsburg",
  ipRange = "192.168.1.0/24",
  gateway = "192.168.1.1",
}: NetworkProps) => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [isSubmitting, setIsSubmitting] = useState(false);

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initValues);
    if (values.networkName.length === 0) {
      newErrors.networkName = "A network name is required";
    }
    setErrors(newErrors);
    return Object.values(newErrors).some((err) => err.length > 0);
  }, [values]);

  const handleContinue = useCallback(() => {
    const hasValidationErrors = validate();

    if (!hasValidationErrors) {
      setIsSubmitting(true);
      try {
        submit(values.networkName);
      } catch {
        // TODO submit is not awaited, cannot catch error
        setIsSubmitting(false);
      }
    }
  }, [validate, submit, values.networkName]);

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear error if the user starts typing
      setErrors(deepClone(initValues));
    },
    [values],
  );

  const hasErrors = useMemo(
    () => Object.values(errors).some((err) => err.length > 0),
    [errors],
  );

  const disableContinue = useMemo(() => {
    return !values.networkName.length || hasErrors || isSubmitting;
  }, [hasErrors, values.networkName.length, isSubmitting]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue();
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <>
      <div className="container mx-auto max-w-xl">
        <div className="flex flex-col gap-6">
          <Header
            title="Give your network a nickname"
            titleSize="text-heading-300"
            description="Proto uses your local network to connect to miners. Give your network a nickname so it’s easier to identify what network your miner is connected to."
          />
          <Input
            onChange={handleChange}
            id="networkName"
            label="Network"
            initValue={values.networkName}
            error={errors.networkName}
          />
          <div className="rounded-xl bg-surface-5 p-3">
            <div className="w-full">
              <Row className="flex justify-between">
                <div className="text-emphasis-300">Network details</div>
              </Row>
            </div>

            <div className="w-full text-300">
              <Row className="flex justify-between">
                <div>Network Name</div>
                <div>{networkName}</div>
              </Row>
              <Row className="flex justify-between">
                <div>IP Range</div>
                <div>{ipRange}</div>
              </Row>
              <Row divider={false} className="flex justify-between">
                <div>Gateway</div>
                <div>{gateway}</div>
              </Row>
            </div>
          </div>
          <Button
            onClick={handleContinue}
            className="ml-auto"
            variant="primary"
            loading={isSubmitting}
          >
            Continue
          </Button>
        </div>
      </div>
    </>
  );
};

export default Network;
