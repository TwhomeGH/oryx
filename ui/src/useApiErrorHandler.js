import React from 'react';
import {useErrorHandler} from 'react-error-boundary';

/**
 * Error handler for API calls that keeps the current view on transient network failures.
 *
 * react-error-boundary's useErrorHandler unmounts the page subtree and shows the fallback, which
 * throws away what the user was reading just because the management service blipped. A network
 * error (an AxiosError without a response) is reported through a global event instead, so the
 * last rendered data stays on screen and only the offline banner changes; real server errors keep
 * the existing fallback behaviour.
 */
export function useApiErrorHandler() {
  const handleError = useErrorHandler();

  return React.useCallback((error) => {
    const isNetwork = !!error && (error.name === 'AxiosError' || error.isAxiosError) && !error.response;
    if (isNetwork) {
      window.dispatchEvent(new Event('oryx:server-unreachable'));
      console.warn(`Server unreachable, keeping the current view: ${error.message}`);
      return;
    }
    handleError(error);
  }, [handleError]);
}
