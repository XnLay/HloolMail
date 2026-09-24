import { CircleAlert, CircleUserRound, Github, Home } from 'lucide-react';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useText } from '../../locales';
import type { AuthDraft, AuthMode } from './authForm';
import { LoginForm } from './LoginForm';
import { oauthErrorFromLocation, oauthProviderDisplayName } from './oauthErrors';
import { usePublicLoginSettings } from './queries';
import { RegistrationForm } from './RegistrationForm';

export type AuthPanelProps = { onDone: () => void; initialMode?: AuthMode };

export function AuthPanel({ onDone, initialMode = 'login' }: AuthPanelProps) {
  const text = useText();
  const [mode, setMode] = useState<AuthMode>(initialMode);
  const [draft, setDraft] = useState<AuthDraft>({
    nickname: '',
    email: '',
    password: '',
    confirmPassword: '',
    captchaAnswer: '',
  });
  const loginTabRef = useRef<HTMLButtonElement>(null);
  const registerTabRef = useRef<HTMLButtonElement>(null);
  const {
    loginSettings,
    oauthProviderRows,
    registrationOpen,
    emailRegistrationAvailable,
    oauthRegistrationAvailable,
    registrationAvailable,
  } = usePublicLoginSettings();
  const settingsResolved = loginSettings.isSuccess || loginSettings.isError;
  const registerModeSelectable = !settingsResolved || registrationAvailable;
  const isRegister = mode === 'register';
  const registrationUnavailable = isRegister && settingsResolved && !registrationAvailable;
  const oauthOnlyRegistration =
    isRegister && !emailRegistrationAvailable && oauthRegistrationAvailable;
  const visibleProviders = isRegister && !oauthRegistrationAvailable ? [] : oauthProviderRows;
  const oauthError = oauthErrorFromLocation();
  const oauthErrorProvider = oauthError?.provider
    ? oauthProviderDisplayName(oauthError.provider)
    : text.oauth.title;

  useEffect(() => {
    setMode(initialMode);
  }, [initialMode]);
  const selectMode = (nextMode: AuthMode) => {
    if (nextMode === 'register' && !registerModeSelectable) return;
    setMode(nextMode);
  };
  const handleTabKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    let nextMode: AuthMode;
    switch (event.key) {
      case 'ArrowRight':
      case 'ArrowDown':
      case 'ArrowLeft':
      case 'ArrowUp':
        nextMode = mode === 'login' && registerModeSelectable ? 'register' : 'login';
        break;
      case 'Home':
        nextMode = 'login';
        break;
      case 'End':
        nextMode = registerModeSelectable ? 'register' : 'login';
        break;
      default:
        return;
    }
    event.preventDefault();
    selectMode(nextMode);
    const tab = nextMode === 'login' ? loginTabRef : registerTabRef;
    window.requestAnimationFrame(() => tab.current?.focus());
  };
  const changeDraft = (field: keyof AuthDraft, value: string) => {
    setDraft((current) => ({ ...current, [field]: value }));
  };

  return (
    <section
      id="auth-panel"
      className="auth-panel"
      aria-label={isRegister ? text.login.registerTitle : text.login.title}
    >
      <div
        className="auth-tabs"
        role="tablist"
        aria-orientation="horizontal"
        aria-label={text.login.title}
        onKeyDown={handleTabKeyDown}
      >
        <button
          ref={loginTabRef}
          className={!isRegister ? 'auth-tab-active' : ''}
          type="button"
          role="tab"
          aria-selected={!isRegister}
          tabIndex={!isRegister ? 0 : -1}
          onClick={() => selectMode('login')}
        >
          {text.login.loginTab}
        </button>
        {(registerModeSelectable || isRegister) && (
          <button
            ref={registerTabRef}
            className={isRegister ? 'auth-tab-active' : ''}
            type="button"
            role="tab"
            aria-selected={isRegister}
            tabIndex={isRegister ? 0 : -1}
            onClick={() => selectMode('register')}
          >
            {text.login.registerTab}
          </button>
        )}
      </div>
      <div className="auth-heading">
        <h2>{isRegister ? text.login.registerTitle : text.login.title}</h2>
        <p>
          {registrationUnavailable
            ? text.login.registrationUnavailableAdmin
            : oauthOnlyRegistration
              ? text.login.oauthOnlyRegisterDesc
              : isRegister
                ? text.login.registerDesc
                : text.login.desc}
        </p>
      </div>
      {oauthError?.code === 'registration_closed' && (
        <RegistrationAlert
          title={text.login.oauthRegistrationClosedTitle}
          description={text.login.oauthRegistrationClosedDesc.replace(
            '{provider}',
            oauthErrorProvider
          )}
          onLogin={() => {
            window.location.hash = '#/login';
          }}
        />
      )}
      {registrationUnavailable && (
        <RegistrationAlert
          title={text.login.registrationUnavailableTitle}
          description={text.login.registrationUnavailableAdmin}
          onLogin={() => selectMode('login')}
        />
      )}
      {/* 草稿跨 Tab 保留，验证结果、错误和验证码实例随各自表单卸载。 */}
      {!isRegister ? (
        <LoginForm
          draft={draft}
          onDraftChange={changeDraft}
          onDone={onDone}
          settings={loginSettings.data}
        />
      ) : !settingsResolved ? (
        <AuthPanelSkeleton label={text.common.loading} />
      ) : emailRegistrationAvailable && loginSettings.data ? (
        <RegistrationForm
          draft={draft}
          onDraftChange={changeDraft}
          onDone={onDone}
          settings={loginSettings.data}
        />
      ) : null}
      {visibleProviders.length > 0 && (
        <div
          className={`oauth-login-box${oauthOnlyRegistration ? ' oauth-login-box-primary' : ''}`}
        >
          {!oauthOnlyRegistration && (
            <div className="oauth-login-divider">
              <span>{text.oauth.title}</span>
            </div>
          )}
          <div className="oauth-login-actions">
            {visibleProviders.map((provider) => {
              const Icon = provider.provider === 'github' ? Github : CircleUserRound;
              const action = isRegister
                ? text.oauth.register_with
                : registrationOpen
                  ? text.oauth.continue_with
                  : text.oauth.login_with;
              const label = action.replace('{provider}', provider.name);
              return (
                <button
                  className="oauth-login-button"
                  type="button"
                  key={provider.provider}
                  aria-label={label}
                  onClick={() => {
                    window.location.href = provider.auth_url;
                  }}
                >
                  <Icon size={17} />
                  {label}
                </button>
              );
            })}
          </div>
        </div>
      )}
      {loginSettings.isError && <p className="oauth-error-fallback">{text.oauth.load_error}</p>}
      <p className="auth-page-terms">
        {text.login.termsPrefix || 'By continuing, you agree to our '}
        <a href="#/terms">{text.login.termsService || 'Terms of Service'}</a>{' '}
        {text.login.termsAnd || 'and'}{' '}
        <a href="#/privacy">{text.login.privacyPolicy || 'Privacy Policy'}</a>.
      </p>
    </section>
  );
}

