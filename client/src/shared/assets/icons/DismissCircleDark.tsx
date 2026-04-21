import InteractiveIcon from "./InteractiveIcon";
import { IconProps } from "./types";

const DismissCircle = ({ width = "w-[28px]", ...props }: IconProps) => {
  return (
    <InteractiveIcon {...props} width={width}>
      <svg width="100%" height="100%" viewBox="0 0 28 28" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path
          d="M0 14C0 6.26801 6.26801 0 14 0C21.732 0 28 6.26801 28 14C28 21.732 21.732 28 14 28C6.26801 28 0 21.732 0 14Z"
          fill="currentColor"
          fillOpacity="0.05"
        />
        <rect width="16" height="16" transform="translate(6 6)" fill="white" fillOpacity="0.02" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M8.81291 8.81291C9.20344 8.42239 9.8366 8.42239 10.2271 8.81291L14 12.5858L17.7729 8.81291C18.1634 8.42239 18.7966 8.42239 19.1871 8.81291C19.5777 9.20344 19.5777 9.8366 19.1871 10.2271L15.4142 14L19.1871 17.7729C19.5777 18.1634 19.5777 18.7966 19.1871 19.1871C18.7966 19.5777 18.1634 19.5777 17.7729 19.1871L14 15.4142L10.2271 19.1871C9.8366 19.5777 9.20344 19.5777 8.81291 19.1871C8.42239 18.7966 8.42239 18.1634 8.81291 17.7729L12.5858 14L8.81291 10.2271C8.42239 9.8366 8.42239 9.20344 8.81291 8.81291Z"
          fill="currentColor"
          fillOpacity="0.7"
        />
      </svg>
    </InteractiveIcon>
  );
};

export default DismissCircle;
