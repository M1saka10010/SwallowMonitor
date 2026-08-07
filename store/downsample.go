package store

import (
	"database/sql"
	"time"
)

// Downsampled history tiers. Raw rows are kept for rawRetentionSeconds, then
// rolled up into 5-minute buckets (kept per the configured retentionDays) and
// hourly buckets (kept for one year). Queries route by span (see QueryUsage).
const (
	rawRetentionSeconds         int64 = 24 * 3600
	downsampled5mSpanSeconds   int64 = 30 * 24 * 3600
	hourlyRetentionSeconds     int64 = 365 * 24 * 3600
	downsampleBucket5mSeconds  int64 = 300
	downsampleBucket1hSeconds  int64 = 3600
	downsampleCompleteWindow   int64 = downsampleBucket5mSeconds // only aggregate full buckets
	settingUsage5mAggTs              = "usage5mAggTs"
	settingUsage1hAggTs              = "usage1hAggTs"
)

// AggregateUsage rolls raw usage rows up into usages_5m buckets and usages_5m
// rows into usages_1h buckets. It is incremental and idempotent: the next
// bucket start is persisted in settings, and ON CONFLICT DO NOTHING skips
// buckets already written (e.g. from late-arriving rows).
func (s *Store) AggregateUsage(now time.Time) error {
	nowUnix := now.Unix()

	cutoff5m := nowUnix - downsampleCompleteWindow
	agg5m := s.aggPoint(settingUsage5mAggTs)
	if agg5m < cutoff5m {
		if err := s.aggregateInto("usages", "ts", "usages_5m", downsampleBucket5mSeconds, agg5m, cutoff5m); err != nil {
			return err
		}
		if err := s.setAggPoint(settingUsage5mAggTs, cutoff5m/downsampleBucket5mSeconds*downsampleBucket5mSeconds); err != nil {
			return err
		}
	}

	// An hourly bucket is only complete once its trailing 5-minute bucket is.
	cutoff1h := nowUnix - downsampleBucket1hSeconds
	agg1h := s.aggPoint(settingUsage1hAggTs)
	if agg1h < cutoff1h {
		if err := s.aggregateInto("usages_5m", "bucket_ts", "usages_1h", downsampleBucket1hSeconds, agg1h, cutoff1h); err != nil {
			return err
		}
		if err := s.setAggPoint(settingUsage1hAggTs, cutoff1h/downsampleBucket1hSeconds*downsampleBucket1hSeconds); err != nil {
			return err
		}
	}
	return nil
}

// aggregateInto inserts buckets from src into dst for rows with ts in
// [from, to). Rates and ratios are averaged, cumulative counters and totals
// take their max per bucket.
func (s *Store) aggregateInto(src, srcTsCol, dst string, bucketSeconds, from, to int64) error {
	_, err := s.db.Exec(`INSERT INTO `+dst+` (public_id, bucket_ts, `+usageMetricsCols+`)
		SELECT public_id, (`+srcTsCol+`/?) * ?, AVG(cpu_usage), MAX(memory_total), MAX(memory_used),
			MAX(swap_total), MAX(swap_used), MAX(disk_total), MAX(disk_used),
			MAX(net_recv), MAX(net_send), AVG(net_recv_speed), AVG(net_send_speed),
			AVG(load1), AVG(load5), AVG(load15)
		FROM `+src+`
		WHERE `+srcTsCol+` >= ? AND `+srcTsCol+` < ?
		GROUP BY public_id, (`+srcTsCol+`/?) * ?
		ON CONFLICT(public_id, bucket_ts) DO NOTHING`,
		bucketSeconds, bucketSeconds, from, to, bucketSeconds, bucketSeconds)
	return err
}

// PruneDownsampled deletes expired rows from the 5-minute and hourly tables.
// A non-positive retentionDays keeps the downsampled history forever.
func (s *Store) PruneDownsampled(retentionDays int) (int64, error) {
	now := time.Now().Unix()
	var total int64
	if retentionDays > 0 {
		n, err := s.pruneByTs("usages_5m", "bucket_ts", now-int64(retentionDays)*24*3600)
		if err != nil {
			return total, err
		}
		total += n
	}
	n, err := s.pruneByTs("usages_1h", "bucket_ts", now-hourlyRetentionSeconds)
	if err != nil {
		return total, err
	}
	return total + n, nil
}

// pruneByTs deletes rows older than cutoff in id-ordered batches so each
// statement's transaction stays short (bounded WAL growth, no long lock).
func (s *Store) pruneByTs(table, tsCol string, cutoff int64) (int64, error) {
	const batchSize = 5000
	var total int64
	for {
		var maxTS sql.NullInt64
		err := s.db.QueryRow(`SELECT MAX(`+tsCol+`) FROM (
				SELECT `+tsCol+` FROM `+table+` WHERE `+tsCol+` < ? ORDER BY `+tsCol+` LIMIT ?
			)`, cutoff, batchSize).Scan(&maxTS)
		if err != nil {
			return total, err
		}
		if !maxTS.Valid {
			return total, nil
		}
		res, err := s.db.Exec(`DELETE FROM `+table+` WHERE `+tsCol+` <= ?`, maxTS.Int64)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
	}
}

func (s *Store) aggPoint(key string) int64 {
	// First run: aggregate everything currently in the source table so
	// pre-existing history is not lost when the raw tier is pruned.
	var v sql.NullInt64
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil || !v.Valid {
		return 0
	}
	return v.Int64
}

func (s *Store) setAggPoint(key string, value int64) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
