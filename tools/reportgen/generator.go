package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// GenerateReport orchestrates the full report generation pipeline.
// All query failures are non-fatal: missing data produces empty sections.
// LLM failure degrades gracefully to rule-based insights only.
func GenerateReport(params QueryParams, templatePath, outputPath string) error {
	q, err := NewQuerier(params.Driver, params.DSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer q.Close()

	var data ReportData
	data.Title = "PairProxy 分析报告"
	data.PeriodLabel = formatPeriodLabel(params.From, params.To)
	data.GeneratedAt = time.Now().Format("2006-01-02 15:04:05")
	data.PeriodDays = int(params.To.Sub(params.From).Hours() / 24)
	if data.PeriodDays < 1 {
		data.PeriodDays = 1
	}
	data.PrevLabel = formatPrevLabel(params.From, params.To)

	// Run all queries (best-effort: individual failures yield nil/zero, not abort)
	data.KPI, _ = q.QueryKPI(params.From, params.To)
	data.DailyTrend, _ = q.QueryDailyTrend(params.From, params.To)
	data.HeatmapData, _ = q.QueryHeatmap(params.From, params.To)
	data.TopUsersByToken, _ = q.QueryTopUsers(params.From, params.To, "tokens", 10)
	data.TopUsersByCost, _ = q.QueryTopUsers(params.From, params.To, "cost", 10)
	data.TopUsersByRequest, _ = q.QueryTopUsers(params.From, params.To, "requests", 10)
	data.ModelDistribution, _ = q.QueryModelDistribution(params.From, params.To)
	data.GroupComparison, _ = q.QueryGroupComparison(params.From, params.To)
	data.UpstreamStats, _ = q.QueryUpstreamStats(params.From, params.To)
	data.StatusCodeDist, _ = q.QueryStatusCodeDist(params.From, params.To)
	data.SlowRequests, _ = q.QuerySlowRequests(params.From, params.To, 10)
	data.ErrorRequests, _ = q.QueryErrorRequests(params.From, params.To)
	data.StreamingRatio, _ = q.QueryStreamingRatio(params.From, params.To)

	registeredUsers := q.CountRegisteredUsers()
	data.Engagement, _ = q.QueryEngagement(params.From, params.To, registeredUsers)

	data.UserFreqBuckets, _ = q.QueryUserFreqBuckets(params.From, params.To)
	data.IORatioBuckets, _ = q.QueryIORatioBuckets(params.From, params.To)
	data.ParetoData, _ = q.QueryParetoData(params.From, params.To)

	// Phase 2: Latency analysis
	data.LatencyBoxPlots, _ = q.QueryLatencyBoxPlotByModel(params.From, params.To)
	data.LatencyPercentiles, _ = q.QueryLatencyPercentileTrend(params.From, params.To)
	data.DailyLatencyTrend, _ = q.QueryDailyLatencyTrend(params.From, params.To)

	// Phase 3: Advanced analysis
	data.RetentionData, _ = q.QueryRetentionData(params.From, params.To)
	data.IOScatterPlot, _ = q.QueryIOScatterPlot(params.From, params.To, 1000)
	data.ModelCostBreakdown, _ = q.QueryModelCostBreakdown(params.From, params.To)

	// Phase 4: High-value supplements
	data.EngagementTrend, _ = q.QueryEngagementTrend(params.From, params.To)
	data.QuotaUsage, _ = q.QueryQuotaUsage(params.From, params.To)
	data.UpstreamLatencyBoxPlot, _ = q.QueryLatencyBoxPlotByUpstream(params.From, params.To)

	// Phase 5: Medium-value supplements
	data.GroupTokenBoxPlots, _ = q.QueryGroupTokenDistribution(params.From, params.To)

	// Phase 6: Low-frequency enhancements
	data.ModelRadarData, _ = q.QueryModelRadarData(params.From, params.To)
	data.AdoptionRate.TotalRegistered = q.CountRegisteredUsers()
	activeUsers, _ := q.QueryActiveUsersInPeriod(params.From, params.To)
	data.AdoptionRate.TotalActive = activeUsers
	if data.AdoptionRate.TotalRegistered > 0 {
		data.AdoptionRate.AdoptionPercent = float64(activeUsers) / float64(data.AdoptionRate.TotalRegistered) * 100
	}

	// Phase 7: Request-count analytics
	data.UserRequestBoxPlot, _ = q.QueryUserRequestBoxPlot(params.From, params.To)

	// Phase 8: Missing/partial features
	data.LatencyHistogram, _ = q.QueryLatencyHistogram(params.From, params.To)
	data.LatencyScatter, _ = q.QueryLatencyScatter(params.From, params.To, 1000)
	data.TokenThroughputHeatmap, _ = q.QueryTokenThroughputHeatmap(params.From, params.To)
	data.UpstreamShare, _ = q.QueryUpstreamShare(params.From, params.To)
	data.UpstreamLatencyTrend, _ = q.QueryUpstreamLatencyTrend(params.From, params.To)
	data.CostPerTokenTrend, _ = q.QueryCostPerTokenTrend(params.From, params.To)
	data.IORatioTrend, _ = q.QueryIORatioTrend(params.From, params.To)
	data.ModelInputBoxPlots, _ = q.QueryModelTokenBoxPlots(params.From, params.To, "input_tokens")
	data.ModelOutputBoxPlots, _ = q.QueryModelTokenBoxPlots(params.From, params.To, "output_tokens")
	data.SourceNodeDist, _ = q.QuerySourceNodeDist(params.From, params.To)
	data.StreamingBoxPlot, _ = q.QueryStreamingBoxPlot(params.From, params.To)
	data.ModelDailyTrend, _ = q.QueryModelDailyTrend(params.From, params.To)
	data.KPI.PeakRPM, _ = q.QueryPeakRPM(params.From, params.To)

	// Phase 9: remaining gaps
	data.UserTierDist, _ = q.QueryUserTierDist(params.From, params.To)
	data.UserTokenPercentiles, _ = q.QueryUserTokenPercentiles(params.From, params.To)

	// Warn when no data was found (empty period or new deployment)
	if data.KPI.TotalRequests == 0 {
		fmt.Fprintf(os.Stderr, "⚠️  指定时间段内无请求数据，将生成空报告\n")
		data.Insights = []Insight{{
			Type:   "no_data",
			Title:  "📭 暂无数据",
			Detail: fmt.Sprintf("在 %s 期间未找到任何请求记录。请确认时间范围和数据库路径是否正确。", data.PeriodLabel),
			Emoji:  "📭",
		}}
	} else {
		// Generate rule-based insights (with panic recovery)
		data.Insights = safeGenerateInsights(&data)

		// Generate LLM insights (best-effort, degrades to rule-based on failure)
		if llmInsight := GenerateLLMInsights(&data, params); llmInsight != nil {
			data.Insights = append(data.Insights, *llmInsight)
		}
	}

	// Read template; fall back to minimal HTML on failure
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  模板读取失败（%v），使用内置最小模板\n", err)
		tmplBytes = []byte(minimalTemplate)
	}

	// Marshal data to JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	// Inject JSON into template
	jsonStr := string(jsonBytes)
	// Escape </script> to prevent premature script tag closure
	jsonStr = strings.ReplaceAll(jsonStr, "</script>", "<\\/script>")

	html := strings.ReplaceAll(string(tmplBytes), "{{REPORT_DATA}}", jsonStr)

	// Write output
	if err := os.WriteFile(outputPath, []byte(html), 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}

// safeGenerateInsights wraps GenerateInsights with panic recovery so that
// a bug in any insight rule cannot crash the entire report generation.
func safeGenerateInsights(data *ReportData) (insights []Insight) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "⚠️  规则洞察生成异常（已跳过）: %v\n", r)
			insights = nil
		}
	}()
	return GenerateInsights(data)
}

