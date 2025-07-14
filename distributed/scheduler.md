# Scheduler

## Cron

支持 Cron 表达式解析，并支持计算下次运行时间。

**结构体定义**

- 用位运算表示 cron 解析后的信息
- 对于 `DayOfMonth` 与 `DayOfWeek`，用最高位表示忽略，即 `?` 符号

```go
// CronSchedule 代表解析后的 cron 表达式 (使用位运算)
type CronSchedule struct {
    Minute     uint64 // Bit 0 for minute 0, bit 59 for minute 59
    Hour       uint64 // Bit 0 for hour 0, bit 23 for hour 23
    DayOfMonth uint64 // Bit 1 for day 1, bit 31 for day 31 (bit 0 unused), bit 63 for ignoring day of month
    Month      uint64 // Bit 1 for month 1, bit 12 for month 12 (bit 0 unused)
    DayOfWeek  uint64 // Bit 0 for Sunday (time.Sunday), bit 6 for Saturday (time.Saturday), bit 63 for ignoring day of week
}
```

**数据解析**

- 注意表达式的合法性，如支持 5 个字段：`"Minute Hour DayOfMonth Month DayOfWeek"`
- 支持 `*`、`,` 和 `?` 符号

**时间计算**

- 根据当前时间，计算 cron 下一次满足条件的时间点
- 按照 `Month`、`DayOfWeek`、`DayOfWeek`、`Hour`、`Minute` 的顺序依次校验
- 不满足时，递增至下一个对应的时间段起点，如 `Month` 不满足时，递增至下个月的 1 号零点
- 一年的迭代次数，约为 (12-1) 个月 + (31-1) 天 + (24-1) 小时 + (60-1) 分钟 + 1 次匹配，即约为 124 次，最大迭代次数以此为准

```go
// Next calculates the next run time after 'fromTime'.
func (s *CronSchedule) Next(fromTime time.Time) time.Time {
    t := fromTime.Truncate(time.Minute).Add(time.Minute)
    const maxIterations = 124 * 10

    for i := 0; i < maxIterations; i++ {
        if !isSet(s.Month, int(t.Month())) {
            t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
            continue
        }
        ...
        // All conditions met. This is the next run time.
        return t
    }
    ...
}
```

## Task

用于调度的最小单位，支持差异化配置。

**结构体定义**

- `Task` 定义了调度任务的必要元素
- `TaskOptions` 支持使用者自定义进行配置

```go
type JobFunc func(ctx context.Context) error

type TaskOptions struct {
    OnlyWorkday       bool          // 只在中国区工作日运行
    DistributedKey    string        // 分布式锁的键，有值时会使用分布式锁
    DistributedExpire time.Duration // 分布式锁的过期时间，默认为 1 分钟
}

// Task 代表一个调度任务
type Task struct {
    Name       string
    Job        JobFunc
    Options    TaskOptions   // 额外调度选项
    parsedCron *CronSchedule // 解析后的 Cron 表达式
}
```

**功能方法**

- `calculateExecTime` 计算下次运行时间，支持特殊配置，如仅在工作日运行

```go
// calculateExecTime 计算并更新任务的下次运行时间
func (t *Task) calculateExecTime(fromTime time.Time) (time.Time, error) {
    ...
    for range 100 {
        ...
        if t.Options.OnlyWorkday {
            if !IsWorkday(next) {
                next = t.parsedCron.Next(next)
                flag = true
            }
        }
        ...
    }
    ...
}
```

## Scheduler

调度中心，在 `runLoop` 中执行定时扫描任务，逐时间片判断各任务是否到达执行时间。

**结构体定义**

- `tasks`：通过 `map` 管理注入的任务实例
- `timeSlice`：`runLoop` 执行的时间片
- `failedCounter`：失败次数计数器，用于处理任务多次失败的场景
- `wg` & `cancel`：控制调度器的主动取消逻辑
- `mu` & `mainLock`：并发控制
- `redisHelper`：redis 实例，中心化存储

