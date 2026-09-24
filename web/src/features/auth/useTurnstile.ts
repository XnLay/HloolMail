import { useCallback, useEffect, useRef, useState } from 'react';

declare global {
  interface Window {
    turnstile?: {
      render: (
        container: string | HTMLElement,
        options: {
          sitekey: string;
          callback?: (token: string) => void;
          'error-callback'?: () => void;
          'expired-callback'?: () => void;
          theme?: string;
          appearance?: string;
        }
      ) => string;
      reset: (widgetId: string) => void;
      remove: (widgetId: string) => void;
    };
  }
}

const TURNSTILE_SCRIPT_SRC =
  'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';

export function useTurnstile(turnstileVisible: boolean, turnstileSiteKey: string) {
  const [turnstileToken, setTurnstileToken] = useState('');

  const [turnstileLoadError, setTurnstileLoadError] = useState(false);

  const turnstileWidgetId = useRef<string | null>(null);

  const turnstileContainerRef = useRef<HTMLDivElement>(null);

  const resetTurnstile = useCallback(() => {
    setTurnstileToken('');
    if (turnstileWidgetId.current && window.turnstile) {
      try {
        window.turnstile.reset(turnstileWidgetId.current);
      } catch {
        turnstileWidgetId.current = null;
      }
    }
  }, []);

  useEffect(() => {
    if (!turnstileVisible) {
      setTurnstileToken('');
      setTurnstileLoadError(false);
      return;
    }
    let cancelled = false;
    let loadTimeout: number | undefined;
    let script = document.querySelector<HTMLScriptElement>(
      'script[data-turnstile-api="true"], script[src*="challenges.cloudflare.com/turnstile/v0/api.js"]'
    );
    const renderWidget = () => {
      if (
        cancelled ||
        !window.turnstile ||
        !turnstileContainerRef.current ||
        turnstileWidgetId.current
      )
        return;
      try {
        turnstileWidgetId.current = window.turnstile.render(turnstileContainerRef.current, {
          sitekey: turnstileSiteKey,
          callback: (token: string) => {
            if (cancelled) return;
            setTurnstileToken(token);
            setTurnstileLoadError(false);
          },
          'error-callback': () => {
            if (cancelled) return;
            setTurnstileToken('');
            setTurnstileLoadError(true);
          },
          'expired-callback': () => {
            if (cancelled) return;
            setTurnstileToken('');
          },
          appearance: 'always',
          theme: 'auto',
        });
        setTurnstileLoadError(false);
      } catch {
        if (!cancelled) {
          setTurnstileToken('');
          setTurnstileLoadError(true);
        }
      }
    };
    const handleLoad = () => {
      if (loadTimeout) window.clearTimeout(loadTimeout);
      renderWidget();
    };
    const handleError = () => {
      if (loadTimeout) window.clearTimeout(loadTimeout);
      if (!cancelled) {
        setTurnstileToken('');
        setTurnstileLoadError(true);
      }
    };
    setTurnstileToken('');
    setTurnstileLoadError(false);
    if (window.turnstile) {
      renderWidget();
    } else {
      if (!script) {
        script = document.createElement('script');
        script.src = TURNSTILE_SCRIPT_SRC;
        script.async = true;
        script.defer = true;
        script.dataset.turnstileApi = 'true';
        document.head.appendChild(script);
      }
      script.addEventListener('load', handleLoad);
      script.addEventListener('error', handleError);
      loadTimeout = window.setTimeout(() => {
        if (!cancelled && !window.turnstile) {
          setTurnstileLoadError(true);
        }
      }, 8000);
    }
    return () => {
      cancelled = true;
      if (loadTimeout) window.clearTimeout(loadTimeout);
      script?.removeEventListener('load', handleLoad);
      script?.removeEventListener('error', handleError);
      if (turnstileWidgetId.current && window.turnstile) {
        try {
          window.turnstile.remove(turnstileWidgetId.current);
        } catch {
          // The widget may already be gone after a strict-mode remount.
        }
        turnstileWidgetId.current = null;
      }
    };
  }, [turnstileSiteKey, turnstileVisible]);
  return { turnstileToken, turnstileLoadError, turnstileContainerRef, resetTurnstile };
}
