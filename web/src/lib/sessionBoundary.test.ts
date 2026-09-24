import { afterEach, describe, expect, it } from 'vitest';
import { clearUserSession, createAppQueryClient } from './queryClient';

type AuthState = { installed: boolean; user: { id: number } | null };
const clients: ReturnType<typeof createAppQueryClient>[] = [];

afterEach(() => {
  for (const client of clients.splice(0)) client.clear();
});

describe('session query boundary', () => {
  it.each(['logout', 'account switch'])(
    'discards a late auth response after %s',
    async (action) => {
      const client = createAppQueryClient();
      clients.push(client);
      let resolveProbe!: (value: AuthState) => void;
      let signal: AbortSignal | undefined;
      const response = new Promise<AuthState>((resolve) => {
        resolveProbe = resolve;
      });
      const pending = client
        .fetchQuery({
          queryKey: ['me'],
          queryFn: (context) => {
            signal = context.signal;
            return response;
          },
        })
        .catch(() => undefined);

      clearUserSession(client);
      const expected: AuthState = {
        installed: true,
        user: action === 'account switch' ? { id: 8 } : null,
      };
      if (action === 'account switch') client.setQueryData(['me'], expected);
      resolveProbe({ installed: true, user: { id: 7 } });
      await pending;

      expect(signal?.aborted).toBe(true);
      expect(client.getQueryData(['me'])).toEqual(expected);
    }
  );
});
