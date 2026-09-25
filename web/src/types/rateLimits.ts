export type APIRateLimitRule = {
  enabled: boolean;
  requests_per_second: number;
  burst: number;
};

export type APIRateLimitConfig = {
  pre_auth: {
    per_ip: APIRateLimitRule;
    per_instance: APIRateLimitRule;
  };
  business: {
    mail: APIRateLimitRule;
    generate_email: APIRateLimitRule;
    available_domains: APIRateLimitRule;
    stats: APIRateLimitRule;
    yyds: APIRateLimitRule;
  };
};

export type APIRateLimitConstraints = {
  min_requests_per_second: number;
  max_requests_per_second: number;
  min_burst: number;
  max_burst: number;
};

export type APIRateLimitSettings = {
  revision: number;
  config: APIRateLimitConfig;
  defaults: APIRateLimitConfig;
  constraints: APIRateLimitConstraints;
  updated_at: string;
  updated_by: string;
  applied_revision: number;
};

export type UpdateAPIRateLimitSettings = Pick<APIRateLimitSettings, 'revision' | 'config'>;
