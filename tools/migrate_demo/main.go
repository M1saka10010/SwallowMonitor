package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
	_ "modernc.org/sqlite"
)

const (
	hostCount = 20
	days      = 10 // 旧库运行 10 天（> 新代码 1 天原始保留，也 > 默认 7 天 5m 保留）
	intervalS = 10
	totalRows = hostCount * days * 24 * 3600 / intervalS // 1,728,000
)

func main() {
	dir, err := os.MkdirTemp("", "migrate-demo")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "old.db")

	fmt.Println("# 旧版数据库升级演示：旧 schema（无 ts 索引、无聚合表）→ 新代码")

	// 1. 用旧版 schema 建库并灌入 10 天数据（模拟旧版长期运行的库）
	db, err := sql.Open("sqlite", path)
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
		CREATE TABLE usages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			public_id TEXT NOT NULL,
			ts INTEGER NOT NULL,
			cpu_usage REAL, memory_total INTEGER, memory_used INTEGER,
			swap_total INTEGER, swap_used INTEGER, disk_total INTEGER, disk_used INTEGER,
			net_recv INTEGER, net_send INTEGER, net_recv_speed REAL, net_send_speed REAL,
			load1 REAL, load5 REAL, load15 REAL
		);
		CREATE INDEX idx_usages_pub_ts ON usages(public_id, ts);`); err != nil {
		panic(err)
	}

	publicIDs := make([]string, hostCount)
	for i := range publicIDs {
		publicIDs[i] = fmt.Sprintf("demo-host-%02d", i)
	}

	now := time.Now().Unix()
	stmt, err := db.Prepare(`INSERT INTO usages (
		public_id, ts, cpu_usage, memory_total, memory_used, swap_total, swap_used,
		disk_total, disk_used, net_recv, net_send, net_recv_speed, net_send_speed,
		load1, load5, load15
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		panic(err)
	}
	start := time.Now()
	tx, _ := db.Begin()
	for i := 0; i < totalRows; i++ {
		ts := now - int64(days*24*3600) + int64(i/hostCount)*intervalS
		if _, err := tx.Stmt(stmt).Exec(
			publicIDs[i%hostCount], ts,
			float64(10+i%80), 16000, uint64(4000+i%12000), 2048, 512,
			500, uint64(300+i%200), uint64(i)*100, uint64(i)*50,
			float64(i%100), float64(i%60), 0.5, 0.4, 0.3,
		); err != nil {
			panic(err)
		}
		if i%5000 == 4999 {
			if err := tx.Commit(); err != nil {
				panic(err)
			}
			tx, _ = db.Begin()
		}
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}
	stmt.Close()
	db.Close()

	fmt.Printf("- 旧库：插入 %d 行（批量事务）: %s\n", totalRows, time.Since(start).Round(time.Millisecond))
	fmt.Printf("- 旧库结构: 仅 usages + idx_usages_pub_ts，无聚合表无 ts 索引\n")
	sizeOld := fileSize(path)
	fmt.Printf("- 旧库体积: %s\n\n", human(sizeOld))

	// 2. 升级：新代码打开旧库（migrate 自动补表补索引）
	start = time.Now()
	st, err := store.Open(path)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	migrateElapsed := time.Since(start)
	fmt.Printf("- 新代码打开旧库（migrate: 建 usages_5m/usages_1h + idx_usages_ts）: %s\n", migrateElapsed.Round(time.Millisecond))

	// 只读辅助连接
	ro, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer ro.Close()
	count := func(table string) int64 {
		var n int64
		if err := ro.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// 3. 首次维护循环：全量聚合（旧数据整体下沉，不丢历史）
	start = time.Now()
	if err := st.AggregateUsage(time.Now()); err != nil {
		panic(err)
	}
	aggElapsed := time.Since(start)
	fmt.Printf("- 首次聚合（旧数据全量下沉）: %s\n", aggElapsed.Round(time.Millisecond))
	fmt.Printf("  usages: %s → usages_5m: %s → usages_1h: %s\n",
		formatCount(count("usages")), formatCount(count("usages_5m")), formatCount(count("usages_1h")))

	start = time.Now()
	if err := st.AggregateUsage(time.Now()); err != nil {
		panic(err)
	}
	fmt.Printf("- 增量聚合（第二轮）: %s\n\n", time.Since(start).Round(time.Millisecond))

	// 4. 清理：原始固定 1 天 + 5m 按配置 7 天 + 1h 一年
	n1, err := st.PruneUsages(1)
	if err != nil {
		panic(err)
	}
	n2, err := st.PruneDownsampled(7)
	if err != nil {
		panic(err)
	}
	vacuumed, err := st.VacuumIfFragmented()
	if err != nil {
		panic(err)
	}
	fmt.Printf("- 清理（原始 1 天 + 5m 7 天）: 删原始 %s 行、删聚合 %s 行，VACUUM=%v\n",
		formatCount(n1), formatCount(n2), vacuumed)
	fmt.Printf("  清理后 usages: %s | usages_5m: %s | usages_1h: %s\n",
		formatCount(count("usages")), formatCount(count("usages_5m")), formatCount(count("usages_1h")))
	fmt.Printf("- 升级后体积: %s（旧库 %s）\n\n", human(fileSize(path)), human(sizeOld))

	// 5. 升级后查询可用性：7d 视图走 5m 表
	pid := publicIDs[0]
	start = time.Now()
	points, err := st.QueryUsage(pid, now-7*24*3600, now)
	if err != nil {
		panic(err)
	}
	fmt.Printf("- 升级后 7d 视图查询（→ usages_5m）: %s，返回 %d 点\n", time.Since(start).Round(time.Microsecond), len(points))
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	}
}

func formatCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
