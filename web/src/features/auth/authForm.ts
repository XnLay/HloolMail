export type AuthMode = 'login' | 'register';

// 草稿随认证面板保留；验证结果、错误和人机验证实例由当前表单独立管理。
export type AuthDraft = {
  nickname: string;
  email: string;
  password: string;
  confirmPassword: string;
  captchaAnswer: string;
};

export type AuthFormProps = {
  draft: AuthDraft;
  onDraftChange: (field: keyof AuthDraft, value: string) => void;
  onDone: () => void;
};

export const authFieldIds = {
  nickname: 'auth-nickname',
  email: 'auth-email',
  password: 'auth-password',
  confirmPassword: 'auth-confirm-password',
  captchaAnswer: 'auth-captcha-answer',
  verificationCode: 'auth-verification-code',
} as const;

export type AuthFieldName = keyof typeof authFieldIds;
export type AuthFieldErrors = Partial<Record<AuthFieldName, string>>;

export function focusFirstInvalidField(errors: AuthFieldErrors) {
  for (const field of Object.keys(authFieldIds) as AuthFieldName[]) {
    if (errors[field]) {
      window.requestAnimationFrame(() => document.getElementById(authFieldIds[field])?.focus());
      return;
    }
  }
}
