import clsx from "clsx";

interface ButtonProps {
  className?: string;
  onClick: () => void;
  text: string;
}

const Button = ({ className, onClick, text }: ButtonProps) => {
  return (
    <button
      type="button"
      className={clsx(
        "h-9 p-3 flex items-center justify-center bg-primary-10 rounded-lg border-[1px] border-primary-100",
        "hover:bg-white-100 hover:border-foreground-100",
        className
      )}
      onClick={onClick}
    >
      {text}
    </button>
  );
};

export default Button;
