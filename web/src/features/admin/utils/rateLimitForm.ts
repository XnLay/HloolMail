import type {
  APIRateLimitConfig,
  APIRateLimitConstraints,
  APIRateLimitRule,
} from '../../../types/rateLimits';

export const preAuthRules = ['per_ip', 'per_instance'] as const;
export const businessRules = [
  'mail',
  'generate_email',
  'available_domains',
  'stats',
  'yyds',
] as const;
export const rateLimitRules = [...preAuthRules, ...businessRules] as const;
export type RateLimitRuleID = (typeof rateLimitRules)[number];
export type RuleDraft = { enabled: boolean; requests_per_second: string; burst: string };
export type RateLimitDraft = Record<RateLimitRuleID, RuleDraft>;
export type RuleErrors = { rate: boolean; burst: boolean };
export type RateLimitFormResult =
  | { ok: true; config: APIRateLimitConfig }
  | { ok: false; errors: Partial<Record<RateLimitRuleID, RuleErrors>> };

export function rateLimitDraft(config: APIRateLimitConfig): RateLimitDraft {
  const draft = (rule: APIRateLimitRule): RuleDraft => ({
    enabled: rule.enabled,
    requests_per_second: String(rule.requests_per_second),
    burst: String(rule.burst),
  });
  return {
    per_ip: draft(config.pre_auth.per_ip),
    per_instance: draft(config.pre_auth.per_instance),
    mail: draft(config.business.mail),
    generate_email: draft(config.business.generate_email),
    available_domains: draft(config.business.available_domains),
    stats: draft(config.business.stats),
    yyds: draft(config.business.yyds),
  };
}

export function parseRateLimitDraft(
  form: RateLimitDraft,
  bounds: APIRateLimitConstraints
): RateLimitFormResult {
  // Number 会接受十六进制等非表单数值，先限定十进制语法再检查范围。
  const decimalNumber = /^(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i;
  const errors: Partial<Record<RateLimitRuleID, RuleErrors>> = {};
  for (const id of rateLimitRules) {
    const rps = Number(form[id].requests_per_second);
    const burst = Number(form[id].burst);
    const rateInvalid =
      !decimalNumber.test(form[id].requests_per_second.trim()) ||
      !Number.isFinite(rps) ||
      rps < bounds.min_requests_per_second ||
      rps > bounds.max_requests_per_second;
    const burstInvalid =
      !decimalNumber.test(form[id].burst.trim()) ||
      !Number.isInteger(burst) ||
      burst < bounds.min_burst ||
      burst > bounds.max_burst;
    if (rateInvalid || burstInvalid) errors[id] = { rate: rateInvalid, burst: burstInvalid };
  }
  if (Object.keys(errors).length) return { ok: false, errors };
  const rule = (id: RateLimitRuleID): APIRateLimitRule => ({
    enabled: form[id].enabled,
    requests_per_second: Number(form[id].requests_per_second),
    burst: Number(form[id].burst),
  });
  return {
    ok: true,
    config: {
      pre_auth: { per_ip: rule('per_ip'), per_instance: rule('per_instance') },
      business: {
        mail: rule('mail'),
        generate_email: rule('generate_email'),
        available_domains: rule('available_domains'),
        stats: rule('stats'),
        yyds: rule('yyds'),
      },
    },
  };
}

export function effectiveRateLimit(
  config: APIRateLimitConfig,
  profile: keyof APIRateLimitConfig['business']
): number | null {
  const rates = [config.pre_auth.per_ip, config.pre_auth.per_instance, config.business[profile]]
    .filter((rule) => rule.enabled)
    .map((rule) => rule.requests_per_second);
  return rates.length ? Math.min(...rates) : null;
}
