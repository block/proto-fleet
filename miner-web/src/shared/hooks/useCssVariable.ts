import { useMemo } from "react";

/**
 * Configuration object for `useCssVariable`
 * 
 * @param variable - Css variable name
 * @param scope - Element at which the css variable value is being queried 
 * @param transform - Function to transform the value of the css variable
 */
type UseCssVariableOptions = {
  variable: string, 
  scope?: Element,
  transform?: (v: string) => any,
}
/**
 *  Custom hook to query the value of a css variable at a given element scope.  
 * 
 * @param options - Config object 
 * @returns value of the css variable
 */
const useCssVariable = ({
  variable,
  scope = document.documentElement,
  transform,
}: UseCssVariableOptions) => {
  const value = useMemo(() => {
    const v = window.getComputedStyle(scope).getPropertyValue(variable);
    if (transform) {
      return transform(v);
    } else {
      return v;
    }
  }, [variable, scope, transform]);

  return value;
};

export default useCssVariable;