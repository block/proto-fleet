import clsx from "clsx";

interface ButtonProps {
  className?: string;
  text: string;
}

const Button = ({ className, text }: ButtonProps) => {
  return (
    <button
      type="button"
      className={clsx(
        "h-9 p-3 flex items-center justify-center bg-primary-10 rounded-lg border-[1px] border-primary-100",
        "hover:bg-white-100 hover:border-foreground-100",
        className
      )}
    >
      {text}
    </button>
  );
};

export default Button;
