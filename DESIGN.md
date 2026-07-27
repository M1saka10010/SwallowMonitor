# DESIGN.md — SwallowMonitor

## 1. Objective

SwallowMonitor 应表现为一件安静、可信、长期运行的监控仪器：用户一眼能确认哪些主机异常，进一步查看时能准确读取趋势，不被装饰干扰。质量标准不是“现代后台模板”，而是所有间距、数字、分隔线和状态颜色都像经过校准。

## 2. Product Context

- **产品作用：** 在一个 Go 单二进制中展示主机实时状态、资源趋势和管理配置。
- **主要用户：** 同时管理少量到数十台服务器，需要快速确认在线状态、资源压力和通知配置的个人运维者或小团队。
- **相邻参考：** Linear 的信息层级、Braun 仪器面板的克制、瑞士国际主义的网格与排版。
- **远离参考：** Grafana 默认仪表盘；它的视觉通道和面板密度服务于大型可配置观测平台，不适合本产品的聚焦范围。
- **文化语域：** 技术、准确、低调，不拟人化，不营销化。

## 3. Visual Foundations

### 3a. Color

- **浅色中性色：** canvas `#F4F4F1`、surface `#FFFFFF`、surface-muted `#ECECE8`、text `#171714`、text-muted `#696963`、border `#D7D7D1`、border-strong `#ADADA5`。
- **深色中性色：** canvas `#11110F`、surface `#181816`、surface-muted `#20201D`、text `#F1F1EC`、text-muted `#A4A49C`、border `#32322E`、border-strong `#55554E`。
- **强调色：** 浅色 `#2457D6`，深色 `#6F91F2`。
- **语义色：** 浅色 success `#267A50`、warning `#9A6500`、danger `#BE3B34`、offline `#85857E`；深色 success `#55A97A`、warning `#D0A04A`、danger `#E06A62`、offline `#77776F`。
- **图表色：** blue `#3478D4`、green `#398760`、orange `#B56A28`、red `#C64B45`、gray `#85857E`。
- **使用规则：** 蓝色只表示选择、链接、焦点与主要数据系列；绿色只表示在线或健康；红色只表示错误、删除与高风险操作；离线使用灰色；标签保持中性；不使用渐变。

### 3b. Typography

- **标题与正文：** `"Helvetica Neue", Arial, "Noto Sans SC", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif`。
- **数据：** `"SFMono-Regular", "Cascadia Code", "Roboto Mono", Consolas, monospace`。
- **字号：** `11 / 12 / 14 / 16 / 20 / 28 / 36`。
- **字重纪律：** 只使用 400、500、600；页面标题 600，区块标题 600，正文 400，控件可用 500。实时数字使用等宽字体与 tabular nums。

### 3c. Spacing & rhythm

- **基础单位：** 4px。
- **间距：** `4 / 8 / 12 / 16 / 24 / 32 / 48 / 64`。
- **页面留白：** 桌面水平 32px，移动端 16px；内容最大宽度约 1440px；主要区块间距 32px。
- **交互目标：** 至少 44×44px；表格行高 44–48px。

### 3d. Component seeds

- **按钮：** primary、secondary、ghost、danger 四种；每屏最多一个主要实心操作。
- **容器：** 4–6px 圆角、1px 边框、默认无阴影，不使用 16px 圆角卡片海洋。
- **状态：** 状态点必须配文字；主机卡片顶部使用 2px 状态校准线，这是产品的签名元素。
- **图标：** 首选文字动作；图标必须有可访问名称或 tooltip，不使用装饰性图标。

## 4. Accessibility

- 正文对背景至少 4.5:1，UI 边界与大文本至少 3:1。
- `focus-visible` 使用 2px accent 外环和 2px canvas 间隔。
- 在线、离线、成功、失败均提供文字，不只依赖颜色。
- 表单错误与保存反馈使用 `aria-live="polite"`。
- 图表提供包含指标和时间范围的可访问名称。
- Dialog 支持焦点圈定、关闭后焦点恢复、Escape 与遮罩关闭。
- 动效仅限 120–180ms 的颜色、边框和透明度过渡；`prefers-reduced-motion` 下取消。

## 5. Voice & Tone

- **语域：** 简洁、技术、直接。
- **句式：** 短句，动作优先。
- **使用词：** “在线”“离线”“保存设置”“重新加载”“复制命令”。
- **拒绝词：** “无缝”“赋能”“体验升级”“探索”“智能洞察”。
- **称呼：** 不主动称呼用户，控件直接描述动作。
- **失败文案：** 同时说明发生了什么和下一步，例如“主机列表加载失败。检查连接后重试”。

## 6. Implementation Practices

- 设计 token 以 CSS variables 定义，并映射到 Tailwind CSS 4 theme。
- `frontend/src/styles.css` 只放 Tailwind 入口、token、主题和必要的基础设施规则；组件业务样式全部使用 Tailwind utility class。
- 不使用 CSS Modules、styled-components、静态 inline style；动态进度宽度和第三方 canvas 等运行时计算可使用动态 style。
- 使用响应式网格：主机卡片宽屏三列、中屏两列、窄屏一列。
- 不依赖外部字体、图片或 CDN；Chart.js 打入生产资源。
- 动画不使用弹簧、弹跳或大幅位移。

## 7. Anti-Patterns

- **无渐变、光晕或玻璃拟态。** 它们会削弱监控仪器的可信感。
- **无圆角阴影卡片海洋。** 层级由排版、细线和留白承担。
- **无 KPI 卡片行。** 主机状态才是概览的首要信息。
- **无随机彩色标签。** 标签表示分类，不表示状态。
- **无 emoji 或装饰性插图。** 产品的信心来自信息准确，不来自装饰。
- **无双 y 轴图表。** 避免误导不同单位的量级比较。
- **不隐藏非理想状态。** loading、empty、error、offline、unauthorized 都必须有明确设计。

## 8. Decision-Making

1. **准确性与可访问性优先。** 它们不为视觉特色让步。
2. **高频状态优先。** 概览信息优先于管理入口和站点描述。
3. **位置和长度优先编码数据。** 颜色只补充语义。
4. **熟悉的后台模式优先。** CRUD 效率高于新奇交互。
5. **克制优先。** 元素争夺注意力时削弱次要元素，不继续增加强调色。
6. **线性实现优先。** 只有真实复用至少三次才建立新抽象。

## 9. Workflow

1. 核对现有 API、SSE、OAuth 与安装命令协议。
2. 先用纯函数测试锁定格式化、范围和离线区间语义。
3. 建立 token、应用壳与 Hash 路由。
4. 迁移概览，再迁移详情图表。
5. 按主机、标签、设置、通知顺序迁移后台。
6. 检查 loading、empty、error、offline、unauthorized 与移动端状态。
7. 执行反模板、无障碍、主题和生产资源检查。
