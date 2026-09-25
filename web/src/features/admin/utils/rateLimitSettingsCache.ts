import { replaceEqualDeep } from '@tanstack/react-query';

function hasRevision(value: unknown): value is { revision: number } {
  return (
    typeof value === 'object' &&
    value !== null &&
    'revision' in value &&
    typeof value.revision === 'number'
  );
}

// 在查询缓存写入时拒绝旧版本，重新挂载表单也不会读回迟到的旧快照。
export function shareRateLimitSettings(previous: unknown, next: unknown): unknown {
  if (hasRevision(previous) && hasRevision(next) && previous.revision > next.revision) {
    return previous;
  }
  return replaceEqualDeep(previous, next);
}
