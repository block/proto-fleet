interface ResponseErrorProps {
  error: {
    message: string;
  };
  status: number;
};

export type ErrorProps = ResponseErrorProps | undefined;
