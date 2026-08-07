# 可视化伴侣使用指南

浏览器里的可视化头脑风暴伴侣，用于展示原型、图表、视觉选项对比。轻量自包含版——无需独立服务器。

## 何时用（逐问题判断）

核心判据：**用户看图理解是不是比读文字更好？**

**用浏览器**（内容本身是视觉的）：
- UI 原型 / 线框 / 布局 / 导航结构
- 架构图 / 数据流 / 组件关系图
- 并排视觉对比（两种布局、两套配色、两个设计方向）
- 空间关系（状态机、流程图、ER 图）

**用终端**（内容是文字或表格的）：
- 需求/范围问题
- 概念性 A/B/C 选择
- 优缺点列表、对比表
- 技术决策、API 设计、数据建模
- 任何答案是用语言表达的澄清问题

**关于 UI 的话题不自动等于视觉问题。** "你想要什么样的向导？"是概念题 → 终端。"这两种向导布局哪个顺眼？"是视觉题 → 浏览器。

## 如何提供（按需，just-in-time）

**不要一上来就提供。** 等到第一次出现"看图比读文字更清楚"的问题时，再单独用一条消息提出：

> "接下来这部分用图给你看可能更直观——我可以在浏览器里画原型、图表、选项对比给你选。要不要开启？我画好会自动帮你打开。"

**这条提议必须独占一条消息**——只说提议，不带任何澄清问题或总结。等用户回应。同意就画第一张图；拒绝就继续纯文字，之后除非用户主动提，不再重复提议。

## 原理（轻量版）

无需独立服务器。流程：把自包含 HTML（含内联主题样式）写到 `docs/brainstorms/.visual/`，用默认浏览器打开。用户在浏览器里看图、点选，然后在终端回复你。

建议把 `docs/brainstorms/.visual/` 加入 `.gitignore`（这些是临时可视化产物，不进版本库）。

## 创建一屏内容

写一个自包含 HTML 文件（开头 `<!DOCTYPE html>` + 内联 `<style>` 主题），保存到 `docs/brainstorms/.visual/<语义名>.html`：

- 文件名用语义命名：`layout.html`、`visual-style.html`、`architecture.html`
- 每一屏用新文件名，不要复用；迭代加版本后缀：`layout-v2.html`
- 用 write 工具写文件，不要用 cat/heredoc（会把噪声倒进终端）

写完用 PowerShell 打开（用绝对路径更稳）：

```powershell
Invoke-Item "D:\项目根目录\docs\brainstorms\.visual\layout.html"
```

`Invoke-Item` 用默认浏览器打开文件。每次换屏都要重新打开新文件（或让用户刷新）。

## 循环

1. 写 HTML 到 `docs/brainstorms/.visual/` 的新文件
2. 用 `Invoke-Item` 打开，并告诉用户：
   - 屏上大概是什么（"展示了首页 3 种布局方案"）
   - 请在终端回复你的看法
3. 用户在终端回复后，根据反馈：
   - 要改当前屏 → 写新版本文件（如 `layout-v2.html`）并重新打开
   - 通过了 → 进入下一问题
4. 回到纯文字时，写一个"等待"屏清掉旧内容，避免用户盯着已决定的选项：

```html
<div style="display:flex;align-items:center;justify-content:center;min-height:60vh">
  <p class="subtitle">继续在终端交流…</p>
</div>
```

5. 重复直到结束。

## 内联主题（直接复用）

