// Additional types for API responses that are not included in the generatedApi.ts
interface ResponseErrorProps {
  error: {
    message: string;
  };
  status: number;
}

export type ErrorProps = ResponseErrorProps | undefined;

// TODO BE error messages should be consistent across all EPs
interface ResponseSimpleErrorProps {
  error: string;
  status: number;
}

export type SimpleErrorProps = ResponseSimpleErrorProps | undefined;
