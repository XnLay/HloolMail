import { useMutation, useQuery } from '@tanstack/react-query';
import { MailCheck, RefreshCcw } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { postJSON, type User } from '../../api';
import { LoadingIndicator } from '../../components/shared';
import { notifySuccess } from '../../lib/feedback';
import { normalizeNicknameInput, validateNicknameInput } from '../../lib/userDisplay';
import { useText } from '../../locales';
import type { PublicLoginSettings, RegisterCaptcha, RegisterResponse } from '../../types';
import { AuthField, TurnstileField } from './AuthField';
import {
  focusFirstInvalidField,
  type AuthDraft,
  type AuthFieldErrors,
  type AuthFormProps,
} from './authForm';
import { useTurnstile } from './useTurnstile';
import { useVerificationDelivery } from './useVerificationDelivery';

type RegistrationRequest = Pick<AuthDraft, 'email' | 'nickname' | 'password'> &
  ({ turnstile_token: string } | { captcha_id: string; captcha_answer: string });
type VerificationState = {
  response: RegisterResponse | null;
  code: string;
  errors: AuthFieldErrors;
};

export function RegistrationForm({
  draft,
  onDraftChange,
  onDone,
  settings,
}: AuthFormProps & {
  settings: PublicLoginSettings;
}) {
  const text = useText();
  const submitRef = useRef<HTMLButtonElement>(null);
  const requestRef = useRef<HTMLButtonElement>(null);
  const revision = useRef(0);
  const mounted = useRef(true);
  const [verification, setVerification] = useState<VerificationState>({
    response: null,
    code: '',
    errors: {},
  });
  const turnstileEnabled = Boolean(settings.turnstile_enabled && settings.turnstile_site_key);
  const { turnstileToken, turnstileLoadError, turnstileContainerRef, resetTurnstile } =
    useTurnstile(turnstileEnabled, settings.turnstile_site_key || '');
  const captcha = useQuery({
    queryKey: ['register-captcha'],
    queryFn: () => postJSON<RegisterCaptcha>('/api/auth/register/captcha', {}),
    enabled: !turnstileEnabled,
    retry: false,
  });
  const delivery = useVerificationDelivery(verification.response, draft.email, requestRef);

  // 字段版本拦截迟到响应；卸载后也不能再修改父面板保留的草稿。
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const changeField = (field: keyof AuthDraft, value: string) => {
    const invalidatesVerification = field !== 'captchaAnswer';
    if (invalidatesVerification) revision.current += 1;
    onDraftChange(field, value);
    setVerification((current) => ({
      response: invalidatesVerification ? null : current.response,
      code: invalidatesVerification ? '' : current.code,
      errors: { ...current.errors, [field]: undefined },
    }));
  };
  const refreshCaptcha = () => {
    changeField('captchaAnswer', '');
    void captcha.refetch();
  };
  const resetChallenge = () => {
    resetTurnstile();
    if (!turnstileEnabled) refreshCaptcha();
  };
  const register = useMutation({
    mutationFn: ({ body }: { body: RegistrationRequest; revision: number }) =>
      postJSON<RegisterResponse>('/api/auth/register', body),
    onSuccess: (response, request) => {
      if (!mounted.current) return;
      // 请求已消耗人机验证凭据，即使草稿已变，也要刷新后才能再次申请。
      resetChallenge();
      if (request.revision !== revision.current) return;
      setVerification({ response, code: '', errors: {} });
    },
    onError: (error, request) => {
      if (!mounted.current) return;
      resetChallenge();
      if (request.revision !== revision.current) return;
      toast.error(error.message);
    },
  });
  const verify = useMutation({
    mutationFn: (body: { verification_id: string; code: string }) =>
      postJSON<User>('/api/auth/register/verify', body),
    onSuccess: () => {
      notifySuccess(text.login.registerDone, { origin: submitRef.current });
      onDone();
    },
    onError: (error) => toast.error(error.message),
  });
  const pending = register.isPending || verify.isPending;
  const requestBlocked =
    pending ||
    delivery.inProgress ||
    (turnstileEnabled ? !turnstileToken : captcha.isLoading || !captcha.data?.captcha_id);

  const showErrors = (errors: AuthFieldErrors) => {
    setVerification((current) => ({ ...current, errors }));
    focusFirstInvalidField(errors);
    return Object.values(errors).some(Boolean);
  };
  const requestVerification = () => {
    if (requestBlocked) return;
    const errors: AuthFieldErrors = {};
    const nickname = normalizeNicknameInput(draft.nickname);
    const nicknameError = validateNicknameInput(nickname, {
      required: text.login.nicknameRequired,
      tooLong: text.login.nicknameTooLong,
      invalid: text.login.nicknameInvalid,
    });
    if (nicknameError) errors.nickname = nicknameError;
    if (!draft.email.trim()) errors.email = text.login.emailRequired;
    if (draft.password.length < 8) errors.password = text.login.passwordTooShort;
    if (draft.password !== draft.confirmPassword)
      errors.confirmPassword = text.login.passwordMismatch;
    if (!turnstileEnabled && !draft.captchaAnswer.trim())
      errors.captchaAnswer = text.login.captchaAnswerRequired;
    if (showErrors(errors)) return;

    const credentials = { nickname, email: draft.email.trim(), password: draft.password };
    if (turnstileEnabled) {
      register.mutate({
        body: { ...credentials, turnstile_token: turnstileToken },
        revision: revision.current,
      });
    } else if (captcha.data) {
      register.mutate({
        body: {
          ...credentials,
          captcha_id: captcha.data.captcha_id,
          captcha_answer: draft.captchaAnswer.trim(),
        },
        revision: revision.current,
      });
    }
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending) return;
    const response = verification.response;
    const code = verification.code.trim();
    if (!response) {
      showErrors({
        verificationCode: text.login.verificationRequestRequired || text.login.verificationMissing,
      });
    } else if (!code) {
      showErrors({ verificationCode: text.login.verificationCodeRequired });
    } else {
      showErrors({});
      verify.mutate({ verification_id: response.verification_id, code });
    }
  };
  const { errors } = verification;
  const changeVerificationCode = (code: string) => {
    setVerification((current) => ({
      ...current,
      code,
      errors: { ...current.errors, verificationCode: undefined },
    }));
  };

  return (
    <form className="auth-form" onSubmit={submit}>
      <AuthField
        name="nickname"
        label={text.login.nickname}
        value={draft.nickname}
        onValueChange={(value) => changeField('nickname', value)}
        error={errors.nickname}
        placeholder={text.login.nicknamePlaceholder}
        autoComplete="nickname"
        maxLength={80}
      />
      <AuthField
        name="email"
        label={text.login.email}
        value={draft.email}
        onValueChange={(value) => changeField('email', value)}
        error={errors.email}
        placeholder={text.login.email}
        type="email"
        autoComplete="email"
      />
      <AuthField
        name="password"
        label={text.login.password}
        value={draft.password}
        onValueChange={(value) => changeField('password', value)}
        error={errors.password}
        placeholder={text.login.password}
        type="password"
        autoComplete="new-password"
        aria-describedby="auth-password-hint"
      />
      <p id="auth-password-hint" className="auth-password-hint">
        {text.login.passwordHint}
      </p>
      <div className="auth-confirm-wrapper">
        <AuthField
          name="confirmPassword"
          label={text.login.confirmPassword}
          value={draft.confirmPassword}
          onValueChange={(value) => changeField('confirmPassword', value)}
          error={errors.confirmPassword}
          placeholder={text.login.confirmPassword}
          type="password"
          autoComplete="new-password"
        />
      </div>
      {turnstileEnabled ? (
        <TurnstileField containerRef={turnstileContainerRef} loadError={turnstileLoadError} />
      ) : (
        <div className="auth-captcha-box">
          <div className="auth-captcha-challenge">
            <span>
              {captcha.isLoading
                ? text.login.captchaLoading
                : captcha.data?.challenge || text.login.captchaLoadError}
            </span>
            <button
              className="icon-button"
              type="button"
              onClick={refreshCaptcha}
              disabled={pending || captcha.isFetching}
              aria-label={text.login.captchaRefresh}
            >
              <RefreshCcw size={15} />
            </button>
          </div>
          <AuthField
            name="captchaAnswer"
            label={text.login.captchaAnswer}
            value={draft.captchaAnswer}
            onValueChange={(value) => changeField('captchaAnswer', value)}
            error={errors.captchaAnswer}
            placeholder={text.login.captchaAnswer}
            autoComplete="off"
          />
        </div>
      )}
      <div className="auth-verification-row">
        <div>
          <AuthField
            name="verificationCode"
            label={text.login.verificationCode}
            value={verification.code}
            onValueChange={changeVerificationCode}
            error={errors.verificationCode}
            placeholder={text.login.verificationCode}
            inputMode="numeric"
            autoComplete="one-time-code"
            style={{ width: '100%' }}
            aria-describedby={delivery.hint ? 'auth-verification-code-hint' : undefined}
          />
        </div>
        <button
          ref={requestRef}
          className="btn-secondary auth-verification-request"
          type="button"
          disabled={requestBlocked}
          onClick={requestVerification}
        >
          {register.isPending || delivery.inProgress ? (
            <LoadingIndicator
              className="auth-submit-loading"
              label={
                delivery.inProgress
                  ? text.login.verificationDeliverySending
                  : text.login.verificationRequestPending
              }
            />
          ) : verification.response ? (
            text.login.resendVerificationCode
          ) : (
            text.login.requestVerificationCode
          )}
        </button>
      </div>
      {delivery.hint && (
        <p id="auth-verification-code-hint" className="auth-verification-hint">
          {delivery.hint}
        </p>
      )}
      <button
        ref={submitRef}
        className="btn-primary auth-submit"
        type="submit"
        disabled={pending || !verification.response || !verification.code.trim()}
      >
        {verify.isPending ? (
          <LoadingIndicator
            className="auth-submit-loading"
            label={text.login.verificationPending}
          />
        ) : (
          <>
            {text.login.verificationSubmit}
            <MailCheck size={16} />
          </>
        )}
      </button>
    </form>
  );
}
