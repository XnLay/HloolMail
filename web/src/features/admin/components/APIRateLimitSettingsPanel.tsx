import { useQueryClient, type Query } from '@tanstack/react-query';
import { Loader2, Save } from 'lucide-react';
import { useEffect, useReducer, useRef, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api';
import { notifySuccess } from '../../../lib/feedback';
import { queryKeys } from '../../../lib/queryKeys';
import { useText } from '../../../locales';
import type { APIRateLimitSettings } from '../../../types/rateLimits';
import {
  useAPIRateLimitSettingsQuery,
  useSaveAPIRateLimitSettingsMutation,
} from '../hooks/useAdminQueries';
import { hasRateLimitEdits, rateLimitEditorReducer } from '../utils/rateLimitEditor';
import {
  businessRules,
  effectiveRateLimit,
  parseRateLimitDraft,
  preAuthRules,
  rateLimitDraft,
  type RateLimitRuleID,
  type RuleDraft,
} from '../utils/rateLimitForm';
import { APIRateLimitRuleFields } from './APIRateLimitRuleFields';
import '../../../styles/api-rate-limits.css';

export function APIRateLimitSettingsPanel({
  onDirtyChange,
}: {
  onDirtyChange: (dirty: boolean) => void;
}) {
  const allText = useText();
  const text = allText.admin.rateLimits;
  const queryClient = useQueryClient();
  const settings = useAPIRateLimitSettingsQuery();
  const [editor, dispatch] = useReducer(rateLimitEditorReducer, null);
  const data = editor?.snapshot;
  const form = data ? (editor?.draft?.form ?? rateLimitDraft(data.config)) : null;
  const displayed = editor?.draft?.base ?? data;
  const conflict = editor?.draft?.conflict ?? false;
  const [reloading, setReloading] = useState(false);
  const saveRef = useRef<HTMLButtonElement>(null);
  const saveQuery = useRef<Query | undefined>(undefined);
  const isSaveQueryCurrent = () =>
    saveQuery.current !== undefined &&
    queryClient.getQueryCache().find({ queryKey: queryKeys.admin.rateLimitSettings }) ===
      saveQuery.current;
  const dirty = hasRateLimitEdits(editor);

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);
  useEffect(() => {
    if (settings.data) dispatch({ type: 'receive', settings: settings.data });
  }, [settings.data]);

  const save = useSaveAPIRateLimitSettingsMutation({
    onMutate: () => {
      saveQuery.current = queryClient
        .getQueryCache()
        .find({ queryKey: queryKeys.admin.rateLimitSettings });
    },
    onSuccess: async (saved) => {
      // 退出或切换账号会移除查询，旧会话的保存响应不得重建缓存。
      if (!isSaveQueryCurrent()) return;
      // 取消写入前发出的查询，避免迟到响应覆盖新缓存及撤销基线。
      await queryClient.cancelQueries({ queryKey: queryKeys.admin.rateLimitSettings });
      if (!isSaveQueryCurrent()) return;
      const cached = queryClient.getQueryData<APIRateLimitSettings>(
        queryKeys.admin.rateLimitSettings
      );
      const data = cached && cached.revision > saved.revision ? cached : saved;
      dispatch({ type: 'accept', settings: data });
      queryClient.setQueryData(queryKeys.admin.rateLimitSettings, data);
      void queryClient.invalidateQueries({ queryKey: queryKeys.admin.auditLogsRoot });
      notifySuccess(text.saved, { origin: saveRef.current });
    },
    onError: (error) => {
      if (!isSaveQueryCurrent()) return;
      if (error instanceof ApiError && error.status === 409) dispatch({ type: 'conflict' });
      else toast.error(error.message);
    },
  });

  const busy = save.isPending || reloading;
  const reload = async () => {
    if (busy) return;
    setReloading(true);
    try {
      const result = await settings.refetch();
      if (result.isSuccess) {
        dispatch({ type: 'accept', settings: result.data });
      }
    } finally {
      setReloading(false);
    }
  };
  const changeRule = (id: RateLimitRuleID, value: RuleDraft) => {
    dispatch({ type: 'edit', rule: id, value });
  };
  const parsed = form && data ? parseRateLimitDraft(form, data.constraints) : null;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!editor?.draft || !parsed?.ok || !dirty || conflict || busy || settings.isError) return;
    save.mutate({ revision: editor.draft.base.revision, config: parsed.config });
  };

  return (
    <section
      className="panel api-rate-limit-settings"
      aria-labelledby="api-rate-limit-title"
      aria-busy={settings.isFetching}
    >
      <div className="panel-header admin-panel-header">
        <div>
          <h2 id="api-rate-limit-title">{text.title}</h2>
          <p>{text.description}</p>
        </div>
      </div>
      {settings.isError && (
        <div className="admin-risk admin-risk-warning" role="alert">
          <span>{text.loadError}</span>
          <button
            type="button"
            className="btn-secondary"
            onClick={() => settings.refetch()}
            disabled={settings.isFetching}
          >
            {allText.common.retry}
          </button>
        </div>
      )}
      {!data || !form || !displayed ? (
        !settings.isError && <p role="status">{allText.common.loading}</p>
      ) : (
        <form onSubmit={submit} noValidate>
          <div className="api-rate-limit-group">
            <h3>{text.preAuthTitle}</h3>
            <p>{text.preAuthDescription}</p>
            {preAuthRules.map((id) => (
              <APIRateLimitRuleFields
                key={id}
                id={id}
                value={form[id]}
                bounds={data.constraints}
                errors={parsed && !parsed.ok ? parsed.errors[id] : undefined}
                disabled={busy}
                onChange={(value) => changeRule(id, value)}
              />
            ))}
          </div>
          <div className="api-rate-limit-group">
            <h3>{text.businessTitle}</h3>
            <p>{text.businessDescription}</p>
            {businessRules.map((id) => {
              const effective = parsed?.ok ? effectiveRateLimit(parsed.config, id) : null;
              const constrained =
                parsed?.ok &&
                effective !== null &&
                (!parsed.config.business[id].enabled ||
                  effective < parsed.config.business[id].requests_per_second);
              return (
                <APIRateLimitRuleFields
                  key={id}
                  id={id}
                  value={form[id]}
                  bounds={data.constraints}
                  errors={parsed && !parsed.ok ? parsed.errors[id] : undefined}
                  disabled={busy}
                  onChange={(value) => changeRule(id, value)}
                >
                  {parsed?.ok && (
                    <p
                      className={constrained ? 'api-rate-limit-bottleneck' : 'api-rate-limit-hint'}
                    >
                      {effective === null
                        ? text.unlimited
                        : text.effective.replace('{rate}', String(effective))}
                      {constrained && ` ${text.bottleneck}`}
                    </p>
                  )}
                </APIRateLimitRuleFields>
              );
            })}
          </div>
          <p className="api-rate-limit-hint">{text.burstHelp}</p>
          <p className="api-rate-limit-hint">
            {text.scopeHelp}{' '}
            <a href="/api/docs.md" target="_blank" rel="noreferrer">
              {text.helpLink}
            </a>
          </p>
          {conflict && (
            <div className="admin-risk admin-risk-warning" role="alert">
              <span>{text.conflict}</span>
              <button
                type="button"
                className="btn-secondary"
                onClick={reload}
                disabled={settings.isFetching}
              >
                {text.reload}
              </button>
            </div>
          )}
          <div className="api-rate-limit-actions">
            <button
              type="button"
              className="btn-secondary"
              disabled={busy || settings.isError}
              onClick={() => dispatch({ type: 'defaults' })}
            >
              {text.restoreDefaults}
            </button>
            <button
              type="button"
              className="btn-secondary"
              disabled={!dirty || busy || settings.isError}
              onClick={() => {
                if (conflict) {
                  void reload();
                  return;
                }
                dispatch({ type: 'discard' });
              }}
            >
              {text.discard}
            </button>
            <button
              ref={saveRef}
              type="submit"
              className="btn-primary"
              disabled={!dirty || !parsed?.ok || conflict || busy || settings.isError}
              aria-busy={save.isPending}
            >
              {save.isPending ? (
                <Loader2 size={16} className="animate-spin" aria-hidden="true" />
              ) : (
                <Save size={16} aria-hidden="true" />
              )}
              {save.isPending ? text.saving : text.save}
            </button>
          </div>
          <p className="api-rate-limit-meta" role="status">
            {text.updatedBy.replace('{actor}', displayed.updated_by)} ·{' '}
            <time dateTime={displayed.updated_at}>
              {new Date(displayed.updated_at).toLocaleString()}
            </time>
          </p>
          <p className="api-rate-limit-hint">{text.applyHelp}</p>
        </form>
      )}
    </section>
  );
}
