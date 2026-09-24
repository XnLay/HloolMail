import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { apiMock, postJSONMock, resetTurnstileMock, passkeyMock, notifyMock, turnstile } =
  vi.hoisted(() => ({
    apiMock: vi.fn(),
    postJSONMock: vi.fn(),
    resetTurnstileMock: vi.fn(),
    passkeyMock: vi.fn(),
    notifyMock: vi.fn(),
    turnstile: { token: '' },
  }));

vi.mock('../../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api')>();
  return { ...actual, api: apiMock, postJSON: postJSONMock };
});

vi.mock('./useTurnstile', () => ({
  useTurnstile: () => ({
    turnstileToken: turnstile.token,
    turnstileLoadError: false,
    turnstileContainerRef: { current: null },
    resetTurnstile: resetTurnstileMock,
  }),
}));

vi.mock('../../lib/feedback', () => ({ notifySuccess: notifyMock }));
vi.mock('../../lib/passkeys', () => ({ loginWithPasskey: passkeyMock }));
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { AuthPanel } from './AuthPanel';
import { queryKeys } from '../../lib/queryKeys';
import type { PublicLoginSettings, RegisterResponse } from '../../types';

let root: Root;
let host: HTMLDivElement;
let queryClient: QueryClient;
let onDone: () => void;

