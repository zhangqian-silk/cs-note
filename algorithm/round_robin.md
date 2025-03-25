# Round Robin

## Weighted Round Robin

### 平滑加权轮询算法

通过动态调整当前权重，实现均匀分配。每个节点维护三个参数：

- **固定权重（Weight）**：初始配置的优先级。
- **有效权重（Effective Weight）**：动态调整后的权重（初始等于固定权重）。
- **当前权重（Current Weight）**：实时计算的临时权重。

**步骤说明**

1. **初始化权重**：所有节点的当前权重（Current Weight）初始为 0
2. **选择节点**：每次选择 **当前权重最大** 的节点（若多个节点权重相同，按顺序选择）
3. **调整当前权重**：将选中节点的当前权重减去所有节点的 **总有效权重之和**，其余节点保持当前权重不变
4. **恢复有效权重**：下一轮开始时，所有节点的当前权重增加其有效权重值，重复步骤 2~4

**示例演示**

假设三个节点 **A(5)、B(3)、C(2)**，总有效权重为 \( 5+3+2=10 \)。

| 轮询次数 | 调整前当前权重（A, B, C） | 选中节点 | 调整后当前权重（A, B, C）        |
|----------|---------------------------|----------|----------------------------------|
| 1        | (5, 3, 2) → A=5 最大      | A        | A:5-10=**-5**；B:3；C:2 → (-5,3,2) |
| 2        | (-5+5=0, 3+3=6, 2+2=4)   | B        | B:6-10=**-4**；A:0；C:4 → (0,-4,4)  |
| 3        | (0+5=5, -4+3=-1, 4+2=6)  | C        | C:6-10=**-4**；A:5；B:-1 → (5,-1,-4)|
| 4        | (5+5=10, -1+3=2, -4+2=-2)| A        | A:10-10=**0**；B:2；C:-2 → (0,2,-2) |
| 5        | (0+5=5, 2+3=5, -2+2=0)   | A 或 B   | 选中 A 或 B，权重减 10 → 后续轮询重复 |

**最终轮询顺序**：A → B → C → A → B → A → C → B → A → C  
（符合 5:3:2 比例且分布均匀）

**伪代码实现**

```python
class WeightedRoundRobin:
    def __init__(self, nodes):
        self.nodes = [
            {'name': name, 'weight': weight, 'current': 0}
            for name, weight in nodes.items()
        ]
        self.total_weight = sum(node['weight'] for node in self.nodes)

    def next(self):
        # 选择当前权重最大的节点
        selected = max(self.nodes, key=lambda x: x['current'])
        # 调整当前权重
        selected['current'] -= self.total_weight
        # 恢复有效权重
        for node in self.nodes:
            node['current'] += node['weight']
        return selected['name']
```
