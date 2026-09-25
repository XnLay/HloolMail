import type { APIRateLimitSettings } from '../../../types/rateLimits';
import { formFingerprint, isDirtyFromBaseline } from './adminFormatting';
import {
  rateLimitDraft,
  type RateLimitDraft,
  type RateLimitRuleID,
  type RuleDraft,
} from './rateLimitForm';

type Draft = {
  base: APIRateLimitSettings;
  form: RateLimitDraft;
  conflict: boolean;
};

export type RateLimitEditor = {
  snapshot: APIRateLimitSettings;
  draft: Draft | null;
};

type EditorEvent =
  | { type: 'receive' | 'accept'; settings: APIRateLimitSettings }
  | { type: 'edit'; rule: RateLimitRuleID; value: RuleDraft }
  | { type: 'defaults' | 'discard' | 'conflict' };

export function hasRateLimitEdits(editor: RateLimitEditor | null): boolean {
  return editor?.draft
    ? isDirtyFromBaseline(
        editor.draft.form,
        formFingerprint(rateLimitDraft(editor.draft.base.config))
      )
    : false;
}

function replaceDraft(editor: RateLimitEditor, form: RateLimitDraft): RateLimitEditor {
  const draft: Draft = editor.draft
    ? { ...editor.draft, form }
    : { base: editor.snapshot, form, conflict: false };
  const next = { ...editor, draft };
  // 撤回全部修改后直接展示最新快照；409 尚无新快照时仍保留重载入口。
  if (
    !hasRateLimitEdits(next) &&
    (!draft.conflict || editor.snapshot.revision > draft.base.revision)
  ) {
    return { ...editor, draft: null };
  }
  return next;
}

export function rateLimitEditorReducer(
  editor: RateLimitEditor | null,
  event: EditorEvent
): RateLimitEditor | null {
  if (event.type === 'receive' || event.type === 'accept') {
    const snapshot =
      editor && editor.snapshot.revision > event.settings.revision
        ? editor.snapshot
        : event.settings;
    if (!editor || event.type === 'accept') return { snapshot, draft: null };
    const next = { ...editor, snapshot };
    return editor.draft ? replaceDraft(next, editor.draft.form) : next;
  }
  if (!editor) return null;
  switch (event.type) {
    case 'edit':
      return replaceDraft(editor, {
        ...(editor.draft?.form ?? rateLimitDraft(editor.snapshot.config)),
        [event.rule]: event.value,
      });
    case 'defaults':
      return replaceDraft(editor, rateLimitDraft(editor.snapshot.defaults));
    case 'discard':
      return { ...editor, draft: null };
    case 'conflict':
      return editor.draft ? { ...editor, draft: { ...editor.draft, conflict: true } } : editor;
  }
}
