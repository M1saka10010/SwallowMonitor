package store

import "github.com/M1saka10010/SwallowMonitor/model"

// InsertUsage stores a system_usage sample for a host.
func (s *Store) InsertUsage(publicID string, u *model.SystemUsage) error {
	_, err := s.db.Exec(`INSERT INTO usages (
		public_id, ts, cpu_usage, memory_total, memory_used, swap_total, swap_used,
		disk_total, disk_used, net_recv, net_send, net_recv_speed, net_send_speed,
		load1, load5, load15
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publicID, u.Timestamp, u.CPUUsage, u.MemoryTotal, u.MemoryUsed, u.SwapTotal,
		u.SwapUsed, u.DiskTotal, u.DiskUsed, u.NetRecv, u.NetSend, u.NetRecvSpeed,
		u.NetSendSpeed, u.Load1, u.Load5, u.Load15,
	)
	return err
}

// UsagePoint is a stored usage sample with its public id.
type UsagePoint struct {
	PublicID string `json:"publicId"`
	model.SystemUsage
}

// LatestUsage returns the most recent usage sample for a host, or nil if none.
func (s *Store) LatestUsage(publicID string) (*model.SystemUsage, error) {
	row := s.db.QueryRow(`SELECT `+usageCols+` FROM usages WHERE public_id = ? ORDER BY ts DESC LIMIT 1`, publicID)
	u, err := scanUsage(row)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

// QueryUsage returns usage samples for a host within [from, to] ordered by ts.
func (s *Store) QueryUsage(publicID string, from, to int64) ([]*model.SystemUsage, error) {
	rows, err := s.db.Query(`SELECT `+usageCols+` FROM usages
		WHERE public_id = ? AND ts >= ? AND ts <= ? ORDER BY ts`, publicID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.SystemUsage
	for rows.Next() {
		u, err := scanUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

const usageCols = `ts, cpu_usage, memory_total, memory_used, swap_total, swap_used,
	disk_total, disk_used, net_recv, net_send, net_recv_speed, net_send_speed,
	load1, load5, load15`

func scanUsage(sc interface{ Scan(...any) error }) (*model.SystemUsage, error) {
	u := &model.SystemUsage{}
	err := sc.Scan(&u.Timestamp, &u.CPUUsage, &u.MemoryTotal, &u.MemoryUsed,
		&u.SwapTotal, &u.SwapUsed, &u.DiskTotal, &u.DiskUsed, &u.NetRecv, &u.NetSend,
		&u.NetRecvSpeed, &u.NetSendSpeed, &u.Load1, &u.Load5, &u.Load15)
	if err != nil {
		return nil, err
	}
	return u, nil
}
