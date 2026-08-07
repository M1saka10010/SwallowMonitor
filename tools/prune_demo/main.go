package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	_ "modernc.org/sqlite"
)

const totalRows = 1_000_000

func main() {
	dir, err := os.MkdirTemp("", "prune-demo")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	oldPath := filepath.Join(dir, "old.db")
	newPath := filepath.Join(dir, "new.db")

	fmt.Println("# 本地演示：100 万行 usages（约 20 台主机 × 10s 上报 × 5.8 天）")
	runOld(oldPath)
	runNew(newPath)

	fmt.Println("## 对比汇总")
	fmt.Println("| 指标 | 旧逻辑 | 新逻辑 |")
	fmt.Println("|---|---|---|")
}

func runOld(path string) {
	fmt.Println("## 旧逻辑（单条大 DELETE + 无 ts 索引 + 无 auto_vacuum）")
	db := openOld(path)
	defer db.Close()

	start := time.Now()
	insertRows(db)
	fmt.Printf("- 插入 %d 行耗时: %s\n", totalRows, time.Since(start).Round(time.Millisecond))

	rows := queryPlan(db, "DELETE FROM usages WHERE ts < 100")
	fmt.Printf("- 删除计划: %s\n", rows)

	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	before := fileSizes(path)
	stop := monitorWal(path)
	start = time.Now()
	res, err := db.Exec(`DELETE FROM usages WHERE ts < ?`, cutoff)
	if err != nil {
		panic(err)
	}
	deleted, _ := res.RowsAffected()
	elapsed := time.Since(start)
	stop()
	after := fileSizes(path)
	fmt.Printf("- 单条 DELETE %d 行耗时: %s\n", deleted, elapsed.Round(time.Millisecond))
	fmt.Printf("- WAL 峰值: %s（正常 checkpoint 水位约 4MB）\n", human(peakWal))
	fmt.Printf("- DB 文件: 删除前 %s → 删除后 %s（free page 不还给系统）\n", human(before[0]), human(after[0]))

	// 老库视角：旧库没有 ts 索引，prune 是全表扫描；新代码打开后建索引，
	// 文件空洞由后续碎片化检查（VacuumIfFragmented）自动压缩。
	start = time.Now()
	st2, err := store.Open(path)
	if err != nil {
		panic(err)
	}
	migrateElapsed := time.Since(start)
	migrated := fileSizes(path)
	st2.Close()
	fmt.Printf("- 新代码打开老库（migrate 补建 ts 索引）耗时: %s\n", migrateElapsed.Round(time.Millisecond))
	fmt.Printf("- 迁移后 DB 文件: %s（空洞待碎片化检查回收）\n", human(migrated[0]))
	fmt.Println()
}

func runNew(path string) {
	fmt.Println("## 新逻辑（ts 索引 + id 游标分批删除 + 碎片化时 VACUUM）")
	st, err := store.Open(path)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	host, err := st.CreateHost("Web-01", "token-demo", nil)
	if err != nil {
		panic(err)
	}

	start := time.Now()
	for i := 0; i < totalRows; i++ {
		u := &model.SystemUsage{
			Timestamp:   uint64(time.Now().Add(-10 * 24 * time.Hour).Unix() - int64(i)*10),
			CPUUsage:    12.5,
			MemoryTotal: 16000,
			MemoryUsed:  uint64(i % 8000),
			NetRecv:     uint64(i * 100),
			NetSend:     uint64(i * 50),
			Load1:       0.5,
			Load5:       0.4,
			Load15:      0.3,
		}
		if err := st.InsertUsage(host.PublicID, u); err != nil {
			panic(err)
		}
	}
	fmt.Printf("- 插入 %d 行耗时: %s\n", totalRows, time.Since(start).Round(time.Millisecond))

	stop := monitorWal(path)
	start = time.Now()
	deleted, err := st.PruneUsages(7)
	if err != nil {
		panic(err)
	}
	pruneElapsed := time.Since(start)
	stop()
	mid := fileSizes(path)
	fmt.Printf("- 分批 DELETE %d 行耗时: %s\n", deleted, pruneElapsed.Round(time.Millisecond))
	fmt.Printf("- WAL 峰值: %s\n", human(peakWal))
	fmt.Printf("- DB 文件（删除后、vacuum 前）: %s\n", human(mid[0]))

	start = time.Now()
	ran, err := st.VacuumIfFragmented()
	if err != nil {
		panic(err)
	}
	vacuumElapsed := time.Since(start)
	final := fileSizes(path)
	fmt.Printf("- VacuumIfFragmented 触发=%v 耗时: %s\n", ran, vacuumElapsed.Round(time.Millisecond))
	fmt.Printf("- DB 文件（VACUUM + checkpoint(TRUNCATE) 后）: %s\n", human(final[0]))
	fmt.Println()
}

func openOld(path string) *sql.DB {
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
	return db
}

func insertRows(db *sql.DB) {
	for i := 0; i < totalRows; i++ {
		if _, err := db.Exec(`INSERT INTO usages (
			public_id, ts, cpu_usage, memory_total, memory_used, swap_total, swap_used,
			disk_total, disk_used, net_recv, net_send, net_recv_speed, net_send_speed,
			load1, load5, load15
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"pub-"+fmt.Sprint(i%20), time.Now().Add(-10*24*time.Hour).Unix()-int64(i)*10,
			12.5, 16000, i%8000, 0, 0, 500, 300, i*100, i*50, 0, 0, 0.5, 0.4, 0.3,
		); err != nil {
			panic(err)
		}
	}
}

func queryPlan(db *sql.DB, stmt string) string {
	rows, err := db.Query(`EXPLAIN QUERY PLAN ` + stmt)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	var detail string
	for rows.Next() {
		var id, parent int
		var notUsed, d string
		if err := rows.Scan(&id, &parent, &notUsed, &d); err != nil {
			panic(err)
		}
		detail += d + " | "
	}
	return detail
}

var peakWal int64

func monitorWal(path string) func() {
	peakWal = 0
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() > peakWal {
				peakWal = fi.Size()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func fileSizes(path string) [2]int64 {
	var out [2]int64
	if fi, err := os.Stat(path); err == nil {
		out[0] = fi.Size()
	}
	if fi, err := os.Stat(path + "-wal"); err == nil {
		out[1] = fi.Size()
	}
	return out
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	}
}
