interface CaretProps {
  className?: string;
}

const Caret = ({ className }: CaretProps) => {
  return (
    <svg width="18" height="19" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
      <g clipPath="url(#a)">
        <path
          d="m4 6.5 5 5 5-5"
          stroke="#C6C6C6"
          strokeWidth="2"
          strokeMiterlimit="10"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </g>
      <defs>
        <clipPath id="a">
          <path
            fill="#fff"
            transform="rotate(90 8.75 9.25)"
            d="M0 0h18v18H0z"
          />
        </clipPath>
      </defs>
    </svg>
  );
};

export default Caret;
