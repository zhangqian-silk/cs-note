# AI 编辑规范

本文件是 AI 工具编辑本仓库的统一规则源文件。

## 格式规范

| 规则 | 说明 |
|------|------|
| 中文引号 | 中文文档使用「」或""，不转换为英文引号 |
| 英文引号 | 英文文档使用 "" |
| 代码字符串 | 统一使用英文引号 |
| 中文标点 | ，。！？：； |
| 英文标点 | ,.!?:; |

## 编辑规范

- **最小化变更**：只修改必要的部分，不格式化整个文件
- **保持原文风格**：不改整体风格和术语
- **保留历史痕迹**：不删除作者、日期等 front matter

## Markdown 规范

- 缩进使用**制表符**
- 无序列表使用 `-` 而不是 `*`
- 标题和其他元素间保持空行
- 主要章节（一级标题）之间使用水平分割线 `---`
- 语言风格偏向专业、学术

## 代码规范

- 代码实现默认使用 **Golang**

## 伪代码规范

使用《算法导论》风格：

```
ALGORITHM-NAME(arg1, arg2, ...)
    // 注释说明
    if condition then
        statement1
        statement2
    else
        statement3
    end if
    
    for i ← 1 to n do
        statement
    end for
    
    while condition do
        statement
    end while
    
    return result
```

**关键符号**：
- 赋值：`←`
- 注释：`//` 单行
- 块结束：`end if` / `end for` / `end while` / `end function`
- 数组索引：`A[i]` 或 `A[1..n]`
- 数学运算：`⌊⌋` `⌈⌉` `mod` `and` `or` `not`

## 提交规范

Conventional Commits: `<type>(<scope>): <description>`

类型: `feat` `fix` `docs` `refactor` `style` `test` `chore`
