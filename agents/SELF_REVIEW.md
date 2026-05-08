# 通用功能模块自评审流程

> 一键触发：`ulw 启动自评审流程，目标：<包路径 | 功能模块描述>`
> 交互触发：`ulw 我要自评审`（逐步问答，无需记参数）
> 示例：`ulw 启动自评审流程，目标：core/helper/search`
> 示例：`ulw 启动自评审流程，目标：订单结算流程`
> 示例：`ulw 启动自评审流程，目标：库存同步与发货`

## 触发方式

支持两种触发模式：

### 模式 1：直接触发（参数完整）

适合熟悉格式、一次性写全的场景。

Sisyphus 将根据输入自动判断目标类型：

- **Go 包路径**（包含 `/` 或 `.`，如 `core/helper/search`）→ 走包发现模式
- **功能模块描述**（自然语言，如 `订单结算流程`）→ 走功能模块发现模式
- **手动指定文件清单**：在上述参数后追加 `|` 分隔的文件路径，跳过自动发现

```
ulw 启动自评审流程，目标：<TARGET> [| file1.go,file2.go] [可选: 额外上下文]
```

额外上下文示例：
- `该模块负责订单结算，依赖 MySQL + Redis`
- `该模块是新增的，需要特别注意错误处理`
- `重点审查并发安全性`
- `涉及多服务交互：go-game-trade-serve → go-goods-serve`

### 模式 2：交互式触发（推荐）

记不住格式、或者想一步步来的时候用。无需记忆任何参数。

```
ulw 我要自评审
```

Sisyphus 收到后将通过对话逐项询问：

1. **评审目标** — 包路径？功能模块描述？还是直接给文件列表？
2. **额外上下文** — 业务背景、关注重点、涉及的服务等
3. **确认** — 展示理解到的目标，让用户确认后再启动

相当于把一次性参数填写变成了问答式引导，降低心智负担。

## Sisyphus 自动执行流程

Sisyphus 收到此请求后将执行：

### Phase 0: 文件发现

**模式 A — 按包路径发现：**
- 解析目标包路径
- `glob` + `grep` 发现包内所有 `.go` 文件
- `grep` 发现包间引用关系
- 汇总为「覆盖文件清单」

**模式 B — 按功能模块描述发现：**
- 启动 2 个 `explore` Agent 并行探索：
  - Agent 1：根据功能描述在相关服务目录下搜索关键词、结构体、函数
  - Agent 2：根据功能描述搜索配置、路由注册、API 入口等外围文件
- 合并结果去重，形成「覆盖文件清单」
- 如发现跨服务调用，在报告中注明涉及的外部服务

**模式 C — 手动指定文件清单：**
- 跳过自动发现，直接使用用户提供的文件路径列表
- 对每个文件做存在性验证，不存在的文件报告警告

### Phase 1: 基线检查

- 若覆盖文件清单归属单一服务或单一包 → `go build ./<PACKAGE>...` + `go test ./<PACKAGE>...`
- 若跨多个包 → 对每个涉及的独立包分别运行 build + test
- 失败则先不进入审查，报告用户

### Phase 2: 审查循环 (直至质量达标)

循环核心原则：**不设轮次上限，只以质量门禁是否全部通过为终止条件。**

#### 质量门禁（必须全部通过）

| # | 门禁 | 判定方式 | 一票否决 |
|---|------|----------|----------|
| G1 | 代码正确性审查 verdict = PASS | Oracle Agent 1 | 是 |
| G2 | 安全+边界审查 verdict = PASS | Oracle Agent 2 | 是 |
| G3 | 架构+模式审查 verdict = PASS | Oracle Agent 3 | 是 |
| G4 | 零 CRITICAL/MAJOR 残留 | 汇总所有 findings 检查 | 是 |
| G5 | 零回归问题 | 对比上一轮 findings，新引入的算回归 | 是 |
| G6 | go build 通过 | bash 执行 | 是 |
| G7 | go test 通过 | bash 执行 | 是 |

所有门禁通过（PASS）才算质量达标，否则继续循环。

#### 循环流程

```
第 N 轮:
  ├─ 启动 3 个 Oracle 并行审查（使用 Phase 0 发现的文件清单）:
  │   bg_1: 代码正确性 (oracle)
  │   bg_2: 安全 + 边界条件 (oracle)
  │   bg_3: 架构 + 模式 (oracle)
  ├─ 等待全部完成 → 汇总所有 findings
  ├─ 质量门禁检查:
  │   ├─ 全部 7 项 PASS → 输出最终报告，循环终止
  │   └─ 有 FAIL 项 → 进入修复流程
  ├─ 修复流程:
  │   ├─ 回归问题优先修复（先还旧债，再修新债）
  │   ├─ 按类型分流到修复 Agent:
  │   │    单文件修改 → category="quick"
  │   │    多文件/复杂 → category="deep"
  │   └─ 修复后执行 build + test
  ├─ 如果连续 3 轮同一门禁 FAIL（僵局处理）:
  │   ├─ 启动 Oracle 深度诊断，分析为什么反复修不好
  │   ├─ 输出根因分析 + 替代方案
  │   └─ 上报用户决策：继续修 / 接受现状 / 改方案
  └─ 进入第 N+1 轮
```

