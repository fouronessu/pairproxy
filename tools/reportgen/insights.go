package main

import (
	"fmt"
	"math"
	"sort"
)

// GenerateInsights produces analytical text insights from populated report data.
func GenerateInsights(data *ReportData) []Insight {
	var insights []Insight

	if g := growthInsight(data); g != nil {
		insights = append(insights, *g)
	}
	if a := anomalyInsight(data); a != nil {
		insights = append(insights, *a)
	}
	if c := costWarningInsight(data); c != nil {
		insights = append(insights, *c)
	}
	if e := engagementInsight(data); e != nil {
		insights = append(insights, *e)
	}
	if e := efficiencyInsight(data); e != nil {
		insights = append(insights, *e)
	}
	if c := capacityInsight(data); c != nil {
		insights = append(insights, *c)
	}
	if m := modelSuggestionInsight(data); m != nil {
		insights = append(insights, *m)
	}

	return insights
}

func growthInsight(data *ReportData) *Insight {
	if data.KPI.PrevTotalRequests == 0 {
		return nil
	}
	k := data.KPI
	detail := fmt.Sprintf(
		"• 总请求量：%s 次（%s%.1f%% vs 上一周期）\n"+
			"• 总 Token 用量：%s（%s%.1f%%）\n"+
			"• 总费用：$%.2f（%s%.1f%%）\n"+
			"• 活跃用户：%d 人（%s%.1f%%）\n"+
			"• 错误率：%.2f%%（%s%.1f%%）\n"+
			"• 平均延迟：%.0fms（%s%.1f%%）",
		formatInt64(k.TotalRequests), arrow(k.RequestsChange), math.Abs(k.RequestsChange),
		formatInt64(k.TotalTokens), arrow(k.TokensChange), math.Abs(k.TokensChange),
		k.TotalCost, arrow(k.CostChange), math.Abs(k.CostChange),
		k.ActiveUsers, arrow(k.UsersChange), math.Abs(k.UsersChange),
		k.ErrorRate, arrow(k.ErrorRateChange), math.Abs(k.ErrorRateChange),
		k.AvgLatencyMs, arrow(k.LatencyChange), math.Abs(k.LatencyChange),
	)
	return &Insight{Type: "growth", Title: "📈 环比变化分析", Detail: detail, Emoji: "📈"}
}

