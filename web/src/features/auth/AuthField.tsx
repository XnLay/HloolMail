import type { ComponentProps, Ref } from 'react';
import { useText } from '../../locales';
import { authFieldIds, type AuthFieldName } from './authForm';

type AuthFieldProps = Omit<ComponentProps<'input'>, 'name' | 'id' | 'value' | 'onChange'> & {
  name: AuthFieldName;
  label: string;
  value: string;
  error?: string;
  onValueChange: (value: string) => void;
};

export function AuthField({ name, label, error, onValueChange, ...input }: AuthFieldProps) {
  const id = authFieldIds[name];
  return (
    <>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <input
        {...input}
        id={id}
        name={name}
        className="input"
        onChange={(event) => onValueChange(event.target.value)}
        aria-invalid={Boolean(error)}
        aria-describedby={
          [input['aria-describedby'], error ? `${id}-error` : ''].filter(Boolean).join(' ') ||
          undefined
        }
      />
      {error && (
        <span id={`${id}-error`} className="field-error" role="alert">
          {error}
        </span>
      )}
    </>
  );
}

export function TurnstileField({
  containerRef,
  loadError,
}: {
  containerRef: Ref<HTMLDivElement>;
  loadError: boolean;
}) {
  const text = useText();
  return (
    <>
      <div ref={containerRef} className="auth-turnstile-widget" />
      {loadError && (
        <p className="auth-turnstile-error" role="status">
          {text.login.turnstileLoadError}
        </p>
      )}
    </>
  );
}
