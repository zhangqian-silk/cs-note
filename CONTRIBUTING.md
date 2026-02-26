# 贡献指南

感谢您对本仓库的贡献！请遵循以下规范。

## 格式规范

### 引号使用

| 文档语言 | 引号类型 | 示例 |
|---------|---------|------|
| 中文文档 | 中文引号 | 这是一个"示例"文本 |
| 英文文档 | 英文引号 | This is an "example" text |
| 代码字符串 | 英文引号 | `"value"` 或 `'value'` |

### 标点符号

- 中文文档使用中文标点：，。！？：；
- 英文文档使用英文标点：,.!?:;
- 混合内容中，主体语言决定标点类型

## 编辑规范

### 最小化变更原则

- 只修改必要的部分
- 不要"顺手"格式化整个文件
- 不要批量替换引号类型
- 保持原文的整体风格和术语

### Markdown 格式

- 标题层级不超过 4 级（H1-H4）
- 列表项之间不留空行
- 表格使用左对齐
- 中文与英文/数字之间可加空格（保持原文风格）

### 文件命名

- 使用小写字母和连字符：`rule-engine.md`
- 中文文件名允许，但推荐使用英文

## 提交规范

使用 [Conventional Commits](https://www.conventionalcommits.org/) 格式：

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

### 类型

| 类型 | 说明 |
|------|------|
| `feat` | 新功能 |
| `fix` | 修复 bug |
| `docs` | 文档更新 |
| `refactor` | 重构（不增加功能、不修复 bug） |
| `style` | 格式调整（不影响逻辑） |
| `test` | 测试相关 |
| `chore` | 构建/工具相关 |

### 示例

```
docs(marketing): 补充规则引擎安全与可观测性内容

- 新增开源方案对比
- 新增集成边界说明
- 新增监控指标定义
```

## AI 辅助贡献

如果您使用 AI 工具（Claude、Cursor、Copilot 等）辅助贡献，请确保：

1. AI 已读取仓库中的规则文件（`.clauderules`、`.cursorrules`、`.github/copilot-instructions.md`）
2. 遵循上述格式规范
3. 审查 AI 生成的代码/文档，确保符合项目风格

## 参考

- [中文文案排版指北](https://github.com/sparanoid/chinese-copywriting-guidelines)
- [Conventional Commits](https://www.conventionalcommits.org/)