import { describe, expect, it } from 'vitest';
import { rateLimitTestSettings } from '../features/admin/utils/rateLimitTestData';
import { clearUserSession, createAppQueryClient } from './queryClient';
import { queryKeys } from './queryKeys';

describe('API rate limit settings session boundary', () => {
  it('removes cached settings and ignores a response arriving after logout', async () => {
    const client = createAppQueryClient();
    const settings = rateLimitTestSettings();
    client.setQueryData(queryKeys.admin.rateLimitSettings, settings);
    let finish!: (value: typeof settings) => void;
    const pending = client
      .fetchQuery({
        queryKey: queryKeys.admin.rateLimitSettings,
        queryFn: () =>
          new Promise<typeof settings>((resolve) => {
            finish = resolve;
          }),
        staleTime: 0,
      })
      .catch(() => undefined);
    clearUserSession(client);
    finish(settings);
    await pending;
    expect(client.getQueryData(queryKeys.admin.rateLimitSettings)).toBeUndefined();
    client.clear();
  });
});
