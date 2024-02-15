import { ChangeEvent, useCallback, useState } from "react";

import "./style.css";
import clsx from "clsx";

interface InputProps {
  id: string;
  label: string;
  maxLength?: number;
  onChange?: (value: string, id: string) => void;
  type?: string;
}

const Input = ({
  id,
  label,
  maxLength,
  onChange,
  type = "text",
}: InputProps) => {
  const [value, setValue] = useState("");

  const handleChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const newValue = (event.target as HTMLInputElement).value;
      setValue(newValue);
      onChange?.(newValue, id);
    },
    [onChange, id]
  );

  return (
    <div className="relative mb-2">
      <input
        type={type}
        id={id}
        className="peer border-border-primary/5 focus:border-border-primary border-2 rounded-lg w-full h-14 outline-none pt-[18px] px-4 text-300"
        onChange={handleChange}
        maxLength={maxLength}
        autoComplete="off"
        value={value}
      />
      <label
        htmlFor={id}
        className={clsx(
          "text-200 text-text-primary/70 absolute left-[17px] peer-focus:top-[7px] transition-top",
          { "top-[18px]": !value.length },
          { "top-[7px]": value.length }
        )}
      >
        {label}
      </label>
    </div>
  );
};

export default Input;
