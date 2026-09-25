import type { APIRateLimitRule, APIRateLimitSettings } from '../../../types/rateLimits';

export function rateLimitTestSettings(): APIRateLimitSettings {
  const rule = (requests_per_second: number, burst: number): APIRateLimitRule => ({
    enabled: true,
    requests_per_second,
    burst,
  });
  const config = {
    pre_auth: { per_ip: rule(5, 20), per_instance: rule(50, 200) },
    business: {
      mail: rule(2, 20),
      generate_email: rule(1, 10),
      available_domains: rule(0.5, 5),
      stats: rule(2, 10),
      yyds: rule(2, 20),
    },
  };
  return {
    revision: 1,
    applied_revision: 1,
    config,
    defaults: structuredClone(config),
    constraints: {
      min_requests_per_second: 0.001,
      max_requests_per_second: 100000,
      min_burst: 1,
      max_burst: 100000,
    },
    updated_at: '2026-09-25T00:00:00Z',
    updated_by: 'system',
  };
}