// minimalTemplate is used when the HTML template file cannot be read.
// It renders the report data as formatted JSON for debugging purposes.
const minimalTemplate = `<!DOCTYPE html>
<html lang="zh">
<head><meta charset="UTF-8"><title>PairProxy 报告（降级模式）</title>
<style>body{font-family:monospace;padding:20px;background:#f5f5f5}
pre{background:#fff;padding:20px;border-radius:8px;overflow:auto;font-size:12px}
h1{color:#e74c3c}p{color:#666}</style></head>
<body>
<h1>⚠️ 报告模板加载失败（降级模式）</h1>
<p>HTML 模板文件不可用，以下为原始报告数据（JSON）。请检查 <code>-template</code> 参数路径是否正确。</p>
<pre id="data"></pre>
<script>
const d = {{REPORT_DATA}};
document.getElementById('data').textContent = JSON.stringify(d, null, 2);
</script>
</body></html>`

func formatPeriodLabel(from, to time.Time) string {
	return fmt.Sprintf("%s 至 %s",
		from.Format("2006-01-02"),
		to.Add(-time.Second).Format("2006-01-02"))
}

func formatPrevLabel(from, to time.Time) string {
	pf, pt := QueryParams{From: from, To: to}.PrevPeriod()
	return formatPeriodLabel(pf, pt)
}
