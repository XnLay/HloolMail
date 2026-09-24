import { useMutation } from '@tanstack/react-query';
import { ArrowRight, Fingerprint } from 'lucide-react';
import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { postJSON, type User } from '../../api';
import { LoadingIndicator } from '../../components/shared';
import { notifySuccess } from '../../lib/feedback';
import { loginWithPasskey } from '../../lib/passkeys';
import { useText } from '../../locales';
import type { PublicLoginSettings } from '../../types';
import { AuthField, TurnstileField } from './AuthField';
import { focusFirstInvalidField, type AuthFieldErrors, type AuthFormProps } from './authForm';
import { useTurnstile } from './useTurnstile';

export function LoginForm({
  draft,
  onDraftChange,
  onDone,
  settings,
}: AuthFormProps & {
  settings: PublicLoginSettings | undefined;
}) {
  const text = useText();
  const submitRef = useRef<HTMLButtonElement>(null);
  const passkeyRef = useRef<HTMLButtonElement>(null);
  const [errors, setErrors] = useState<AuthFieldErrors>({});
  const turnstileEnabled = Boolean(settings?.turnstile_enabled && settings.turnstile_site_key);
  const { turnstileToken, turnstileLoadError, turnstileContainerRef, resetTurnstile } =
    useTurnstile(turnstileEnabled, settings?.turnstile_site_key || '');

  const login = useMutation({
    mutationFn: () =>
      postJSON<User>('/api/auth/login', {
        email: draft.email.trim(),
        password: draft.password,
        turnstile_token: turnstileToken,
      }),
    onSuccess: () => {
      notifySuccess(text.toast.loginDone, { origin: submitRef.current });
      onDone();
    },
    onError: (error) => {
      resetTurnstile();
      toast.error(error.message);
    },
  });
  const passkey = useMutation({
    mutationFn: () => loginWithPasskey(draft.email.trim()),
    onSuccess: () => {
      notifySuccess(text.toast.loginDone, { origin: passkeyRef.current });
      onDone();
    },
    onError: (error) => toast.error(error.message),
  });
  const pending = login.isPending || passkey.isPending;

  const changeCredential = (field: 'email' | 'password', value: string) => {
    onDraftChange(field, value);
    setErrors((current) => ({ ...current, [field]: undefined }));
  };
  const validateEmail = () => {
    const nextErrors = draft.email.trim() ? {} : { email: text.login.emailRequired };
    setErrors(nextErrors);
    focusFirstInvalidField(nextErrors);
    return !nextErrors.email;
  };

  return (
    <form
      className="auth-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (!pending && (!turnstileEnabled || turnstileToken) && validateEmail()) login.mutate();
      }}
    >
      <AuthField
        name="email"
        label={text.login.email}
        value={draft.email}
        onValueChange={(value) => changeCredential('email', value)}
        error={errors.email}
        placeholder={text.login.email}
        type="email"
        autoComplete="email"
      />
      <AuthField
        name="password"
        label={text.login.password}
        value={draft.password}
        onValueChange={(value) => changeCredential('password', value)}
        error={errors.password}
        placeholder={text.login.password}
        type="password"
        autoComplete="current-password"
      />
      {turnstileEnabled && (
        <TurnstileField containerRef={turnstileContainerRef} loadError={turnstileLoadError} />
      )}
      <button
        ref={submitRef}
        className="btn-primary auth-submit"
        type="submit"
        disabled={pending || (turnstileEnabled && !turnstileToken)}
      >
        {login.isPending ? (
          <LoadingIndicator className="auth-submit-loading" label={text.login.loginPending} />
        ) : (
          <>
            {text.login.submit}
            <ArrowRight size={16} />
          </>
        )}
      </button>
      {settings?.passkey_enabled && (
        <button
          ref={passkeyRef}
          className="btn-secondary auth-submit"
          type="button"
          disabled={pending}
          onClick={() => {
            if (validateEmail()) passkey.mutate();
          }}
        >
          {passkey.isPending ? (
            <LoadingIndicator className="auth-submit-loading" label={text.login.passkeyPending} />
          ) : (
            <>
              <Fingerprint size={16} />
              {text.login.passkeySubmit}
            </>
          )}
        </button>
      )}
    </form>
  );
}