```go
type Scheduler struct {
    ctx           context.Context 
    cancel        context.CancelFunc
    tasks         map[string]*Task
    timeSlice     time.Duration
    failedCounter map[string]int64

    redisHelper *RedisHelper
    mainLock    *DistributedLock
    wg          sync.WaitGroup
    mu          sync.Mutex
}
```

**Run Loop**

- 第一个定时器用于对齐运行时间，并首次触发扫描任务

```go
func (s *Scheduler) runLoop() {
    ...
    now := time.Now()
    // 额外增加 10ms 的延迟，确保在时间片的边界上开始扫描任务
    nextTickTime := now.Truncate(s.timeSlice).Add(s.timeSlice).Add(10 * time.Millisecond)
    firstTickTimer := time.NewTimer(nextTickTime.Sub(now))

    select {
    case <-s.ctx.Done(): // 上下文取消
        firstTickTimer.Stop()
        return
    case firstFireTime := <-firstTickTimer.C:
        firstTickTimer.Stop()
        ticker = time.NewTicker(s.timeSlice)
        defer ticker.Stop()

        s.scanAndRunTasks(firstFireTime)
    }
    ...
}
```

- 第二个定时器用于逐时间片触发扫描任务

```go
func (s *Scheduler) runLoop() {
    ...
    for {
        select {
        case <-s.ctx.Done(): // 上下文取消
            return
        case fireTime := <-ticker.C:
            // 随机延迟，避免始终由同一实例率先获取锁
            // 控制延迟时间在 1s 内
            time.Sleep(time.Duration(rand.IntN(980)) * time.Millisecond)
            s.scanAndRunTasks(fireTime)
        }
    }
}
```

**任务注入**

- 添加任务时，初始化任务的运行时间，存储在 redis 中

```go
func (s *Scheduler) addTask(task *Task) error {
    ...
    s.tasks[task.Name] = task

    // 计算初始运行时间
    return s.initTaskExecTime(task)
}
```

**扫描任务**

- 通过 `mainLock` 避免多实例同时执行扫描任务
  - 锁的有效期要略大于时间片
  - 锁的持有时间不能过短，避免其他调度器加锁成功（每个调度器执行时有毫米级别的差异）

```go
func (s *Scheduler) scanAndRunTasks(currentTime time.Time) {
    fmt.Printf("[%s] Scheduler tick: Scanning tasks...\n", currentTime.Format(time.DateTime))

    err := s.mainLock.Lock()
    if err != nil {
        fmt.Printf("Failed to acquire scheduler leader lock. Err info: %v\n", err)
        return
    }

    lockBeginTime := time.Now()
    defer func() {
        lockDuration := time.Since(lockBeginTime)
        if lockDuration < s.timeSlice/2 {
            time.Sleep(s.timeSlice/2 - lockDuration)
        }
        s.mainLock.Unlock()
    }()

    fmt.Printf("Acquired scheduler leader lock. Proceeding with task scan.\n")
    ...
}
```

- 拷贝任务列表，防止并发修改

```go
func (s *Scheduler) scanAndRunTasks(currentTime time.Time) {
    ...
    // 防止并发修改任务列表
    s.mu.Lock()
    taskToScan := make([]*Task, 0, len(s.tasks))
    for _, task := range s.tasks {
        taskToScan = append(taskToScan, task)
    }
    s.mu.Unlock()
    ...
}
```

- 遍历任务列表，判断是否到达执行时间

```go
func (s *Scheduler) scanAndRunTasks(currentTime time.Time) {
    ...
    for _, task := range taskToScan {
        execTime, err := s.redisHelper.GetTime(task.execTimeKey())
        if err != nil {
            ...
            continue
        }

        if execTime.IsZero() {
            ...
            continue
        }

        if currentTime.After(execTime) {
            ...
            go s.executeTask(task, execTime)
        }
    }
}
```

**执行任务**

- 校验分布式锁

