package auth

import (
	"context"
	"time"
)

// ldapSyncLockName is the fleet-wide lease that coordinates the periodic
// LDAP sync across instances sharing one auth database.
const ldapSyncLockName = "ldap_sync"

// AcquireSyncLock atomically claims the named fleet-wide lock when no claim
// exists yet or the current claim is older than notBefore. The winner writes
// its instance id and the claim time; everyone else gets false without
// waiting. Only PostgreSQL's clock is involved, so instance clock skew does
// not matter.
func (s *Store) AcquireSyncLock(ctx context.Context, name, instanceID string, notBefore time.Duration) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO auth_sync_locks (name, instance_id, acquired_at)
		VALUES ($1, $2, now())
		ON CONFLICT (name) DO UPDATE SET
			instance_id = EXCLUDED.instance_id,
			acquired_at = now()
		WHERE auth_sync_locks.acquired_at <= now() - $3 * interval '1 second'`,
		name, instanceID, int64(notBefore.Seconds()))
	if err != nil {
		return false, err
	}

	count, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return count == 1, nil
}

// TouchSyncLock unconditionally stamps the lock with this instance and the
// current time. A completed sync calls it so the next periodic run waits a
// full interval from the moment the work finished, manual syncs included.
func (s *Store) TouchSyncLock(ctx context.Context, name, instanceID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_sync_locks (name, instance_id, acquired_at)
		VALUES ($1, $2, now())
		ON CONFLICT (name) DO UPDATE SET
			instance_id = EXCLUDED.instance_id,
			acquired_at = now()`,
		name, instanceID)

	return err
}
