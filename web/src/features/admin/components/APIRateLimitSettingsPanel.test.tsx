import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api';
import { queryKeys } from '../../../lib/queryKeys';
import { clearUserSession } from '../../../lib/queryClient';
import type { APIRateLimitSettings, UpdateAPIRateLimitSettings } from '../../../types/rateLimits';
import { rateLimitTestSettings } from '../utils/rateLimitTestData';
import { APIRateLimitSettingsPanel } from './APIRateLimitSettingsPanel';

const { loadMock, saveMock, notifyMock, errorMock } = vi.hoisted(() => ({
  loadMock: vi.fn(),
  saveMock: vi.fn(),
  notifyMock: vi.fn(),
  errorMock: vi.fn(),
}));

vi.mock('../services/adminService', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/adminService')>()),
  fetchAPIRateLimitSettings: loadMock,
  saveAPIRateLimitSettings: saveMock,
}));
vi.mock('../../../locales', async () => {
  const { default: text } = await import('../../../locales/en-US');
  return { useText: () => text };
});
vi.mock('../../../lib/feedback', () => ({ notifySuccess: notifyMock }));
vi.mock('sonner', () => ({ toast: { error: errorMock } }));

let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
let settings: APIRateLimitSettings;
let onDirtyChange: ReturnType<typeof vi.fn<(dirty: boolean) => void>>;

function input(rule: string, field = 'rps') {
  const element = host.querySelector('#rate-limit-' + rule + '-' + field);
  if (!(element instanceof HTMLInputElement)) throw new Error('Missing rate field: ' + rule);
  return element;
}

function action(index: number) {
  const element = host.querySelectorAll('.api-rate-limit-actions button')[index];
  if (!(element instanceof HTMLButtonElement)) throw new Error('Missing action: ' + index);
  return element;
}

function change(rule: string, value: string, field = 'rps') {
  const element = input(rule, field);
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(element, value);
  element.dispatchEvent(new Event('input', { bubbles: true }));
}

async function settle() {
  await new Promise((resolve) => window.setTimeout(resolve, 0));
}