```go
func (s *Scheduler) executeTask(t *Task, execTime time.Time) {
    ...
    if t.Options.DistributedKey != "" {
        // 需要分布式锁
        expire := time.Minute
        if t.Options.DistributedExpire > 0 {
            expire = t.Options.DistributedExpire
        }

        lock := NewDistributedLock(t.Options.DistributedKey, expire)
        err := lock.Lock()
        if err != nil {
            fmt.Printf("Task '%s' skipped: could not acquire distributed lock for key '%s'. Err info: %v\n", t.Name, t.Options.DistributedKey, err)
            return // 获取并发锁失败，不执行
        }
        defer lock.Unlock()

        fmt.Printf("Task '%s' acquired distributed lock '%s'. Executing...\n", t.Name, t.Options.DistributedKey)
    } else {
        fmt.Printf("Task '%s' executing (no distributed lock)...\n", t.Name)
    }
    ...
}
```

- 通过 CAS 进行并发保障

```go
func (s *Scheduler) executeTask(t *Task, execTime time.Time) {
    ...
    // 并发保障，通过 cas 更新下次执行时间，如果更新失败，说明任务已经被其他实例执行过
    execTime, err := t.calculateExecTime(execTime)
    if err != nil {
        fmt.Printf("Calculate next exec time for task '%s' failed. Err info: %v\n", t.Name, err)
        return
    }

    res, err := s.redisHelper.CASForTime(t.execTimeKey(), execTime, execTime, time.Until(execTime)*2)
    if err != nil {
        fmt.Printf("Update next exec time for task '%s' failed. Err info: %v\n", t.Name, err)
        return
    }
    if !res {
        fmt.Printf("Task '%s' skipped: next exec time has changed since last scan\n", t.Name)
        return
    }
    ...
}
```

- 执行具体任务

```go
func (s *Scheduler) executeTask(t *Task, execTime time.Time) {
    ...
    beginTime := time.Now()
    err = t.Job(s.ctx)
    if err != nil {
        fmt.Printf("Execute task '%s' failed. Err info: %v\n", t.Name, err)
    } else {
        fmt.Printf("Task '%s' executed successfully. Execution time:%s\n", t.Name, time.Since(beginTime).String())
    }
}
```

## 代码附录

### Cron

