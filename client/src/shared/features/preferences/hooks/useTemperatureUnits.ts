import { useEffect, useMemo, useState } from "react";
import { TEMP_UNITS } from "../constants";
import { TemperatureUnits } from "../types";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const useTemperatureUnits = () => {
  const { getItem, setItem } = useLocalStorage();

  const [temperatureUnits, setTemperatureUnits] = useState<TemperatureUnits>(
    getItem("temperatureUnits") || TEMP_UNITS.celcius,
  );

  useEffect(() => {
    setItem("temperatureUnits", temperatureUnits);
  }, [temperatureUnits, setItem]);

  return useMemo(
    () => ({
      temperatureUnits,
      setTemperatureUnits,
    }),
    [temperatureUnits, setTemperatureUnits],
  );
};

export default useTemperatureUnits;
