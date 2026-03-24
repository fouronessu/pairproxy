# AI Coding 方法论：分享文档与模板

本目录包含基于 PairProxy 项目开发实战总结的 AI Coding 方法论，以及配套的可复用模板。

## 目录结构

```
docs/sharing/
├── README.md                       # 本文件：导览
├── talk.md                         # 完整演讲内容（方法论 + 实操案例）
├── templates/
│   ├── CLAUDE-md-template.md       # CLAUDE.md 项目上下文模板
│   ├── spec-template.md            # Spec 设计规格模板
│   ├── plan-template.md            # Plan 执行计划模板
│   └── ai-failure-checklist.md     # AI 六类失败模式 Review 检查表
```

## 快速导览

| 你想要的 | 看这个 |
|---------|--------|
| 完整演讲内容和案例 | [talk.md](./talk.md) |
| 给 AI 写项目背景文档 | [templates/CLAUDE-md-template.md](./templates/CLAUDE-md-template.md) |
| 写新功能的设计规格 | [templates/spec-template.md](./templates/spec-template.md) |
| 把规格拆解成可执行计划 | [templates/plan-template.md](./templates/plan-template.md) |
| Code Review 前的 AI 专项检查 | [templates/ai-failure-checklist.md](./templates/ai-failure-checklist.md) |

## 核心论点（一句话版）

> AI 是执行能力极强、但记忆极短、判断需要被约束的工程师。
> 你的工作是设计它的工作环境，而不是跟它聊天。

## 五个控制点

```
1. 上下文管理  →  CLAUDE.md：把"不言而喻"写出来
2. 任务定义    →  Spec：Why + What + Success Criteria + Out of Scope
3. 执行控制    →  Plan：File Map + Chunk + Step + Expected
4. 验证机制    →  TDD 强制：先写测试，先验证失败，再实现
5. 交付验收    →  结构化报告：数字 + 过程中发现的问题
```

## 背景：数据来源

本文档的所有方法论和案例均来自 PairProxy 项目的真实开发过程：
- **规模**：60,500+ 行 Go 代码
- **周期**：v1.0 → v2.18.0，20+ 个版本
- **测试**：1,894 个测试，0 失败
- **验收评分**：4.8 / 5.0