```go
// CronSchedule 代表解析后的 cron 表达式 (使用位运算)
type CronSchedule struct {
    Minute     uint64 // Bit 0 for minute 0, bit 59 for minute 59
    Hour       uint64 // Bit 0 for hour 0, bit 23 for hour 23
    DayOfMonth uint64 // Bit 1 for day 1, bit 31 for day 31 (bit 0 unused), bit 63 for ignoring day of month
    Month      uint64 // Bit 1 for month 1, bit 12 for month 12 (bit 0 unused)
    DayOfWeek  uint64 // Bit 0 for Sunday (time.Sunday), bit 6 for Saturday (time.Saturday), bit 63 for ignoring day of week
}

type fieldType int

const (
    fieldMinute fieldType = iota
    fieldHour
    fieldDayOfMonth
    fieldMonth
    fieldDayOfWeek
)

type parser struct {
    cron *CronSchedule
    err  error
}

func newParser() *parser {
    return &parser{
        cron: &CronSchedule{},
    }
}

func (p *parser) parseField(field string, t fieldType) *parser {
    if p.err != nil {
        return p
    }

    parsed, err := parseCronField(field, t)
    if err != nil {
        p.err = err
        return p
    }

    switch t {
    case fieldMinute:
        p.cron.Minute = parsed
    case fieldHour:
        p.cron.Hour = parsed
    case fieldDayOfMonth:
        p.cron.DayOfMonth = parsed
    case fieldMonth:
        p.cron.Month = parsed
    case fieldDayOfWeek:
        p.cron.DayOfWeek = parsed
    }
    return p
}

// parseCronField 解析 cron 表达式的单个字段，并将其转换为位掩码
// min 和 max 定义了该字段允许的数字范围。
func parseCronField(field string, t fieldType) (uint64, error) {
    fullMaskRes := fullMask(t)
    if field == "*" {
        return fullMaskRes, nil
    }

    if field == "?" {
        if t == fieldDayOfMonth || t == fieldDayOfWeek {
            return 1 << 63, nil
        }
        return 0, nil
    }

    parts := strings.Split(field, ",")
    if len(parts) == 0 { // Should not happen if field is not empty
        return 0, fmt.Errorf("empty cron field part: '%s'", field)
    }

    var res uint64 = 0
    parsedSomething := false
    for _, part := range parts {
        trimmedPart := strings.TrimSpace(part)
        if trimmedPart == "" {
            continue // Skip empty parts, e.g., "1,,2"
        }

        val, err := strconv.Atoi(trimmedPart)
        if err != nil {
            continue
        }
        if !isSet(fullMaskRes, val) {
            continue
        }

        res |= 1 << uint(val)
        parsedSomething = true
    }

    if !parsedSomething {
        return 0, fmt.Errorf("no valid values found in cron field: '%s'", field)
    }
    return res, nil
}

// ParseCron 解析 crontab 字符串
// 格式: "Minute Hour DayOfMonth Month DayOfWeek"
func ParseCron(cronSpec string) (*CronSchedule, error) {
    fields := strings.Fields(cronSpec)
    if len(fields) != 5 {
        return nil, fmt.Errorf("invalid cron spec: expected 5 fields, got %d for '%s'", len(fields), cronSpec)
    }

    p := newParser()
    for i, field := range fields {
        p.parseField(field, fieldType(i))
    }

    if p.err != nil {
        return nil, p.err
    }
    return p.cron, nil
}

// Next calculates the next run time after 'fromTime'.
func (s *CronSchedule) Next(fromTime time.Time) time.Time {
    // 重置初始时刻（精度为分钟）
    t := fromTime.Truncate(time.Minute).Add(time.Minute)

    // 覆盖一年的迭代次数，约为 (12-1) 个月 + (31-1) 天 + (24-1) 小时 + (60-1) 分钟 + 1 次匹配，即约为 124 次
    // 特殊情况下，如周和日期不匹配，或 2 月 29 日，可能会触发额外迭代
    // 最大迭代次数限制为 124 * 10，避免无限循环
    const maxIterations = 124 * 10

    for i := 0; i < maxIterations; i++ {
        // 按照 month、day、hour、minute 的顺序检查
        // 如果某个字段不匹配，将时间向前推进一个单位，并重置后续字段

        if !isSet(s.Month, int(t.Month())) {
            t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
            continue
        }

        if !isSet(s.DayOfWeek, int(t.Weekday())) && !isIgnore(s.DayOfWeek) {
            t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
            continue
        }

        if !isSet(s.DayOfMonth, t.Day()) && !isIgnore(s.DayOfMonth) {
            t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
            continue
        }

        if !isSet(s.Hour, t.Hour()) {
            t = t.Truncate(time.Hour).Add(time.Hour)
            continue
        }

        if !isSet(s.Minute, t.Minute()) {
            t = t.Add(time.Minute)
            continue
        }

        // All conditions met. This is the next run time.
        return t
    }

    fmt.Printf("Warning: Could not find next run time for schedule within %d iterations from %s\n", maxIterations, fromTime.String())
    return time.Time{} // Return zero time to indicate failure
}

func fullMask(t fieldType) uint64 {
    switch t {
    case fieldMinute:
        return (1 << 60) - 1 // 0～59 bits set
    case fieldHour:
        return (1 << 24) - 1 // 0～23 bits set
    case fieldDayOfMonth:
        return (1 << 32) - 2 // 1~31 bits set
    case fieldMonth:
        return (1 << 13) - 2 // 1～12 bits set
    case fieldDayOfWeek:
        return (1 << 7) - 1 // 0～6 bits set
    default:
        return 0
    }
}

// isSet 辅助函数，检查特定值在位掩码中是否被设置
func isSet(mask uint64, val int) bool {
    if val < 0 || val >= 64 { // Safety check, as we shift by uint(val)
        return false // Values outside the representable bit range are considered not set
    }
    return (mask & (1 << uint(val))) != 0
}

// isIgnore 辅助函数，检查第 63 位是否被设置，以确定是否忽略该字段
func isIgnore(mask uint64) bool {
    return isSet(mask, 63)
}
```

