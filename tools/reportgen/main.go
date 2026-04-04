package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	var dbPath, fromStr, toStr, outputPath, templatePath string

	flag.StringVar(&dbPath, "db", "", "SQLite 数据库文件路径（必填）")
	flag.StringVar(&fromStr, "from", "", "开始日期，格式 YYYY-MM-DD（必填）")
	flag.StringVar(&toStr, "to", "", "结束日期，格式 YYYY-MM-DD（必填）")
	flag.StringVar(&outputPath, "output", "report.html", "输出 HTML 文件路径")
	flag.StringVar(&templatePath, "template", "templates/report.html", "HTML 模板文件路径")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "PairProxy 报告生成器 — 从数据库生成可视化分析报告\n\n")
		fmt.Fprintf(os.Stderr, "用法: reportgen -db <数据库路径> -from <开始日期> -to <结束日期> [选项]\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  reportgen -db pairproxy.db -from 2026-04-01 -to 2026-04-07 -output weekly.html\n")
	}

	flag.Parse()

	// Validate required flags
	if dbPath == "" || fromStr == "" || toStr == "" {
		fmt.Fprintf(os.Stderr, "错误：-db、-from、-to 为必填参数\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Validate db file exists
	if !fileExists(dbPath) {
		fmt.Fprintf(os.Stderr, "错误：数据库文件不存在: %s\n", dbPath)
		os.Exit(1)
	}

	// Parse dates
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误：无效的开始日期格式: %s（需要 YYYY-MM-DD）\n", fromStr)
		os.Exit(1)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误：无效的结束日期格式: %s（需要 YYYY-MM-DD）\n", toStr)
		os.Exit(1)
	}
	to = endOfDay(to)

	if !from.Before(to) {
		fmt.Fprintf(os.Stderr, "错误：开始日期必须早于结束日期\n")
		os.Exit(1)
	}

	// Resolve template path
	tmplPath, err := filepath.Abs(templatePath)
	if err != nil {
		tmplPath = templatePath
	}

	// Resolve output path
	outPath, err := filepath.Abs(outputPath)
	if err != nil {
		outPath = outputPath
	}

	params := QueryParams{
		DBPath: dbPath,
		From:   from,
		To:     to,
	}

	if err := GenerateReport(params, tmplPath, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "错误：%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ 报告已生成: %s\n", outPath)
}

func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
