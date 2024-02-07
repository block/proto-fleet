import { KeyboardEvent, useCallback } from "react";

interface InputProps {
  id: string;
  label: string;
  maxLength?: number;
  onKeyUp?: (value: string, id: string) => void;
  type?: string;
}

const Input = ({ id, label, maxLength, onKeyUp, type = "text" }: InputProps) => {
  const handleKeyUp = useCallback((event: KeyboardEvent<HTMLInputElement>) => {
    onKeyUp?.((event.target as HTMLInputElement).value, id);
  }, [id, onKeyUp]);

  return (
    <div className="relative mb-2">
      <label
        htmlFor={id}
        className="text-200 text-black-100/70 absolute left-[17px] top-[7px]"
      >
        {label}
      </label>
      <input
        type={type}
        id={id}
        className="border-black-100/5 focus:border-black-100 border-2 rounded-lg w-full h-14 outline-none pt-[18px] px-4 text-300"
        onKeyUp={handleKeyUp}
        maxLength={maxLength}
        autoComplete="off"
      />
    </div>
  );
};

export default Input;