### Task

```go
type JobFunc func(ctx context.Context) error

type TaskOptions struct {
    OnlyWorkday       bool          // 只在中国区工作日运行
    DistributedKey    string        // 分布式锁的键，有值时会使用分布式锁
    DistributedExpire time.Duration // 分布式锁的过期时间，默认为 1 分钟
}

type Task struct {
    Name       string
    Job        JobFunc
    Options    TaskOptions   // 额外调度选项
    parsedCron *CronSchedule // 解析后的 Cron 表达式
}

func NewTask(name, cronSpec string, job JobFunc, opts TaskOptions) (*Task, error) {
    if name == "" {
        return nil, fmt.Errorf("task name cannot be empty")
    }
    if cronSpec == "" {
        return nil, fmt.Errorf("task cronSpec cannot be empty")
    }
    if job == nil {
        return nil, fmt.Errorf("task job function cannot be nil")
    }

    parsed, err := ParseCron(cronSpec)
    if err != nil {
        return nil, fmt.Errorf("failed to parse cron spec for task '%s': %w", name, err)
    }
    return &Task{
        Name:       name,
        Job:        job,
        Options:    opts,
        parsedCron: parsed,
    }, nil
}

func (t *Task) calculateExecTime(fromTime time.Time) (time.Time, error) {
    execTime := t.parsedCron.Next(fromTime)
    if execTime.IsZero() {
        return time.Time{}, fmt.Errorf("calculate exec time failed")
    }

    // 应用额外调度选项
    var flag bool
    for range 100 {
        flag = false

        if t.Options.OnlyWorkday {
            if !IsWorkday(execTime) {
                execTime = t.parsedCron.Next(execTime)
                flag = true
            }
        }

        // 异常情况处理，或没有额外改动，直接结束
        if execTime.IsZero() || !flag {
            break
        }
    }

    if execTime.IsZero() {
        return time.Time{}, fmt.Errorf("calculate exec time failed")
    }
    return execTime, nil
}

func (t *Task) execTimeKey() string {
    return fmt.Sprintf("task_%s_exec_time", t.Name)
}

func IsWorkday(t time.Time) bool {
    weekday := t.Weekday()
    return weekday != time.Saturday && weekday != time.Sunday
}
```

### Scheduler