#### 僵局处理（Escalation）

当连续 3 轮同一门禁 FAIL 时，说明常规修复手段无效。此时：

1. **暂停修复**，不自欺欺人继续打补丁
2. **启动 Oracle 深度诊断**，分析根因：
   - 是设计缺陷导致修不好？（如：当前架构本身就不安全）
   - 是修复引入了新问题？（如：为了修 A 破坏了 B）
   - 是审查标准不合理？（如：过于理想化，与现有代码风格冲突）
3. **输出根因分析报告**，给出 2-3 个可选方案
4. **上报用户**，由用户决策下一步方向

### Phase 3: 输出报告

## 审查 Agent 提示词模板

Sisyphus 将「覆盖文件清单」和模块上下文代入以下模板：

### Agent 1: 代码正确性

```
task(subagent_type="oracle", load_skills=[], run_in_background=true,
  description="Review correctness of MODULE_NAME",
  prompt="""
<review_type>CODE CORRECTNESS + QUALITY REVIEW</review_type>
<module>{MODULE_NAME}</module>
<files>{NEWLINE_SEPARATED_FILE_LIST_WITH_FULL_CONTENT}</files>
<context>{MODULE_SPECIFIC_CONTEXT_FROM_USER}</context>

Review for: logic errors, concurrency issues, error handling gaps, data integrity risks,
nil pointer dereference potential, race conditions, and dead code.
OUTPUT: <verdict>PASS or FAIL</verdict> <findings>each with CRITICAL/MAJOR/MINOR</findings>
""")
```

### Agent 2: 安全 + 边界条件

```
task(subagent_type="oracle", load_skills=[], run_in_background=true,
  description="Review security of MODULE_NAME",
  prompt="""
<review_type>SECURITY + EDGE CASE REVIEW</review_type>
<module>{MODULE_NAME}</module>
<files>{FILE_LIST}</files>

Review for: input validation, injection risks, secrets exposure, DoS vectors,
edge cases (empty inputs, max-size, unicode, zero values).
OUTPUT: <verdict>PASS or FAIL</verdict> <findings>each with CRITICAL/HIGH/MEDIUM/LOW</findings>
""")
```

### Agent 3: 架构 + 模式

```
task(subagent_type="oracle", load_skills=[], run_in_background=true,
  description="Review architecture of MODULE_NAME",
  prompt="""
<review_type>ARCHITECTURE + PATTERN REVIEW</review_type>
<module>{MODULE_NAME}</module>
<files>{FILE_LIST}</files>

Review for: package structure, dependency direction, duplicated code,
over-engineering, dead code, constant organization, interface design.
OUTPUT: <verdict>PASS or FAIL</verdict> <findings>each with CRITICAL/MAJOR/MINOR</findings>
""")
```

## 修复 Agent 分流规则

| 问题规模 | Agent | 策略 |
|----------|-------|------|
| 单文件简单修改 | `category="quick"` | 逐一给出文件路径+行号+精确修改内容 |
| 多文件协调修改 | `category="quick"` 分批 | 按「修改的文件不重叠」原则并行 |
| 复杂逻辑重写 | `category="deep"` | 给出完整上下文和期望结果 |

## 退出条件

循环终止条件（按优先级）：

| 优先级 | 条件 | 说明 |
|--------|------|------|
| 1 | **全部质量门禁通过** | 正常退出 — 质量达标 |
| 2 | **用户手动终止** | `stop` / `终止` / `暂停` |
| 3 | **僵局经用户决策终止** | 上报后用户选择「接受现状」或「改方案」 |

**不存在「无新发现就自动停止」这条退路。** 只要门禁没全过，就继续循环。
只有质量达标、用户叫停、或用户决策接受现状这三种情况才能终止。

## 最终报告模板

```markdown
# {MODULE_NAME} 自评审报告

## 总览
- 模块: {MODULE_NAME}
- 发现模式: [包路径 / 功能模块探索 / 手动指定]
- 涉及包/服务: {PACKAGES / SERVICES}
- 轮次: {ROUNDS}
- 最终判定: PASS / FAIL
- 已修复: {FIXED_COUNT} 项
- 已知设计约束: {CONSTRAINT_COUNT} 项

## 已修复问题
| # | 严重度 | 描述 | 文件 | 修复方式 |
|---|--------|------|------|----------|

## 已知设计约束
| # | 描述 | 原因 |
|---|------|------|

## 验证
- build: {STATUS}
- test: {PASSED}/{TOTAL}
```
