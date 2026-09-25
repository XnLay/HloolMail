import type { ReactNode } from 'react';
import { useText } from '../../../locales';
import type { APIRateLimitConstraints } from '../../../types/rateLimits';
import type { RateLimitRuleID, RuleDraft, RuleErrors } from '../utils/rateLimitForm';

type Props = {
  id: RateLimitRuleID;
  value: RuleDraft;
  bounds: APIRateLimitConstraints;
  errors?: RuleErrors;
  disabled: boolean;
  onChange: (value: RuleDraft) => void;
  children?: ReactNode;
};

export function APIRateLimitRuleFields({
  id,
  value,
  bounds,
  errors,
  disabled,
  onChange,
  children,
}: Props) {
  const text = useText().admin.rateLimits;
  const fieldId = `rate-limit-${id}`;
  return (
    <fieldset className="api-rate-limit-rule" disabled={disabled}>
      <legend>{text.rules[id]}</legend>
      <div className="api-rate-limit-fields">
        <label className="api-rate-limit-enabled">
          <input
            type="checkbox"
            checked={value.enabled}
            aria-label={`${text.rules[id]} ${text.enabled}`}
            onChange={(event) => onChange({ ...value, enabled: event.target.checked })}
          />
          <span>{value.enabled ? text.enabled : text.disabled}</span>
        </label>
        <div>
          <label htmlFor={`${fieldId}-rps`}>{text.rate}</label>
          <input
            id={`${fieldId}-rps`}
            className="input"
            type="number"
            step="any"
            min={bounds.min_requests_per_second}
            max={bounds.max_requests_per_second}
            value={value.requests_per_second}
            aria-invalid={Boolean(errors?.rate)}
            aria-describedby={errors?.rate ? `${fieldId}-rate-error` : undefined}
            onChange={(event) => onChange({ ...value, requests_per_second: event.target.value })}
          />
          {errors?.rate && (
            <span className="field-error" id={`${fieldId}-rate-error`} role="alert">
              {text.rateError
                .replace('{min}', String(bounds.min_requests_per_second))
                .replace('{max}', String(bounds.max_requests_per_second))}
            </span>
          )}
        </div>
        <div>
          <label htmlFor={`${fieldId}-burst`}>{text.burst}</label>
          <input
            id={`${fieldId}-burst`}
            className="input"
            type="number"
            step="1"
            min={bounds.min_burst}
            max={bounds.max_burst}
            value={value.burst}
            aria-invalid={Boolean(errors?.burst)}
            aria-describedby={errors?.burst ? `${fieldId}-burst-error` : undefined}
            onChange={(event) => onChange({ ...value, burst: event.target.value })}
          />
          {errors?.burst && (
            <span className="field-error" id={`${fieldId}-burst-error`} role="alert">
              {text.burstError.replace('{max}', String(bounds.max_burst))}
            </span>
          )}
        </div>
      </div>
      {children}
    </fieldset>
  );
}