func anomalyInsight(data *ReportData) *Insight {
	var lines []string

	// Check daily anomalies
	if len(data.DailyTrend) > 0 {
		var totalReqs int64
		for _, d := range data.DailyTrend {
			totalReqs += d.Requests
		}
		avgDaily := float64(totalReqs) / float64(len(data.DailyTrend))
		for _, d := range data.DailyTrend {
			if avgDaily > 0 && float64(d.Requests) > avgDaily*2 {
				lines = append(lines, fmt.Sprintf("• %s 请求量 %d 次，达到日均 %.0f 次的 %.1f 倍，建议确认是否为正常业务需求",
					d.Date, d.Requests, avgDaily, float64(d.Requests)/avgDaily))
			}
		}
	}

	// Error rate check
	if data.KPI.ErrorRate > 5 {
		lines = append(lines, fmt.Sprintf("• 周期错误率 %.2f%% 超过 5%% 阈值，需关注服务稳定性", data.KPI.ErrorRate))
	}

	// P95 latency check
	if data.KPI.P95LatencyMs > 30000 {
		lines = append(lines, fmt.Sprintf("• P95 延迟 %dms 超过 30s 阈值，存在慢请求瓶颈", data.KPI.P95LatencyMs))
	}

	// Top user concentration
	if len(data.TopUsersByToken) > 0 {
		top := data.TopUsersByToken[0]
		if data.KPI.TotalTokens > 0 {
			pct := float64(top.InputTokens+top.OutputTokens) / float64(data.KPI.TotalTokens) * 100
			if pct > 50 {
				lines = append(lines, fmt.Sprintf("• 用户 %s 占总用量的 %.1f%%，集中度过高", top.Username, pct))
			}
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return &Insight{
		Type:   "anomaly",
		Title:  "⚠️ 异常检测",
		Detail: joinLines(lines),
		Emoji:  "⚠️",
	}
}

func costWarningInsight(data *ReportData) *Insight {
	var lines []string

	// Check model cost concentration
	if len(data.ModelDistribution) > 0 {
		totalCost := data.KPI.TotalCost
		if totalCost > 0 {
			// Find highest cost model
			type mc struct {
				name      string
				cost      float64
				pct       float64
				reqs      int64
				totalReqs int64
			}
			var models []mc
			var totalReqs int64
			for _, m := range data.ModelDistribution {
				totalReqs += m.Count
				models = append(models, mc{name: m.Model, cost: m.CostUSD, reqs: m.Count})
			}
			sort.Slice(models, func(i, j int) bool { return models[i].cost > models[j].cost })
			if len(models) > 0 {
				top := models[0]
				topPct := top.cost / totalCost * 100
				reqPct := float64(top.reqs) / float64(totalReqs) * 100
				if topPct > 50 {
					lines = append(lines, fmt.Sprintf(
						"• 模型 %s 占总费用的 %.1f%%（$%.2f），但仅占请求量的 %.1f%%，建议评估是否有更经济的替代方案",
						top.name, topPct, top.cost, reqPct))
				}
			}
		}
	}

	// Monthly cost extrapolation
	if data.PeriodDays > 0 && data.PeriodDays < 30 {
		dailyAvg := data.KPI.TotalCost / float64(data.PeriodDays)
		projected := dailyAvg * 30
		lines = append(lines, fmt.Sprintf(
			"• 按当前日均消耗 $%.2f 外推，预计月费用 $%.2f", dailyAvg, projected))
	}

	if len(lines) == 0 {
		return nil
	}
	return &Insight{
		Type:   "cost_warning",
		Title:  "💰 成本预警",
		Detail: joinLines(lines),
		Emoji:  "💰",
	}
}

func engagementInsight(data *ReportData) *Insight {
	var lines []string
	e := data.Engagement

	lines = append(lines, fmt.Sprintf(
		"• 活跃用户 %d / 注册用户 %d（采纳率 %.1f%%）",
		e.WAU, data.KPI.RegisteredUsers, e.AdoptionRate))

	if e.MAU > 0 {
		stickiness := e.Stickness
		lines = append(lines, fmt.Sprintf(
			"• DAU/MAU = %d/%d = %.1f%%（%s）",
			e.DAU, e.MAU, stickiness, sticknessComment(stickiness)))
	}

	// ROI: per-user cost
	if e.WAU > 0 && data.KPI.TotalCost > 0 {
		perUserCost := data.KPI.TotalCost / float64(e.WAU)
		lines = append(lines, fmt.Sprintf(
			"• 人均成本：$%.2f/用户（总费用 $%.2f ÷ %d 活跃用户）",
			perUserCost, data.KPI.TotalCost, e.WAU))
	}

	// Pareto check
	if len(data.ParetoData) >= 3 {
		top3 := data.ParetoData[2].CumulativePct
		lines = append(lines, fmt.Sprintf(
			"• TOP 3 用户贡献 %.1f%% 用量（帕累托效应%s）",
			top3, paretoComment(top3)))
	}

	if e.ZeroUseCount > 0 {
		lines = append(lines, fmt.Sprintf(
			"• %d 名用户（%.1f%%）本周期零使用，建议推动培训或回收配额",
			e.ZeroUseCount, e.ZeroUsePct))
	}

	if e.NewUsersThisPeriod > 0 {
		lines = append(lines, fmt.Sprintf(
			"• 本周期新增 %d 名用户开始使用", e.NewUsersThisPeriod))
	}

	if len(data.Engagement.PowerUsers) > 0 {
		names := ""
		for i, u := range data.Engagement.PowerUsers {
			if i > 0 {
				names += "、"
			}
			names += u
		}
		lines = append(lines, fmt.Sprintf("• 核心用户（TOP 5%%）：%s", names))
	}

	return &Insight{
		Type:   "engagement",
		Title:  "👥 用户参与度",
		Detail: joinLines(lines),
		Emoji:  "👥",
	}
}

func efficiencyInsight(data *ReportData) *Insight {
	var lines []string
	sr := data.StreamingRatio

	// Streaming vs non-streaming latency
	if sr.NonStreamingAvgLatency > 0 && sr.StreamingAvgLatency > 0 {
		ratio := sr.NonStreamingAvgLatency / sr.StreamingAvgLatency
		if ratio > 2 {
			lines = append(lines, fmt.Sprintf(
				"• 非流式请求平均延迟 %.0fms，是流式请求（%.0fms）的 %.1f 倍，建议全面切换流式模式",
				sr.NonStreamingAvgLatency, sr.StreamingAvgLatency, ratio))
		}
	}

	// Model error rates
	for _, m := range data.ModelDistribution {
		if m.ErrorRate > 5 {
			lines = append(lines, fmt.Sprintf(
				"• 模型 %s 错误率 %.2f%%，高于正常水平（<1%%），建议检查配置或上游可用性",
				m.Model, m.ErrorRate))
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return &Insight{
		Type:   "efficiency",
		Title:  "🎯 效率优化建议",
		Detail: joinLines(lines),
		Emoji:  "🎯",
	}
}

func capacityInsight(data *ReportData) *Insight {
	var lines []string

	// Peak RPM estimate
	if len(data.DailyTrend) > 0 {
		var maxReqs int64
		for _, d := range data.DailyTrend {
			if d.Requests > maxReqs {
				maxReqs = d.Requests
			}
		}
		peakRPM := float64(maxReqs) / 1440 * 3 // burst multiplier
		lines = append(lines, fmt.Sprintf(
			"• 峰值日请求 %d 次，估算峰值 RPM ≈ %.0f（含突发系数 3x）",
			maxReqs, peakRPM))
	}

	// Upstream distribution
	if len(data.UpstreamStats) > 1 {
		var totalReqs int64
		for _, u := range data.UpstreamStats {
			totalReqs += u.Requests
		}
		for _, u := range data.UpstreamStats {
			pct := float64(u.Requests) / float64(totalReqs) * 100
			if pct > 80 {
				lines = append(lines, fmt.Sprintf(
					"• 上游 %s 承载 %.1f%% 流量，负载集中，建议增加备用端点",
					shortenURL(u.URL), pct))
			}
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return &Insight{
		Type:   "capacity",
		Title:  "🔧 容量评估",
		Detail: joinLines(lines),
		Emoji:  "🔧",
	}
}

func modelSuggestionInsight(data *ReportData) *Insight {
	if len(data.ModelDistribution) < 2 {
		return nil
	}

	totalCost := data.KPI.TotalCost
	totalReqs := data.KPI.TotalRequests
	if totalCost == 0 || totalReqs == 0 {
		return nil
	}

	var lines []string
	for _, m := range data.ModelDistribution {
		costPct := m.CostUSD / totalCost * 100
		reqPct := float64(m.Count) / float64(totalReqs) * 100
		if costPct > 60 && reqPct < 20 {
			lines = append(lines, fmt.Sprintf(
				"• 模型 %s 占费用 %.1f%% 但仅占请求 %.1f%%，建议评估是否有更经济的替代模型",
				m.Model, costPct, reqPct))
		}
	}

	if len(lines) == 0 {
		return nil
	}
	return &Insight{
		Type:   "model_suggestion",
		Title:  "🏷️ 模型使用建议",
		Detail: joinLines(lines),
		Emoji:  "🏷️",
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func arrow(change float64) string {
	if change > 0 {
		return "↑"
	}
	if change < 0 {
		return "↓"
	}
	return "→"
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

func sticknessComment(s float64) string {
	if s >= 50 {
		return "粘性优秀"
	}
	if s >= 20 {
		return "粘性良好"
	}
	return "粘性偏低，用户回访率不足"
}

func paretoComment(top3Pct float64) string {
	if top3Pct > 80 {
		return "极显著"
	}
	if top3Pct > 50 {
		return "显著"
	}
	return "不明显"
}

func shortenURL(url string) string {
	if len(url) > 40 {
		return url[:37] + "..."
	}
	return url
}

func formatInt64(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