async function interact(run: () => void) {
  await act(async () => {
    run();
    await settle();
  });
  await act(settle);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

describe('APIRateLimitSettingsPanel', () => {
  beforeEach(async () => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    settings = rateLimitTestSettings();
    loadMock.mockReset().mockImplementation(async () => structuredClone(settings));
    saveMock.mockReset().mockImplementation(async (payload: UpdateAPIRateLimitSettings) => {
      settings = {
        ...settings,
        ...payload,
        revision: payload.revision + 1,
        applied_revision: payload.revision + 1,
        updated_by: 'admin@example.test',
      };
      return structuredClone(settings);
    });
    notifyMock.mockReset();
    errorMock.mockReset();
    onDirtyChange = vi.fn();
    client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    client.setQueryData(queryKeys.admin.rateLimitSettings, structuredClone(settings));
    host = document.createElement('div');
    document.body.appendChild(host);
    root = createRoot(host);
    await interact(() =>
      root.render(
        <QueryClientProvider client={client}>
          <APIRateLimitSettingsPanel onDirtyChange={onDirtyChange} />
        </QueryClientProvider>
      )
    );
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    client.clear();
    host.remove();
    vi.unstubAllGlobals();
  });

  it('starts clean and associates invalid fields with inline errors', async () => {
    expect(host.querySelectorAll('fieldset')).toHaveLength(7);
    expect(action(2).disabled).toBe(true);
    await interact(() => change('mail', ''));
    expect(input('mail').getAttribute('aria-invalid')).toBe('true');
    expect(input('mail').getAttribute('aria-describedby')).toBe('rate-limit-mail-rate-error');
    expect(host.querySelector('#rate-limit-mail-rate-error')?.textContent).toContain('0.001');
    expect(action(2).disabled).toBe(true);
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    await interact(() => change('mail', '100'));
    expect(input('mail').getAttribute('aria-invalid')).toBe('false');
    expect(host.querySelector('.api-rate-limit-bottleneck')?.textContent).toContain('5');
    expect(action(2).disabled).toBe(false);
  });

  it('saves one complete configuration and updates the baseline', async () => {
    await interact(() => change('mail', '100'));
    await interact(() => action(2).click());
    expect(saveMock).toHaveBeenCalledTimes(1);
    expect(saveMock.mock.calls[0][0]).toEqual({ revision: 1, config: settings.config });
    expect(settings.config.business.mail.requests_per_second).toBe(100);
    expect(action(2).disabled).toBe(true);
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    expect(
      client.getQueryData<APIRateLimitSettings>(queryKeys.admin.rateLimitSettings)?.revision
    ).toBe(2);
    expect(notifyMock).toHaveBeenCalledTimes(1);
  });

  it('keeps failed input and allows a retry without losing edits', async () => {
    saveMock.mockRejectedValueOnce(new ApiError('storage unavailable', 500));
    await interact(() => change('mail', '100'));
    await interact(() => action(2).click());
    expect(input('mail').value).toBe('100');
    expect(action(2).disabled).toBe(false);
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    expect(errorMock).toHaveBeenCalledWith('storage unavailable');
    await interact(() => action(2).click());
    expect(action(2).disabled).toBe(true);
    expect(saveMock).toHaveBeenCalledTimes(2);
  });

  it('locks fields during saving and persists explicit disabled rules', async () => {
    const pending = deferred<APIRateLimitSettings>();
    saveMock.mockReturnValueOnce(pending.promise);
    const checkbox = host.querySelector('fieldset input[type="checkbox"]');
    if (!(checkbox instanceof HTMLInputElement)) throw new Error('Missing enable switch');
    await interact(() => checkbox.click());
    await interact(() => action(2).click());
    expect(host.querySelector('fieldset')?.disabled).toBe(true);
    expect(action(0).disabled).toBe(true);
    expect(saveMock.mock.calls[0][0].config.pre_auth.per_ip.enabled).toBe(false);
    const saved = structuredClone(settings);
    saved.revision = 2;
    saved.config.pre_auth.per_ip.enabled = false;
    await interact(() => pending.resolve(saved));
    expect(host.querySelector('fieldset')?.disabled).toBe(false);
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it('keeps unsaved edits during refresh and discards to the newest data', async () => {
    await interact(() => change('mail', '80'));
    const newer = structuredClone(settings);
    newer.revision = 2;
    newer.config.business.mail.requests_per_second = 30;
    await interact(() => client.setQueryData(queryKeys.admin.rateLimitSettings, newer));
    expect(input('mail').value).toBe('80');
    await interact(() => action(1).click());
    expect(input('mail').value).toBe('30');
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it('adopts the newest snapshot when edits return to the old baseline', async () => {
    await interact(() => change('mail', '80'));
    const newer = structuredClone(settings);
    newer.revision = 2;
    newer.updated_by = 'newer-admin@example.test';
    newer.config.business.mail.requests_per_second = 30;
    await interact(() => client.setQueryData(queryKeys.admin.rateLimitSettings, newer));
    await interact(() => change('mail', '2'));
    expect(input('mail').value).toBe('30');
    expect(action(1).disabled).toBe(true);
    expect(action(2).disabled).toBe(true);
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    expect(host.querySelector('.api-rate-limit-meta')?.textContent).toContain(newer.updated_by);
    await interact(() => change('mail', '2'));
    await interact(() => action(2).click());
    expect(saveMock.mock.calls[0][0].revision).toBe(2);
    expect(saveMock.mock.calls[0][0].config.business.mail.requests_per_second).toBe(2);
  });

  it('clears a resolved conflict when a clean draft adopts a newer snapshot', async () => {
    saveMock.mockRejectedValueOnce(new ApiError('conflict', 409));
    await interact(() => change('mail', '80'));
    await interact(() => action(2).click());
    await interact(() => change('mail', '2'));
    const newer = structuredClone(settings);
    newer.revision = 2;
    newer.config.business.mail.requests_per_second = 40;
    await interact(() => client.setQueryData(queryKeys.admin.rateLimitSettings, newer));
    expect(input('mail').value).toBe('40');
    expect(host.querySelector('[role="alert"]')).toBeNull();
    await interact(() => change('mail', '60'));
    expect(action(2).disabled).toBe(false);
    await interact(() => action(2).click());
    expect(saveMock.mock.calls[1][0].revision).toBe(2);
  });

  it('fills defaults as a draft and only writes them after save', async () => {
    settings.config.business.mail.requests_per_second = 100;
    await interact(() =>
      client.setQueryData(queryKeys.admin.rateLimitSettings, structuredClone(settings))
    );
    await interact(() => action(0).click());
    expect(input('mail').value).toBe('2');
    expect(saveMock).not.toHaveBeenCalled();
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    await interact(() => action(2).click());
    expect(saveMock.mock.calls[0][0].config).toEqual(settings.defaults);
  });

  it('retains conflicted edits until an explicit successful reload', async () => {
    saveMock.mockRejectedValueOnce(new ApiError('conflict', 409));
    await interact(() => change('mail', '80'));
    await interact(() => action(2).click());
    expect(input('mail').value).toBe('80');
    expect(action(2).disabled).toBe(true);
    const reload = host.querySelector('[role="alert"] button');
    if (!(reload instanceof HTMLButtonElement)) throw new Error('Missing reload action');
    const pending = deferred<APIRateLimitSettings>();
    loadMock.mockReturnValueOnce(pending.promise);
    await interact(() => reload.click());
    expect(host.querySelector('fieldset')?.disabled).toBe(true);
    const newer = structuredClone(settings);
    newer.revision = 2;
    newer.config.business.mail.requests_per_second = 40;
    await interact(() => pending.resolve(newer));
    expect(input('mail').value).toBe('40');
    expect(host.querySelector('fieldset')?.disabled).toBe(false);
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it('keeps conflicting edits during background refresh and a failed reload', async () => {
    saveMock.mockRejectedValueOnce(new ApiError('conflict', 409));
    await interact(() => change('mail', '80'));
    await interact(() => action(2).click());
    const newer = structuredClone(settings);
    newer.revision = 2;
    newer.config.business.mail.requests_per_second = 40;
    await interact(() => client.setQueryData(queryKeys.admin.rateLimitSettings, newer));
    expect(input('mail').value).toBe('80');
    expect(action(2).disabled).toBe(true);
    expect(host.querySelector('.api-rate-limit-meta')?.textContent).toContain(settings.updated_by);
    const reload = host.querySelector<HTMLButtonElement>('[role="alert"] button');
    if (!reload) throw new Error('Missing reload action');
    loadMock.mockRejectedValueOnce(new Error('offline'));
    await interact(() => reload.click());
    expect(input('mail').value).toBe('80');
    expect(action(2).disabled).toBe(true);
    expect(host.textContent).toContain('Another administrator updated');
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
  });

  it('keeps a newer snapshot when conflict reload completes with an older response', async () => {
    saveMock.mockRejectedValueOnce(new ApiError('conflict', 409));
    await interact(() => change('mail', '80'));
    await interact(() => action(2).click());
    const pending = deferred<APIRateLimitSettings>();
    loadMock.mockReturnValueOnce(pending.promise);
    const reload = host.querySelector<HTMLButtonElement>('[role="alert"] button');
    if (!reload) throw new Error('Missing reload action');
    await interact(() => reload.click());
    const newer = structuredClone(settings);
    newer.revision = 3;
    newer.config.business.mail.requests_per_second = 60;
    await interact(() => client.setQueryData(queryKeys.admin.rateLimitSettings, newer));
    const older = structuredClone(newer);
    older.revision = 2;
    older.config.business.mail.requests_per_second = 40;
    await interact(() => pending.resolve(older));
    expect(
      client.getQueryData<APIRateLimitSettings>(queryKeys.admin.rateLimitSettings)?.revision
    ).toBe(3);
    await interact(() =>
      root.render(
        <QueryClientProvider client={client}>
          <APIRateLimitSettingsPanel key="reopened" onDirtyChange={onDirtyChange} />
        </QueryClientProvider>
      )
    );
    expect(input('mail').value).toBe('60');
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
    await interact(() => change('mail', '100'));
    await interact(() => action(2).click());
    expect(saveMock.mock.calls[1][0].revision).toBe(3);
  });

  it('does not restore an old baseline when a read finishes after a save', async () => {
    const pending = deferred<APIRateLimitSettings>();
    const old = structuredClone(settings);
    loadMock.mockReturnValueOnce(pending.promise);
    await interact(() => {
      void client.invalidateQueries({ queryKey: queryKeys.admin.rateLimitSettings });
    });
    await interact(() => change('mail', '100'));
    await interact(() => action(2).click());
    await interact(() => pending.resolve(old));
    expect(input('mail').value).toBe('100');
    await interact(() => change('mail', '80'));
    await interact(() => action(1).click());
    expect(input('mail').value).toBe('100');
    expect(host.querySelector('.api-rate-limit-meta')?.textContent).toContain('admin@example.test');
  });

  it.each(['logout', 'account switch'])('ignores a save completing after %s', async (action) => {
    const pending = deferred<APIRateLimitSettings>();
    saveMock.mockReturnValueOnce(pending.promise);
    await interact(() => change('mail', '80'));
    await interact(() => host.querySelector<HTMLButtonElement>('button[type="submit"]')?.click());
    const nextSession = { ...structuredClone(settings), updated_by: 'other-admin@example.test' };
    await interact(() => {
      root.render(null);
      clearUserSession(client);
      if (action === 'account switch')
        client.setQueryData(queryKeys.admin.rateLimitSettings, nextSession);
    });
    await interact(() => pending.resolve({ ...settings, revision: 2 }));
    expect(client.getQueryData(queryKeys.admin.rateLimitSettings)).toEqual(
      action === 'logout' ? undefined : nextSession
    );
    expect(notifyMock).not.toHaveBeenCalled();
    expect(errorMock).not.toHaveBeenCalled();
  });

  it('keeps a draft on refresh failure and recovers after retry', async () => {
    await interact(() => change('mail', '80'));
    loadMock.mockRejectedValueOnce(new Error('offline'));
    await interact(() => {
      void client.invalidateQueries({ queryKey: queryKeys.admin.rateLimitSettings });
    });
    expect(input('mail').value).toBe('80');
    expect(action(2).disabled).toBe(true);
    const retry = host.querySelector('[role="alert"] button');
    if (!(retry instanceof HTMLButtonElement)) throw new Error('Missing retry action');
    await interact(() => retry.click());
    expect(input('mail').value).toBe('80');
    expect(action(2).disabled).toBe(false);
  });
});
