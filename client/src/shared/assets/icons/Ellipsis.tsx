import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const Ellipsis = ({ width = "w-[20px]", ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props} width={width}>
      <svg width="100%" height="100%" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M4.99984 10.0002C4.99984 10.9206 4.25365 11.6668 3.33317 11.6668C2.4127 11.6668 1.6665 10.9206 1.6665 10.0002C1.6665 9.07969 2.4127 8.3335 3.33317 8.3335C4.25365 8.3335 4.99984 9.07969 4.99984 10.0002ZM11.6665 10.0002C11.6665 10.9206 10.9203 11.6668 9.99984 11.6668C9.07936 11.6668 8.33317 10.9206 8.33317 10.0002C8.33317 9.07969 9.07936 8.3335 9.99984 8.3335C10.9203 8.3335 11.6665 9.07969 11.6665 10.0002ZM16.6665 11.6668C17.587 11.6668 18.3332 10.9206 18.3332 10.0002C18.3332 9.07969 17.587 8.3335 16.6665 8.3335C15.746 8.3335 14.9998 9.07969 14.9998 10.0002C14.9998 10.9206 15.746 11.6668 16.6665 11.6668Z"
          fill="currentColor"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default Ellipsis;