function RegistrationAlert({
  title,
  description,
  onLogin,
}: {
  title: string;
  description: string;
  onLogin: () => void;
}) {
  const text = useText();
  return (
    <div className="oauth-error-alert" role="alert">
      <span className="oauth-error-alert-icon">
        <CircleAlert size={18} aria-hidden="true" />
      </span>
      <div className="oauth-error-alert-content">
        <h3>{title}</h3>
        <p>{description}</p>
        <button className="btn-secondary" type="button" onClick={onLogin}>
          <Home size={15} aria-hidden="true" />
          {text.login.oauthRegistrationClosedAction}
        </button>
      </div>
    </div>
  );
}

function AuthPanelSkeleton({ label }: { label: string }) {
  return (
    <div className="auth-loading-state" role="status" aria-live="polite" aria-busy="true">
      <span className="sr-only">{label}</span>
      <div className="auth-form-skeleton" aria-hidden="true">
        <span className="auth-skeleton-line auth-skeleton-line-wide" />
        <span className="auth-skeleton-input" />
        <span className="auth-skeleton-input" />
        <span className="auth-skeleton-line" />
        <span className="auth-skeleton-button" />
        <span className="auth-skeleton-divider" />
        <span className="auth-skeleton-button auth-skeleton-button-muted" />
      </div>
    </div>
  );
}