```go
type DistributedLock struct {
}

func NewDistributedLock(key string, expire time.Duration) *DistributedLock {
    return &DistributedLock{}
}

func (s *DistributedLock) Lock() error {
    return nil
}

func (s *DistributedLock) Unlock() {
}

type RedisHelper struct {
}

func NewRedisHelper() *RedisHelper {
    return &RedisHelper{}
}

func (h *RedisHelper) SetTimeNX(key string, value time.Time, expire time.Duration) error {
    return nil
}

func (h *RedisHelper) GetTime(key string) (time.Time, error) {
    return time.Time{}, nil
}

func (h *RedisHelper) CASForTime(key string, oldValue, newValue time.Time, expire time.Duration) (bool, error) {
    return false, nil
}

// Scheduler 分布式调度器
type Scheduler struct {
    ctx           context.Context
    cancel        context.CancelFunc
    tasks         map[string]*Task
    timeSlice     time.Duration
    failedCounter map[string]int64

    redisHelper *RedisHelper
    mainLock    *DistributedLock
    wg          sync.WaitGroup
    mu          sync.Mutex
}

var (
    scheduler *Scheduler
    once      sync.Once
)

const (
    timeSlice   = time.Minute
    mainLockKey = "scheduler_control"
)

func Init(ctx context.Context) {
    once.Do(func() {
        ctx, cancel := context.WithCancel(ctx)
        scheduler = &Scheduler{
            ctx:           ctx,
            cancel:        cancel,
            tasks:         make(map[string]*Task),
            timeSlice:     timeSlice,
            failedCounter: make(map[string]int64),
            mainLock:      NewDistributedLock(mainLockKey, time.Minute),
            redisHelper:   NewRedisHelper(),
        }
        scheduler.start()
    })
}

func AddTask(task *Task) error {
    if scheduler == nil {
        return fmt.Errorf("scheduler not initialized")
    }
    return scheduler.addTask(task)
}

func (s *Scheduler) start() {
    s.wg.Add(1)
    go s.runLoop()
    fmt.Printf("Scheduler started.\n")
}

func (s *Scheduler) stop() {
    fmt.Printf("Stopping scheduler...\n")
    s.cancel()
    s.wg.Wait()
    fmt.Printf("Scheduler stopped.\n")
}

func (s *Scheduler) addTask(task *Task) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if _, exists := s.tasks[task.Name]; exists {
        return fmt.Errorf("task '%s' already exists", task.Name)
    }

    s.tasks[task.Name] = task

    // 计算初始运行时间
    return s.initTaskExecTime(task)
}

func (s *Scheduler) initTaskExecTime(task *Task) error {
    execTime, err := task.calculateExecTime(time.Now())
    if err != nil {
        fmt.Printf("Calculate exec time for task '%s' failed. Err info: %v\n", task.Name, err)
        return err
    }

    next, err := s.redisHelper.GetTime(task.execTimeKey())
    if err != nil {
        fmt.Printf("Get exec time for task '%s' failed. Err info: %v\n", task.Name, err)
        return err
    }

    if !next.IsZero() && execTime.Equal(next) {
        fmt.Printf("Task '%s' already initialized. Next exec time: %s\n", task.Name, next.Format(time.DateTime))
        return nil
    }

    err = s.redisHelper.SetTime(task.execTimeKey(), execTime, time.Until(execTime)*2)
    if err != nil {
        fmt.Printf("Update exec time for task '%s' failed. Err info: %v\n", task.Name, err)
        return err
    }
    return nil
}

func (s *Scheduler) runLoop() {
    defer s.wg.Done()

    var ticker *time.Ticker

    // 对齐运行时间，确保是 timeSlice 的整数倍
    now := time.Now()
    // 额外增加 10ms 的延迟，确保在时间片的边界上开始扫描任务
    nextTickTime := now.Truncate(s.timeSlice).Add(s.timeSlice).Add(10 * time.Millisecond)
    firstTickTimer := time.NewTimer(nextTickTime.Sub(now))

    select {
    case <-s.ctx.Done(): // 上下文取消
        firstTickTimer.Stop()
        return
    case firstFireTime := <-firstTickTimer.C:
        firstTickTimer.Stop()
        ticker = time.NewTicker(s.timeSlice)
        defer ticker.Stop()

        s.scanAndRunTasks(firstFireTime)
    }

    if ticker == nil {
        return
    }

    for {
        select {
        case <-s.ctx.Done(): // 上下文取消
            return
        case fireTime := <-ticker.C:
            // 随机延迟，避免始终由同一实例率先获取锁
            // 控制延迟时间在 1s 内
            time.Sleep(time.Duration(rand.IntN(980)) * time.Millisecond)
            s.scanAndRunTasks(fireTime)
        }
    }
}

func (s *Scheduler) scanAndRunTasks(currentTime time.Time) {
    fmt.Printf("[%s] Scheduler tick: Scanning tasks...\n", currentTime.Format(time.DateTime))

    // 获取主调度锁，确保只有一个实例执行扫描任务，TTL 需要略大于时间片
    err := s.mainLock.Lock()
    if err != nil {
        // 获取锁失败，意味着另一个实例正在工作，本实例跳过此轮
        fmt.Printf("Failed to acquire scheduler leader lock. Err info: %v\n", err)
        return
    }

    lockBeginTime := time.Now()
    defer func() {
        // 限制锁的最短持有时间，防止扫描时间过短，其他实例在该次扫描中也成功获取到锁
        lockDuration := time.Since(lockBeginTime)
        if lockDuration < s.timeSlice/2 {
            time.Sleep(s.timeSlice/2 - lockDuration)
        }
        s.mainLock.Unlock()
    }()

    fmt.Printf("Acquired scheduler leader lock. Proceeding with task scan.\n")

    // 防止并发修改任务列表
    s.mu.Lock()
    taskToScan := make([]*Task, 0, len(s.tasks))
    for _, task := range s.tasks {
        taskToScan = append(taskToScan, task)
    }
    s.mu.Unlock()

    for _, task := range taskToScan {
        execTime, err := s.redisHelper.GetTime(task.execTimeKey())
        if err != nil {
            s.failedCounter[task.Name]++
            fmt.Printf("Get exec time for task '%s' failed. Failed time:%d. Err info: %v\n", task.Name, s.failedCounter[task.Name], err)

            if s.failedCounter[task.Name] >= 3 {
                err = s.initTaskExecTime(task)
                if err != nil {
                    fmt.Printf("Reset exec time for task '%s' failed. Err info: %v\n", task.Name, err)
                }
            }
            continue
        }

        if execTime.IsZero() {
            fmt.Printf("Task '%s' exec time is zero. Initializing...\n", task.Name)
            err = s.initTaskExecTime(task)
            if err != nil {
                fmt.Printf("Reset exec time for task '%s' failed. Err info: %v\n", task.Name, err)
            }
            continue
        }

        if currentTime.After(execTime) {
            s.failedCounter[task.Name] = 0
            fmt.Printf("Task '%s' is due, exec time: %s\n", task.Name, execTime.Format(time.DateTime))
            go s.executeTask(task, execTime)
        }
    }
}

func (s *Scheduler) executeTask(t *Task, execTime time.Time) {
    defer func() {
        if r := recover(); r != nil {
            fmt.Printf("Task '%s' panicked: %v\n", t.Name, r)
        }
    }()

    if t.Options.DistributedKey != "" {
        // 需要分布式锁
        expire := time.Minute
        if t.Options.DistributedExpire > 0 {
            expire = t.Options.DistributedExpire
        }

        lock := NewDistributedLock(t.Options.DistributedKey, expire)
        err := lock.Lock()
        if err != nil {
            fmt.Printf("Task '%s' skipped: could not acquire distributed lock for key '%s'. Err info: %v\n", t.Name, t.Options.DistributedKey, err)
            return // 获取并发锁失败，不执行
        }
        defer lock.Unlock()

        fmt.Printf("Task '%s' acquired distributed lock '%s'. Executing...\n", t.Name, t.Options.DistributedKey)
    } else {
        fmt.Printf("Task '%s' executing (no distributed lock)...\n", t.Name)
    }

    // 并发保障，通过 cas 更新下次执行时间，如果更新失败，说明任务已经被其他实例执行过
    nextExecTime, err := t.calculateExecTime(execTime)
    if err != nil {
        fmt.Printf("Calculate exec time for task '%s' failed. Err info: %v\n", t.Name, err)
        return
    }

    res, err := s.redisHelper.CASForTime(t.execTimeKey(), execTime, nextExecTime, time.Until(nextExecTime)*2)
    if err != nil {
        fmt.Printf("Update exec time for task '%s' failed. Err info: %v\n", t.Name, err)
        return
    }
    if !res {
        fmt.Printf("Task '%s' skipped: exec time has changed since last scan\n", t.Name)
        return
    }

    beginTime := time.Now()
    err = t.Job(s.ctx)
    if err != nil {
        fmt.Printf("Execute task '%s' failed. Err info: %v\n", t.Name, err)
    } else {
        fmt.Printf("Task '%s' executed successfully. Execution time:%s\n", t.Name, time.Since(beginTime).String())
    }
}
```
