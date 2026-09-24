import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, type RefObject } from 'react';
import { toast } from 'sonner';
import { api } from '../../api';
import {
  formatDeliveryError,
  isEmailDeliveryFailed,
  isEmailDeliveryInProgress,
  isEmailDeliverySucceeded,
} from '../../lib/emailDelivery';
import { notifySuccess } from '../../lib/feedback';
import { queryKeys } from '../../lib/queryKeys';
import { useText } from '../../locales';
import type { EmailDelivery, RegisterResponse } from '../../types';

export function useVerificationDelivery(
  verification: RegisterResponse | null,
  email: string,
  origin: RefObject<HTMLButtonElement | null>
) {
  const text = useText();
  const deliveryId = verification?.delivery_id ?? null;
  const delivery = useQuery({
    queryKey: queryKeys.loginSettingsEmailDelivery(deliveryId),
    queryFn: ({ signal }) =>
      api<EmailDelivery>(`/api/email-deliveries/${encodeURIComponent(String(deliveryId))}`, {
        signal,
      }),
    enabled: deliveryId !== null,
    retry: false,
    refetchInterval: (query) =>
      isEmailDeliveryInProgress(query.state.data?.status) ? 2000 : false,
  });
  const status = delivery.isError
    ? undefined
    : delivery.data?.status || verification?.email_delivery_status;
  const error =
    delivery.data?.error ||
    verification?.email_delivery_error ||
    text.login.verificationDeliveryFailed;
  const inProgress = isEmailDeliveryInProgress(status);
  const failed = isEmailDeliveryFailed(status);
  const sent =
    isEmailDeliverySucceeded(status) ||
    (verification !== null &&
      deliveryId === null &&
      verification.email_delivery_status === undefined);
  const notice = inProgress ? 'queued' : failed ? 'failed' : sent ? 'sent' : '';
  const lastNotice = useRef<{ verification: RegisterResponse | null; kind: string }>({
    verification: null,
    kind: '',
  });

  useEffect(() => {
    if (!verification || !notice) return;
    if (lastNotice.current.verification === verification && lastNotice.current.kind === notice)
      return;
    lastNotice.current = { verification, kind: notice };
    if (notice === 'failed') {
      toast.error(error);
    } else {
      notifySuccess(
        notice === 'queued' ? text.login.verificationQueued : text.login.verificationSent,
        { origin: origin.current }
      );
    }
  }, [
    verification,
    notice,
    error,
    origin,
    text.login.verificationQueued,
    text.login.verificationSent,
  ]);

  const hint = inProgress
    ? text.login.verificationDeliverySendingHint
    : failed
      ? formatDeliveryError(text.login.verificationDeliveryFailedWithError, error)
      : deliveryId !== null && delivery.isError
        ? text.login.verificationDeliveryStatusLoadError
        : verification
          ? text.login.verificationDesc.replace('{email}', email.trim())
          : '';
  return { inProgress, hint };
}