每个 HTML 文件开头粘贴下面的主题，内容区用下列 class。

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>布局对比</title>
<style>
  :root{
    --bg:#0f1115; --panel:#181b22; --ink:#e6e9ef; --muted:#9aa3b2;
    --accent:#6ea8fe; --accent2:#8b5cf6; --line:#2a2f3a;
    --ok:#34d399; --bad:#f87171;
  }
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--ink);
    font-family:-apple-system,"Segoe UI",system-ui,"PingFang SC","Microsoft YaHei",sans-serif;
    line-height:1.6;padding:32px;max-width:980px;margin:0 auto}
  h2{font-size:1.5rem;margin:.2em 0 .1em}
  h3{margin:0 0 .2em}
  p{margin:.2em 0}
  .subtitle{color:var(--muted);margin-top:0}
  .section{margin-bottom:28px}
  .label{text-transform:uppercase;letter-spacing:.08em;font-size:.72rem;color:var(--muted)}
  .options{display:grid;gap:14px;margin-top:16px}
  .option{display:flex;gap:14px;align-items:flex-start;background:var(--panel);
    border:1px solid var(--line);border-radius:12px;padding:16px;cursor:pointer;
    transition:.15s}
  .option:hover{border-color:var(--accent)}
  .option.selected{border-color:var(--accent);box-shadow:0 0 0 2px rgba(110,168,254,.3) inset}
  .option .letter{flex:0 0 32px;height:32px;border-radius:8px;display:grid;place-items:center;
    font-weight:700;background:var(--accent2);color:#fff}
  .option .content{flex:1}
  .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px;margin-top:16px}
  .card{background:var(--panel);border:1px solid var(--line);border-radius:14px;overflow:hidden;
    cursor:pointer;transition:.15s}
  .card:hover{border-color:var(--accent)}
  .card.selected{border-color:var(--accent);box-shadow:0 0 0 2px rgba(110,168,254,.3) inset}
  .card-image{height:130px;display:grid;place-items:center;
    background:repeating-linear-gradient(45deg,#22262f,#22262f 10px,#262b35 10px,#262b35 20px);color:var(--muted);font-size:.8rem}
  .card-body{padding:14px}
  .mockup{background:var(--panel);border:1px solid var(--line);border-radius:12px;overflow:hidden;margin-top:16px}
  .mockup-header{padding:8px 14px;font-size:.78rem;color:var(--muted);
    border-bottom:1px solid var(--line);background:#14171d}
  .mockup-body{padding:18px}
  .split{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px}
  .pros-cons{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px}
  .pros,.cons{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px}
  .pros h4{color:var(--ok);margin:0 0 .4em}.cons h4{color:var(--bad);margin:0 0 .4em}
  .pros ul,.cons ul{margin:0;padding-left:1.2em}
  .mock-nav{background:#14171d;border:1px solid var(--line);border-radius:8px;padding:10px 14px;color:var(--muted);margin-bottom:12px}
  .mock-sidebar{background:#14171d;border:1px solid var(--line);border-radius:8px;padding:12px;color:var(--muted);min-height:120px}
  .mock-content{background:#14171d;border:1px solid var(--line);border-radius:8px;padding:12px;color:var(--muted);min-height:120px;flex:1}
  .mock-button{display:inline-block;background:var(--accent);color:#0f1115;font-weight:600;
    border:none;border-radius:8px;padding:8px 16px;margin:4px 4px 0 0;cursor:default}
  .mock-input{background:#14171d;border:1px solid var(--line);border-radius:8px;color:var(--muted);
    padding:8px 12px;margin:4px 4px 0 0}
  .placeholder{background:repeating-linear-gradient(45deg,#22262f,#22262f 10px,#262b35 10px,#262b35 20px);
    border-radius:8px;color:var(--muted);padding:20px;text-align:center}
</style>
</head>
<body>
  <h2>哪种布局更合适？</h2>
  <p class="subtitle">考虑可读性和视觉层次</p>
  <div class="options">
    <div class="option" onclick="this.classList.toggle('selected')">
      <div class="letter">A</div>
      <div class="content"><h3>单列</h3><p>干净、专注的阅读体验</p></div>
    </div>
    <div class="option" onclick="this.classList.toggle('selected')">
      <div class="letter">B</div>
      <div class="content"><h3>双列</h3><p>侧边导航 + 主内容区</p></div>
    </div>
  </div>
</body>
</html>
```

### 可用 class 速查

| 用途 | class |
|------|-------|
| A/B/C 选项（点击 `.selected` 高亮） | `.options` > `.option`（含 `.letter` `.content`） |
| 视觉设计卡 | `.cards` > `.card`（含 `.card-image` `.card-body`） |
| 原型容器 | `.mockup`（含 `.mockup-header` `.mockup-body`） |
| 并排左右 | `.split` |
| 优缺点 | `.pros-cons` > `.pros` / `.cons` |
| 线框积木 | `.mock-nav` `.mock-sidebar` `.mock-content` `.mock-button` `.mock-input` `.placeholder` |
| 排版 | `.subtitle` `.section` `.label` |

## 设计建议

- **保真度匹配问题**：布局问题用线框，观感问题才抛光
- **每屏顶部说清问题**："哪种布局更专业？"而不是干巴巴"选一个"
- **迭代优先于推进**：反馈要改当前屏就改，别急着跳下一步
- **每屏最多 2-4 个选项**
- **该用真实内容就用真实内容**：占位内容会掩盖设计问题
