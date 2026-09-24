-- 只补齐有原始关联且资源在收件之前已稳定存在的记录；地址相同不能证明归属。
UPDATE messages
SET owner_id = (SELECT owner_id FROM mailboxes WHERE mailboxes.id = messages.mailbox_id)
WHERE owner_id IS NULL AND deleted_at IS NULL
  AND mailbox_id IS NOT NULL AND domain_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM mailboxes
    JOIN domains ON domains.id = mailboxes.domain_id
    JOIN users ON users.id = mailboxes.owner_id
    WHERE mailboxes.id = messages.mailbox_id
      AND mailboxes.domain_id = messages.domain_id
      AND mailboxes.email = messages.recipient
      AND mailboxes.created_at <= mailboxes.updated_at
      AND mailboxes.updated_at < messages.created_at
  );

-- 私有域名 catch-all 也必须持有原始 domain_id，且不能覆盖不一致的邮箱关联。
UPDATE messages
SET owner_id = (SELECT owner_id FROM domains WHERE domains.id = messages.domain_id)
WHERE owner_id IS NULL AND deleted_at IS NULL
  AND mailbox_id IS NULL AND domain_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM domains
    JOIN users ON users.id = domains.owner_id
    WHERE domains.id = messages.domain_id
      AND domains.domain = messages.root_domain
      AND domains.mode = 'private'
      AND domains.created_at <= domains.updated_at
      AND domains.updated_at < messages.created_at
  );
