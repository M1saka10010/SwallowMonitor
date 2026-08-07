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
	hostCount  = 20
	intervalS  = 10
	days       = 30
	totalRows  = hostCount * days * 24 * 3600 / intervalS // 5,184,000
	rawRetain  = 1
	retainDays = 7
)

func main() {
	dir, err := os.MkdirTemp("", "downsample-demo")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "demo.db")

	fmt.Println("# 分级保留演示：20 主机 × 10s 上报 × 30 天")

	// 1. 建表 + 注册主机
	st, err := store.Open(path)
	if err != nil {
		panic(err)
	}
	publicIDs := make([]string, hostCount)
	for i := range publicIDs {
		h, err := st.CreateHost(fmt.Sprintf("Web-%02d", i), fmt.Sprintf("token-%d", i), nil)
		if err != nil {
			panic(err)
		}
		publicIDs[i] = h.PublicID
	}
	st.Close()

	// 2. 批量插入 30 天原始数据（模拟优化前的库）
	db, err := sql.Open("sqlite", path)
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)
	start := time.Now()
	now := time.Now().Unix()
	stmt, err := db.Prepare(`INSERT INTO usages (
		public_id, ts, cpu_usage, memory_total, memory_used, swap_total, swap_used,
		disk_total, disk_used, net_recv, net_send, net_recv_speed, net_send_speed,
		load1, load5, load15
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		panic(err)
	}
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
	insertElapsed := time.Since(start)
	stmt.Close()
	db.Close()

	fmt.Printf("- 批量插入 %d 行（批量事务，5000 行/事务）: %s\n", totalRows, insertElapsed.Round(time.Millisecond))
	sizeBefore := fileSize(path)
	fmt.Printf("- 优化前体积（30 天原始全量）: %s\n", human(sizeBefore))

	// 3. 重新打开，首次全量聚合
	st, err = store.Open(path)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	// 只读辅助连接（WAL 支持与 store 连接并发读）
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

	start = time.Now()
	if err := st.AggregateUsage(time.Now()); err != nil {
		panic(err)
	}
	aggElapsed := time.Since(start)
	fmt.Printf("\n- 首次全量聚合耗时: %s\n", aggElapsed.Round(time.Millisecond))
	fmt.Printf("  usages 原始行数: %s\n", formatCount(count("usages")))
	fmt.Printf("  usages_5m 行数: %s\n", formatCount(count("usages_5m")))
	fmt.Printf("  usages_1h 行数: %s\n", formatCount(count("usages_1h")))

	// 增量聚合（模拟后续每 5 分钟一轮）
	start = time.Now()
	if err := st.AggregateUsage(time.Now()); err != nil {
		panic(err)
	}
	fmt.Printf("- 增量聚合（第二轮，应无新桶）: %s\n", time.Since(start).Round(time.Millisecond))

	// 旧路径查询：7 天视图直接扫原始表（优化前行为，数据尚未清理）
	from := now - 7*24*3600
	to := now
	pid := publicIDs[0]
	start = time.Now()
	oldRows, err := ro.Query(`SELECT ts FROM usages WHERE public_id = ? AND ts >= ? AND ts <= ? ORDER BY ts`, pid, from, to)
	if err != nil {
		panic(err)
	}
	oldCount := 0
	for oldRows.Next() {
		oldCount++
	}
	oldRows.Close()
	fmt.Printf("\n- 7d 视图查询（旧路径 → 原始表全量）: %s，返回 %d 点\n",
		time.Since(start).Round(time.Microsecond), oldCount)

	// 4. 清理：原始 1 天 + 5m 按 retentionDays（7 天）+ 1h 一年
	start = time.Now()
	n1, err := st.PruneUsages(rawRetain)
	if err != nil {
		panic(err)
	}
	n2, err := st.PruneDownsampled(retainDays)
	if err != nil {
		panic(err)
	}
	pruneElapsed := time.Since(start)
	vacuumed, err := st.VacuumIfFragmented()
	if err != nil {
		panic(err)
	}
	fmt.Printf("- 清理（原始 %d 天 + 5m %d 天 + 1h 365 天）: %s（删原始 %d 行、删聚合 %d 行，VACUUM=%v）\n",
		rawRetain, retainDays, pruneElapsed.Round(time.Millisecond), n1, n2, vacuumed)
	fmt.Printf("  清理后 usages: %s | usages_5m: %s | usages_1h: %s\n",
		formatCount(count("usages")), formatCount(count("usages_5m")), formatCount(count("usages_1h")))
	sizeAfter := fileSize(path)
	fmt.Printf("- 优化后体积: %s（含索引）\n", human(sizeAfter))

	// 新路径查询：7 天视图走 5m 聚合表
	start = time.Now()
	points, err := st.QueryUsage(pid, from, to)
	if err != nil {
		panic(err)
	}
	fmt.Printf("- 7d 视图查询（新路径 → usages_5m）: %s，返回 %d 点\n",
		time.Since(start).Round(time.Microsecond), len(points))

	// 稳态模拟：1h 表灌满 365 天（20 主机 × 8760 小时桶）
	steadyBefore := fileSize(path)
	start = time.Now()
	ins, err := ro.Prepare(`INSERT INTO usages_1h (public_id, bucket_ts, cpu_usage) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		panic(err)
	}
	tx2, _ := ro.Begin()
	hourStart := now - 365*24*3600
	for h := 0; h < 365*24; h++ {
		ts := hourStart + int64(h)*3600
		for _, pid2 := range publicIDs {
			if _, err := tx2.Stmt(ins).Exec(pid2, ts, 30.0); err != nil {
				panic(err)
			}
		}
	}
	if err := tx2.Commit(); err != nil {
		panic(err)
	}
	ins.Close()
	steadyElapsed := time.Since(start)
	steadySize := fileSize(path)
	fmt.Printf("\n- 稳态模拟：1h 表扩展至 365 天（%s 行）: %s，体积 %s → %s\n",
		formatCount(20*365*24), steadyElapsed.Round(time.Millisecond), human(steadyBefore), human(steadySize))

	fmt.Printf("\n## 汇总\n")
	fmt.Printf("| 项目 | 优化前（30 天原始全量） | 优化后（分级保留） |\n")
	fmt.Printf("|---|---|---|\n")
	fmt.Printf("| 行数 | %s | %s + %s + %s |\n",
		formatCount(totalRows), formatCount(count("usages")), formatCount(count("usages_5m")), formatCount(count("usages_1h")))
	fmt.Printf("| 体积 | %s | %s |\n", human(sizeBefore), human(sizeAfter))
	fmt.Printf("| 历史深度 | 30 天原始 | 1 天原始 + %d 天 5m + 365 天 1h |\n", retainDays)
	fmt.Printf("| 一年后稳态体积 | — | %s（含 365 天 1h 数据） |\n", human(steadySize))
}

func formatCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
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
