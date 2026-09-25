import { describe, expect, it } from 'vitest';
import { effectiveRateLimit, parseRateLimitDraft, rateLimitDraft } from './rateLimitForm';
import { rateLimitTestSettings } from './rateLimitTestData';

describe('API rate limit form', () => {
  it('preserves fractional rates and explicit disabled rules', () => {
    const settings = rateLimitTestSettings();
    const form = rateLimitDraft(settings.config);
    form.mail = { enabled: false, requests_per_second: '0.001', burst: '1' };
    const parsed = parseRateLimitDraft(form, settings.constraints);
    expect(parsed.ok).toBe(true);
    if (!parsed.ok) throw new Error('Expected valid configuration');
    expect(parsed.config.business.mail).toEqual({
      enabled: false,
      requests_per_second: 0.001,
      burst: 1,
    });
    expect(parsed.config.business.available_domains.requests_per_second).toBe(0.5);
    expect(settings.config.business.mail.enabled).toBe(true);
  });

  it.each([
    '',
    ' ',
    'NaN',
    'Infinity',
    '-1',
    '0',
    '0.0001',
    '100001',
    '1e309',
    '0x10',
    '0b10',
    '0o10',
    '2 requests',
  ])('rejects invalid rate %j even while disabled', (value) => {
    const settings = rateLimitTestSettings();
    const form = rateLimitDraft(settings.config);
    form.mail.enabled = false;
    form.mail.requests_per_second = value;
    expect(parseRateLimitDraft(form, settings.constraints)).toEqual({
      ok: false,
      errors: { mail: { rate: true, burst: false } },
    });
  });

  it.each(['', ' ', '0', '-1', '1.5', 'Infinity', '100001', '0x10', '0b10', '3x'])(
    'rejects invalid burst %j without truncation',
    (value) => {
      const settings = rateLimitTestSettings();
      const form = rateLimitDraft(settings.config);
      form.per_ip.burst = value;
      expect(parseRateLimitDraft(form, settings.constraints)).toEqual({
        ok: false,
        errors: { per_ip: { rate: false, burst: true } },
      });
    }
  );

  it('uses server bounds and reports all invalid fields together', () => {
    const settings = rateLimitTestSettings();
    const form = rateLimitDraft(settings.config);
    form.mail.requests_per_second = '201';
    form.mail.burst = '501';
    form.yyds.requests_per_second = '0';
    expect(
      parseRateLimitDraft(form, {
        ...settings.constraints,
        max_requests_per_second: 200,
        max_burst: 500,
      })
    ).toEqual({
      ok: false,
      errors: { mail: { rate: true, burst: true }, yyds: { rate: true, burst: false } },
    });
  });

  it('calculates the enabled ingress bottleneck and handles all rules disabled', () => {
    const { config } = rateLimitTestSettings();
    config.business.mail.requests_per_second = 100;
    expect(effectiveRateLimit(config, 'mail')).toBe(5);
    config.pre_auth.per_ip.enabled = false;
    expect(effectiveRateLimit(config, 'mail')).toBe(50);
    config.pre_auth.per_instance.enabled = false;
    expect(effectiveRateLimit(config, 'mail')).toBe(100);
    config.business.mail.enabled = false;
    expect(effectiveRateLimit(config, 'mail')).toBeNull();
    expect(effectiveRateLimit(config, 'available_domains')).toBe(0.5);
  });
});
