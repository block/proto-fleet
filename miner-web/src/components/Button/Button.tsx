import clsx from "clsx";

interface ButtonProps {
  className?: string;
  icon?: string;
  onClick: () => void;
  text: string;
}

const Button = ({ className, icon, onClick, text }: ButtonProps) => {
  return (
    <button
      type="button"
      className={clsx(
        "text-button text-foreground-100 h-9 p-3 flex items-center justify-center bg-black-100/5 rounded-lg",
        "hover:bg-warning-100/5",
        className
      )}
      onClick={onClick}
    >
      {icon && <img src={icon} className="mr-2" />}
      {text}
    </button>
  );
};

export default Button;