function changeInput(id: string, value: string) {
  const input = document.getElementById(id);
  if (!(input instanceof HTMLInputElement)) throw new Error(`Missing input #${id}`);
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
  setValue?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

async function settle() {
  await new Promise((resolve) => window.setTimeout(resolve, 0));
}

const settings: PublicLoginSettings = {
  installed: true,
  registration_open: true,
  email_registration_enabled: true,
  oauth_providers: [],
  passkey_enabled: false,
  turnstile_enabled: false,
};
const registration: RegisterResponse = {
  verification_id: 'verification-1',
  email_verification_required: true,
  expires_at: '2026-09-25T12:00:00Z',
};

function input(id: string) {
  const element = document.getElementById(id);
  if (!(element instanceof HTMLInputElement)) throw new Error(`Missing input #${id}`);
  return element;
}

function button(selector: string) {
  const element = host.querySelector(selector);
  if (!(element instanceof HTMLButtonElement)) throw new Error(`Missing button ${selector}`);
  return element;
}

async function interact(action: () => void) {
  await act(async () => {
    action();
    await settle();
  });
}

async function selectTab(mode: 'login' | 'register') {
  await interact(() => button(`[role="tab"]:nth-child(${mode === 'login' ? 1 : 2})`).click());
}

async function fillRegistration() {
  await selectTab('register');
  await interact(() => {
    changeInput('auth-nickname', 'Mailbox User');
    changeInput('auth-email', 'user@example.test');
    changeInput('auth-password', 'secure-pass-123');
    changeInput('auth-confirm-password', 'secure-pass-123');
    if (document.getElementById('auth-captcha-answer')) changeInput('auth-captcha-answer', '4');
  });
}

describe('AuthPanel', () => {
  beforeEach(async () => {
    apiMock.mockReset().mockResolvedValue(settings);
    postJSONMock.mockReset().mockImplementation(async (path: string) => {
      if (path === '/api/auth/register/captcha') {
        return { captcha_id: 'captcha-1', challenge: '2 + 2 = ?' };
      }
      if (path === '/api/auth/register') {
        return registration;
      }
      if (path === '/api/auth/login') return { id: 7, email: 'user@example.test' };
      if (path === '/api/auth/register/verify') return { id: 7, email: 'user@example.test' };
      throw new Error(`Unexpected request: ${path}`);
    });
    resetTurnstileMock.mockReset();
    passkeyMock.mockReset().mockResolvedValue({ id: 7 });
    notifyMock.mockReset();
    turnstile.token = '';
    onDone = vi.fn<() => void>();
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    host = document.createElement('div');
    document.body.appendChild(host);
    root = createRoot(host);
    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <AuthPanel onDone={onDone} />
        </QueryClientProvider>
      );
      await settle();
    });
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    queryClient.clear();
    host.remove();
    vi.unstubAllEnvs();
    vi.clearAllMocks();
  });

  it('supports keyboard navigation and the email registration verification flow', async () => {
    const tabList = host.querySelector('[role="tablist"]');
    expect(tabList).not.toBeNull();
    await act(async () => {
      tabList?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
      await settle();
    });

    const selectedTab = host.querySelector('[role="tab"][aria-selected="true"]');
    expect(selectedTab).toBe(host.querySelectorAll('[role="tab"]')[1]);
    expect(document.getElementById('auth-email')?.getAttribute('aria-label')).toBeNull();
    expect(host.querySelector('label[for="auth-email"]')).not.toBeNull();

    await act(async () => {
      changeInput('auth-nickname', 'Mailbox User');
      changeInput('auth-email', 'user@example.test');
      changeInput('auth-password', 'secure-pass-123');
      changeInput('auth-confirm-password', 'secure-pass-123');
      changeInput('auth-captcha-answer', '4');
      await settle();
    });

    const requestCode = host.querySelector<HTMLButtonElement>('.auth-verification-request');
    expect(requestCode).not.toBeNull();
    await act(async () => {
      requestCode?.click();
      await settle();
    });

    expect(postJSONMock).toHaveBeenCalledWith(
      '/api/auth/register',
      expect.objectContaining({ email: 'user@example.test', captcha_id: 'captcha-1' })
    );
    expect(host.querySelector('#auth-verification-code')).not.toBeNull();

    await act(async () => {
      changeInput('auth-verification-code', '123456');
      const submit = host.querySelector<HTMLButtonElement>('button[type="submit"]');
      submit?.click();
      await settle();
    });

    expect(postJSONMock).toHaveBeenCalledWith('/api/auth/register/verify', {
      verification_id: 'verification-1',
      code: '123456',
    });
    expect(onDone).toHaveBeenCalledOnce();
  });

  it.each([
    ['nickname', 'Changed User'],
    ['email', 'changed@example.test'],
    ['password', 'changed-pass-456'],
    ['confirm-password', 'changed-pass-456'],
  ])('invalidates verification when %s changes', async (field, value) => {
    await fillRegistration();
    await interact(() => button('.auth-verification-request').click());
    await interact(() => changeInput('auth-verification-code', '123456'));
    expect(button('button[type="submit"]').disabled).toBe(false);

    await interact(() => changeInput(`auth-${field}`, value));
    expect(input('auth-verification-code').value).toBe('');
    expect(button('button[type="submit"]').disabled).toBe(true);
    // 即使绕过按钮禁用直接提交表单，失效的验证 ID 也不能再发送。
    await interact(() => {
      changeInput('auth-verification-code', '123456');
      host
        .querySelector('form')
        ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(postJSONMock).not.toHaveBeenCalledWith('/api/auth/register/verify', expect.anything());
  });

  it('preserves drafts across tabs while discarding verification state', async () => {
    await fillRegistration();
    await selectTab('login');
    expect(input('auth-email').value).toBe('user@example.test');
    expect(input('auth-password').value).toBe('secure-pass-123');
    expect(input('auth-password').autocomplete).toBe('current-password');
    await interact(() => changeInput('auth-email', 'changed@example.test'));
    await selectTab('register');
    expect(input('auth-email').value).toBe('changed@example.test');
    expect(input('auth-nickname').value).toBe('Mailbox User');
    expect(input('auth-confirm-password').value).toBe('secure-pass-123');
    expect(input('auth-captcha-answer').value).toBe('4');
    expect(input('auth-password').autocomplete).toBe('new-password');

    await interact(() => button('.auth-verification-request').click());
    await interact(() => changeInput('auth-verification-code', '123456'));
    await selectTab('login');
    await selectTab('register');
    expect(input('auth-verification-code').value).toBe('');
    expect(button('button[type="submit"]').disabled).toBe(true);
    expect(host.querySelector('#auth-verification-code-hint')).toBeNull();
  });

  it.each(['edit', 'switch tabs'])(
    'discards a delayed registration response after %s',
    async (action) => {
      await fillRegistration();
      let resolve!: (value: RegisterResponse) => void;
      postJSONMock.mockImplementationOnce(
        () =>
          new Promise<RegisterResponse>((done) => {
            resolve = done;
          })
      );
      await interact(() => button('.auth-verification-request').click());
      if (action === 'switch tabs') await selectTab('login');
      await interact(() => changeInput('auth-email', 'new@example.test'));
      if (action === 'switch tabs') await selectTab('register');
      await interact(() => resolve(registration));
      expect(input('auth-email').value).toBe('new@example.test');
      expect(button('button[type="submit"]').disabled).toBe(true);
      expect(host.querySelector('#auth-verification-code-hint')).toBeNull();
      expect(notifyMock).not.toHaveBeenCalled();
      // 当前表单刷新已消耗的挑战；旧表单的回调不能清空新表单的草稿。
      expect(input('auth-captcha-answer').value).toBe(action === 'edit' ? '' : '4');
    }
  );

  it('submits password login without mounting registration queries', async () => {
    await interact(() => {
      changeInput('auth-email', 'user@example.test');
      changeInput('auth-password', 'secure-pass-123');
    });
    await interact(() => button('button[type="submit"]').click());
    expect(postJSONMock).toHaveBeenCalledWith('/api/auth/login', {
      email: 'user@example.test',
      password: 'secure-pass-123',
      turnstile_token: '',
    });
    expect(postJSONMock).not.toHaveBeenCalledWith('/api/auth/register/captcha', expect.anything());
    expect(onDone).toHaveBeenCalledOnce();
  });

  it('supports passkey login', async () => {
    await interact(() => {
      queryClient.setQueryData(queryKeys.loginSettings, { ...settings, passkey_enabled: true });
    });
    await interact(() => changeInput('auth-email', 'user@example.test'));
    await interact(() => button('.btn-secondary.auth-submit').click());
    expect(passkeyMock).toHaveBeenCalledWith('user@example.test');
    expect(onDone).toHaveBeenCalledOnce();
  });

  it('requires Turnstile for password login and uses it for registration', async () => {
    await interact(() => {
      queryClient.setQueryData(queryKeys.loginSettings, {
        ...settings,
        turnstile_enabled: true,
        turnstile_site_key: 'site-key',
      });
    });
    expect(button('button[type="submit"]').disabled).toBe(true);
    await interact(() => {
      turnstile.token = 'proof-token';
      changeInput('auth-email', 'user@example.test');
      changeInput('auth-password', 'secure-pass-123');
    });
    expect(button('button[type="submit"]').disabled).toBe(false);
    await fillRegistration();
    expect(host.querySelector('#auth-captcha-answer')).toBeNull();
    await interact(() => button('.auth-verification-request').click());
    expect(postJSONMock).toHaveBeenCalledWith('/api/auth/register', {
      nickname: 'Mailbox User',
      email: 'user@example.test',
      password: 'secure-pass-123',
      turnstile_token: 'proof-token',
    });
    expect(resetTurnstileMock).toHaveBeenCalledOnce();
  });

  it('supports OAuth-only registration and closed registration', async () => {
    await interact(() => {
      queryClient.setQueryData(queryKeys.loginSettings, {
        ...settings,
        email_registration_enabled: false,
        oauth_providers: [
          { provider: 'github', name: 'GitHub', auth_url: 'https://example.test/oauth' },
        ],
      });
    });
    await selectTab('register');
    expect(host.querySelector('form')).toBeNull();
    expect(host.querySelectorAll('.oauth-login-button')).toHaveLength(1);
    expect(postJSONMock).not.toHaveBeenCalledWith('/api/auth/register/captcha', expect.anything());
    await interact(() => {
      queryClient.setQueryData(queryKeys.loginSettings, { ...settings, registration_open: false });
    });
    expect(host.querySelector('form')).toBeNull();
    expect(host.querySelector('[role="alert"]')).not.toBeNull();
    await interact(() => button('.oauth-error-alert button').click());
    expect(host.querySelector('#auth-email')).not.toBeNull();
  });

  it('tracks queued verification delivery and reports success once', async () => {
    await fillRegistration();
    postJSONMock.mockResolvedValueOnce({
      ...registration,
      delivery_id: 'delivery-1',
      email_delivery_status: 'pending',
    });
    apiMock.mockResolvedValueOnce({ id: 'delivery-1', status: 'pending' });
    await interact(() => button('.auth-verification-request').click());
    expect(button('.auth-verification-request').disabled).toBe(true);
    expect(notifyMock).toHaveBeenCalledTimes(1);
    await interact(() => {
      queryClient.setQueryData(queryKeys.loginSettingsEmailDelivery('delivery-1'), {
        id: 'delivery-1',
        status: 'succeeded',
      });
    });
    expect(button('.auth-verification-request').disabled).toBe(false);
    expect(notifyMock).toHaveBeenCalledTimes(2);
    await interact(() => changeInput('auth-verification-code', '123456'));
    expect(notifyMock).toHaveBeenCalledTimes(2);
  });
});
