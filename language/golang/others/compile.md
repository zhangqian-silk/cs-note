# Compile

## 编译器主函数

`src/cmd/compile/internal/gc/main.go` 中的 [Main()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/main.go#L59) 函数，是 Go 编译器的程序入口，会先读取命令行配置的参数，然后更新对应的编译选项和配置。

```go
// Main parses flags and Go source files specified in the command-line
// arguments, type-checks the parsed Go package, compiles functions to machine
// code, and finally writes the compiled package definition to disk.
func Main(archInit func(*ssagen.ArchInfo)) {
    base.Timer.Start("fe", "init")
    ...
}
```

<br>

随后会通过 [LoadPackage()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/noder.go#L27) 方法，加载并解析文件，[LoadPackage()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/noder.go#L27) 方法内部会调用 [syntax.Parse()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/syntax.go#L66) 方法对输入文件进行词法分析与语法分析，得到抽象语法树（AST），然后通过 [unified()](https://github.com/golang/go/blob/master/src/cmd/compile/internal/noder/unified.go#L187) 进行类型检查，并从 AST 构造出编译器所需的内部数据。

```go
func Main(archInit func(*ssagen.ArchInfo)) {
    ...
    // Parse and typecheck input.
    noder.LoadPackage(flag.Args())
    ...
}

func LoadPackage(filenames []string) {
    base.Timer.Start("fe", "parse")

    // Move the entire syntax processing logic into a separate goroutine to avoid blocking on the "sem".
    go func() {
        for i, filename := range filenames {
            ...
            go func() {
                ...
                p.file, _ = syntax.Parse(fbase, f, p.error, p.pragma, syntax.CheckBranches) // errors are tracked via p.error
            }()
        }
    }()
    ...
    unified(m, noders)
}
```

<br>

之后初始化编译器的后端程序，即 [ssagen.InitConfig()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L71)，然后会先执行 [enqueueFunc()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/compile.go#L30) 函数，对要编译的目标函数做一些处理，例如替换函数的具体实现，并将目标函数添加至队列中。

```go
func Main(archInit func(*ssagen.ArchInfo)) {
    ...
    // Prepare for backend processing.
    ssagen.InitConfig()
    ...
    // Compile top-level declarations.
    base.Timer.Start("be", "compilefuncs")
    for nextFunc, nextExtern := 0, 0; ; {
        ...
        if nextFunc < len(typecheck.Target.Funcs) {
            enqueueFunc(typecheck.Target.Funcs[nextFunc])
            nextFunc++
            continue
        }
        ...
    }
    ...
}

func enqueueFunc(fn *ir.Func) {
    ...
    todo := []*ir.Func{fn}
    for len(todo) > 0 {
        next := todo[len(todo)-1]
        todo = todo[:len(todo)-1]

        prepareFunc(next)
        todo = append(todo, next.Closures...)
    }
    ...
    // Enqueue just fn itself. compileFunctions will handle
    // scheduling compilation of its closures after it's done.
    compilequeue = append(compilequeue, fn)
}
```

<br>

在 [compileFunctions()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/compile.go#L115) 函数中，会编译所有函数，将其转化为 SSA 形式的中间代码。

```go
func Main(archInit func(*ssagen.ArchInfo)) {
    ...
    for nextFunc, nextExtern := 0, 0; ; {
        ...
        // The SSA backend supports using multiple goroutines, so keep it
        // as late as possible to maximize how much work we can batch and
        // process concurrently.
        if len(compilequeue) != 0 {
            compileFunctions(profile)
            continue
        }

        ...
        break
    }
    ...
}
```

## 词法分析

[词法分析](https://zh.wikipedia.org/wiki/%E8%AF%8D%E6%B3%95%E5%88%86%E6%9E%90)是计算机科学中将字符序列转换为标记（token）序列的过程，在 Golang 中，[token](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/tokens.go#L7) 主要分为五类，分别是名称、字面量、操作符、分隔符和关键字：

```go
const (
    _    token = iota
    _EOF       // EOF

    // names and literals
    _Name    // name
    _Literal // literal

    // operators and operations
    _Assign   // =
    _Define   // :=
    ...

    // delimiters
    _Lparen    // (
    _Lbrack    // [
    ...

    // keywords
    _Import      // import
    _Struct      // struct
    _Type        // type
    _Var         // var
    ...
)
```

### [Parse()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/syntax.go#L66)

[Parse()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/syntax.go#L66) 函数内部会构建一个 `parser` 对象，然后通过 [p.fileOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L392) 方法，循环调用 [next()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L88) 方法，触发词法解析逻辑。

```go
// Parse parses a single Go source file from src and returns the corresponding
// syntax tree. If there are errors, Parse will return the first error found,
// and a possibly partially constructed syntax tree, or nil.
func Parse(base *PosBase, src io.Reader, errh ErrorHandler, pragh PragmaHandler, mode Mode) (_ *File, first error) {
    ...
    var p parser
    p.init(base, src, errh, pragh, mode)
    p.next()
    return p.fileOrNil(), p.first
}

func (p *parser) fileOrNil() *File {
    ...
    for p.tok != _EOF {
        ...
        p.next()
        ...
    }
    ...
}
```

<br>

[parser](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L18) 结构体内部嵌套了 [scaner](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L30) 结构体，故 [next()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L88) 函数真正的执行逻辑，是由 [scaner](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L30) 负责

```go
type parser struct {
    scanner
    ...
}

// next advances the scanner by reading the next token.
func (s *scanner) next() {
    ...
}
```

### [next()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L88)

[next()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L88) 函数内部，会先跳过一些空白符，然后针对字母或关键字进行特殊处理

```go
func (s *scanner) next() {
    ...
redo:
    // skip white space
    s.stop()
    startLine, startCol := s.pos()
    for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' && !nlsemi || s.ch == '\r' {
        s.nextch()
    }

    // token start
    s.line, s.col = s.pos()
    s.blank = s.line > startLine || startCol == colbase
    s.start()
    if isLetter(s.ch) || s.ch >= utf8.RuneSelf && s.atIdentChar(true) {
        s.nextch()
        s.ident()
        return
    }
    ...
}
```

<br>

最后是识别字面量、操作符与分隔符对应的特殊符号，枚举进行处理。

```go
func (s *scanner) next() {
redo:
    ...
    switch s.ch {
    case '\n':
        s.nextch()
        s.lit = "newline"
        s.tok = _Semi

    case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
        s.number(false)

    case '"':
        s.stdString()

    case '(':
        s.nextch()
        s.tok = _Lparen

    ...

    default:
        s.errorf("invalid character %#U", s.ch)
        s.nextch()
        goto redo
    }
    ...
}
```

<br>

以最常见的标准字符串，即 [stdString](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L674) 为例，会循环读取后续所有字符，直至遇到下一个双引号：

```go
func (s *scanner) stdString() {
    ok := true
    s.nextch()

    for {
        if s.ch == '"' {
            s.nextch()
            break
        }
        if s.ch == '\\' {
            s.nextch()
            if !s.escape('"') {
                ok = false
            }
            continue
        }
        if s.ch == '\n' {
            s.errorf("newline in string")
            ok = false
            break
        }
        if s.ch < 0 {
            s.errorAtf(0, "string not terminated")
            ok = false
            break
        }
        s.nextch()
    }

    s.setLit(StringLit, ok)
}
```

## 语法分析

[语法分析](https://zh.wikipedia.org/wiki/%E8%AF%AD%E6%B3%95%E5%88%86%E6%9E%90)是根据某种给定的形式文法对由单词序列（即 token 序列）构成的输入文本进行分析并确定其语法结构的一种过程。

在 golang 中，语法分析的过程，同样在 [Parse()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/syntax.go#L666) 函数内部，由 [fileOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L392) 方法进行处理，即与词法解析同步进行，方法最终会返回该源文件文件对应的 AST 文件。

```go
func Parse(base *PosBase, src io.Reader, errh ErrorHandler, pragh PragmaHandler, mode Mode) (_ *File, first error) {
    ...
    var p parser
    p.init(base, src, errh, pragh, mode)
    p.next()
    return p.fileOrNil(), p.first
}
```

### [fileOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L392)

Go 会针对每个源文件生成一个独立的 AST，即 [File](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L38) 结构体，所以 [fileOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L392) 方法首先会解析 `_Package` 字段，匹配包名，并保存至该结构体中。

```go
type File struct {
    Pragma    Pragma
    PkgName   *Name
    DeclList  []Decl
    EOF       Pos
    GoVersion string
    node
}

// SourceFile = PackageClause ";" { ImportDecl ";" } { TopLevelDecl ";" } .
func (p *parser) fileOrNil() *File {
    ...
    f := new(File)
    ...
    // PackageClause
    f.GoVersion = p.goVersion
    p.top = false
    if !p.got(_Package) {
        p.syntaxError("package statement must be first")
        return nil
    }
    f.Pragma = p.takePragma()
    f.PkgName = p.name()
    p.want(_Semi)
    ...
}
```

<br>

其中 [got()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L193) 方法会去调用一次词法分析，并判断是否是想要的 token 类型， [name()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L2700) 方法会去匹配并得到一个 `_Name` 类型的 token：

```go
func (p *parser) got(tok token) bool {
    if p.tok == tok {
        p.next()
        return true
    }
    return false
}

func (p *parser) name() *Name {
    if p.tok == _Name {
        n := NewName(p.pos(), p.lit)
        p.next()
        return n
    }
    ...
}
```

<br>

紧接着会循环解析所有 `_Import` 字段，并确保 `import` 声明全部在 `package` 声明之后，在其他字段之前，[importDecl()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L539) 会用来处理 `_Import` 类型：

```go
func (p *parser) fileOrNil() *File {
    ...
    // Accept import declarations anywhere for error tolerance, but complain.
    // { ( ImportDecl | TopLevelDecl ) ";" }
    prev := _Import
    for p.tok != _EOF {
        if p.tok == _Import && prev != _Import {
            p.syntaxError("imports must appear before other declarations")
        }
        prev = p.tok
        switch p.tok {
        case _Import:
            p.next()
            f.DeclList = p.appendGroup(f.DeclList, p.importDecl)
        ...
        }
        ...
    }
    ...
}
```

<br>

在之后，会继续处理其他顶层声明，即常量、类型、变量和函数，相对应的，他们也都有各自的处理函数：

```go
func (p *parser) fileOrNil() *File {
    ...
    for p.tok != _EOF {
        ...
        switch p.tok {
        ...
        case _Const:
            p.next()
            f.DeclList = p.appendGroup(f.DeclList, p.constDecl)

        case _Type:
            p.next()
            f.DeclList = p.appendGroup(f.DeclList, p.typeDecl)

        case _Var:
            p.next()
            f.DeclList = p.appendGroup(f.DeclList, p.varDecl)

        case _Func:
            p.next()
            if d := p.funcDeclOrNil(); d != nil {
                f.DeclList = append(f.DeclList, d)
            }
        ...
        }
        ...
    }
    ...
}

```

<br>

在匹配到对应的字段后，均会通过 [appendGroup()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L518) 方法，执行对应的处理逻辑，得到该节点对应的结构体，满足 [Decl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L51) 接口，并将其添加至 AST 文件的 `DeclList` 中：

```go
Decl interface {
    Node
    aDecl()
}

type Node interface {
    Pos() Pos
    SetPos(Pos)
    aNode()
}

// appendGroup(f) = f | "(" { f ";" } ")" . // ";" is optional before ")"
func (p *parser) appendGroup(list []Decl, f func(*Group) Decl) []Decl {
    if p.tok == _Lparen {
        g := new(Group)
        p.clearPragma()
        p.next() // must consume "(" after calling clearPragma!
        p.list("grouped declaration", _Semi, _Rparen, func() bool {
            if x := f(g); x != nil {
                list = append(list, x)
            }
            return false
        })
    } else {
        if x := f(nil); x != nil {
            list = append(list, x)
        }
    }
    return list
}
```

### [decl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L116)

针对于不同的字段，处理逻辑也会有相对应的差异，并最终得到相对应的节点的结构体，每个结构体中都嵌入了 `decl` 结构体，故均满足 `Decl` 接口：

- [decl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L116)

    ```go
    type decl struct{ node }

    func (*decl) aDecl() {}

    type node struct {
        pos Pos
    }

    func (n *node) Pos() Pos       { return n.pos }
    func (n *node) SetPos(pos Pos) { n.pos = pos }
    func (*node) aNode()           {}
    ```

<br>

- [importDecl()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L539) : [ImportDecl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L58)

    ```go
    // ImportSpec = [ "." | PackageName ] ImportPath .
    // ImportPath = string_lit .
    func (p *parser) importDecl(group *Group) Decl {
        d := new(ImportDecl)
        ...
    }

    //              Path
    // LocalPkgName Path
    type ImportDecl struct {
        Group        *Group // nil means not part of a group
        Pragma       Pragma
        LocalPkgName *Name     // including "."; nil means no rename present
        Path         *BasicLit // Path.Bad || Path.Kind == StringLit; nil means no path
        decl
    }
    ```

<br>

- [constDecl()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L572) : [ConstDecl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L69)

    ```go
    // ConstSpec = IdentifierList [ [ Type ] "=" ExpressionList ] .
    func (p *parser) constDecl(group *Group) Decl {
        d := new(ConstDecl)
        ...
    }

    // NameList
    // NameList      = Values
    // NameList Type = Values
    type ConstDecl struct {
        Group    *Group // nil means not part of a group
        Pragma   Pragma
        NameList []*Name
        Type     Expr // nil means no type
        Values   Expr // nil means no values
        decl
    }
    ```

<br>

- [typeDecl()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L594) : [typeDecl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L79)

    ```go
    // TypeSpec = identifier [ TypeParams ] [ "=" ] Type .
    func (p *parser) typeDecl(group *Group) Decl {
        d := new(TypeDecl)
        ...
    }

    // Name Type
    type TypeDecl struct {
        Group      *Group // nil means not part of a group
        Pragma     Pragma
        Name       *Name
        TParamList []*Field // nil means no type parameters
        Alias      bool
        Type       Expr
        decl
    }
    ```

<br>

- [varDecl()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L745) : [VarDecl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L92)

    ```go
    // VarSpec = IdentifierList ( Type [ "=" ExpressionList ] | "=" ExpressionList ) .
    func (p *parser) varDecl(group *Group) Decl {
        d := new(VarDecl)
        ...
    }

    // NameList Type
    // NameList Type = Values
    // NameList      = Values
    type VarDecl struct {
        Group    *Group // nil means not part of a group
        Pragma   Pragma
        NameList []*Name
        Type     Expr // nil means no type
        Values   Expr // nil means no values
        decl
    }
    ```

<br>

- [funcDeclOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L773) : [funcDecl](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/nodes.go#L105)

    ```go
    // FunctionDecl = "func" FunctionName [ TypeParams ] ( Function | Signature ) .
    // FunctionName = identifier .
    // Function     = Signature FunctionBody .
    // MethodDecl   = "func" Receiver MethodName ( Function | Signature ) .
    // Receiver     = Parameters .
    func (p *parser) funcDeclOrNil() *FuncDecl {
        f := new(FuncDecl)
        ...
    }

    // func          Name Type { Body }
    // func          Name Type
    // func Receiver Name Type { Body }
    // func Receiver Name Type
    FuncDecl struct {
        Pragma     Pragma
        Recv       *Field // nil means regular function
        Name       *Name
        TParamList []*Field // nil means no type parameters
        Type       *FuncType
        Body       *BlockStmt // nil means no body (forward declaration)
        decl
    }
    ```

### [stmtOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L2551)

在 [funcDeclOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L773) 函数内部，会调用 [funcBody()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L819) 方法与 [blockStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L2240) 方法生成函数体。

```go
func (p *parser) funcDeclOrNil() *FuncDecl {
    f := new(FuncDecl)
    ...
    if p.tok == _Lbrace {
        f.Body = p.funcBody()
    }

    return f
}

func (p *parser) funcBody() *BlockStmt {
    ...
    body := p.blockStmt("")
    ...
    return body
}

func (p *parser) blockStmt(context string) *BlockStmt {
    ...
    s.List = p.stmtList()
    ...
    return s
}
```

<br>

我们日常所编写的代码逻辑，例如函数调用，局部变量声明，逻辑判断与循环等等， 其实绝大部分都是在各个函数体中，并非是 AST 的顶级声明。

在构建函数体，亦即构建 block 声明时，会调用 [stmtList()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L2654) 方法和 [stmtOrNil()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/parser.go#L2551) 方法来进行处理。

```go
// StatementList = { Statement ";" } .
func (p *parser) stmtList() (l []Stmt) {
    for p.tok != _EOF && p.tok != _Rbrace && p.tok != _Case && p.tok != _Default {
        s := p.stmtOrNil()
        ...
    }
    return
}

// stmtOrNil parses a statement if one is present, or else returns nil.
//
//  Statement =
//      Declaration | LabeledStmt | SimpleStmt |
//      GoStmt | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt |
//      FallthroughStmt | Block | IfStmt | SwitchStmt | SelectStmt | ForStmt |
//      DeferStmt .
func (p *parser) stmtOrNil() Stmt {

}
```

<br>

在具体判断时，因为大部分语句都是 `_Name` 类型的 token，例如函数调用，变量赋值等，所以先对该类型进行判断并处理。

```go
func (p *parser) stmtOrNil() Stmt {
    ...
    // Most statements (assignments) start with an identifier;
    // look for it first before doing anything more expensive.
    if p.tok == _Name {
        p.clearPragma()
        lhs := p.exprList()
        if label, ok := lhs.(*Name); ok && p.tok == _Colon {
            return p.labeledStmtOrNil(label)
        }
        return p.simpleStmt(lhs, 0)
    }
    ...
}
```

<br>

在之后对 `_Var`、`_Const` 和 `_Type` 三种顶级声明进行处理，需要注意的时，此时他们的作用域并非全局，而是在 block 内部。

```go
func (p *parser) stmtOrNil() Stmt {
    ...
    switch p.tok {
    case _Var:
        return p.declStmt(p.varDecl)

    case _Const:
        return p.declStmt(p.constDecl)

    case _Type:
        return p.declStmt(p.typeDecl)
    }
    ...
}
```

<br>

最后是对其他关键字进行处理，例如 `_For`、`_Go`、`_Defer` 等。

```go
func (p *parser) stmtOrNil() Stmt {
    ...
    switch p.tok {
    case _For:
        return p.forStmt()

    case _Go, _Defer:
        return p.callStmt()
    ...
    }

    return nil
}
```

## 类型检查

执行完词法分析、语法分析，得到最终的抽象语法树（AST）后，还需要执行 [unified()](https://github.com/golang/go/blob/master/src/cmd/compile/internal/noder/unified.go#L187) 函数，加载数据，对数据进行类型检查，并构造出内部节点（IR, Internal Representation）。

```go
// unified constructs the local package's Internal Representation (IR)
// from its syntax tree (AST).
func unified(m posMap, noders []*noder) {
    ...
    readBodies(target, false)
    ...
}
```

<br>

其中 [readBodies()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/unified.go#L216) 函数会针对源文件中的函数体，进行类型检查。

```go
func readBodies(target *ir.Package, duringInlining bool) {
    ...
    for {
        ...
        pri, ok := bodyReader[fn]
        pri.funcBody(fn)
        ...
    }
    ...
}
```

<br>

函数内部通过 `fn` 获取到对应的 `pri` 实例，并执行 [funcBody()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/reader.go#L1227) 函数，函数内部会通过 [stmts()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/reader.go#L1580) 函数，检查函数体中的所有语句，并返回构造完成的 `body`。

```go
func (pri pkgReaderIndex) funcBody(fn *ir.Func) {
    r := pri.asReader(pkgbits.RelocBody, pkgbits.SyncFuncBody)
    r.funcBody(fn)
}

// funcBody reads a function body definition from the element
// bitstream, and populates fn with it.
func (r *reader) funcBody(fn *ir.Func) {
    ...
    ir.WithFunc(fn, func() {
        ...
        body := r.stmts()
        ...
        fn.Body = body
        ...
    })
    ...
}
```

<br>

而 [stmts()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/reader.go#L1580) 函数中，会继续调用 [typecheck.Stmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/typecheck.go#L24)、[typecheck()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/typecheck.go#L150) 和 [typecheck1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/typecheck.go#L218) 函数，执行真正的类型检查逻辑。

```go
func (r *reader) stmts() ir.Nodes {
    ...
    for {
        ...
        res.Append(typecheck.Stmt(n))
        ...
    }
}

func Stmt(n ir.Node) ir.Node       { return typecheck(n, ctxStmt) }

func typecheck(n ir.Node, top int) (res ir.Node) {
    ...
    n = typecheck1(n, top)
    ...
}
```

## 节点替换

在将待编译函数添加至队列中时，会先执行 [enqueueFunc()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/compile.go#L30) 函数，对要编译的目标函数做一些处理，并将目标函数添加至队列中。

```go
func enqueueFunc(fn *ir.Func) {
    ...
    todo := []*ir.Func{fn}
    for len(todo) > 0 {
        next := todo[len(todo)-1]
        todo = todo[:len(todo)-1]

        prepareFunc(next)
        todo = append(todo, next.Closures...)
    }
    ...
    // Enqueue just fn itself. compileFunctions will handle
    // scheduling compilation of its closures after it's done.
    compilequeue = append(compilequeue, fn)
}
```

<br>

在 [prepareFunc()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/compile.go#L90C6-L90C17) 函数中，会调用 [walk.Walk](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/walk.go#L22) 函数，将 AST 中的部分关键字和内建函数替换为真正的运行时函数。

```go
func prepareFunc(fn *ir.Func) {
    ...
    walk.Walk(fn)
    ...
}

func Walk(fn *ir.Func) {
    ...
    walkStmtList(ir.CurFunc.Body)
    ...
}
```

### [walkStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L15)

在 [walkStmtList()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L174) 会对传入的节点进行遍历处理，最终通过 [walkStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L15) 函数内部，针对具体语句，分别执行替换逻辑。

```go
func walkStmtList(s []ir.Node) {
    for i := range s {
        s[i] = walkStmt(s[i])
    }
}
```

<br>

例如对于 `OAS`、`OPANIC` 等语句，会额外调用 [walkExpr()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L29)函数和 [walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数 进行处理。对于 `OAS` 节点，会通过 [walkAssign()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/assign.go#L20)  函数继续处理赋值相关逻辑，对于 `panic` 关键字，则转化为调用 `gopanic()` 函数：

```go
func walkStmt(n ir.Node) ir.Node {
    ...
    switch n.Op() {
    case ir.OAS,
            ...
            ir.OPANIC:
        ...
        n = walkExpr(n, &init)
        ...
    ...
    }
    ...
}

func walkExpr(n ir.Node, init *ir.Nodes) ir.Node {
    ...
    n = walkExpr1(n, init)
    ...
    return n
}

func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
    switch n.Op() {
    case ir.OAS, ir.OASOP:
        return walkAssign(init, n)
    case ir.OPANIC:
        n := n.(*ir.UnaryExpr)
        return mkcall("gopanic", nil, init, n.X)
    ...
    }
}
```

<br>

对于部分语句，例如 `OBREAK` 和 `OGOTO` 等，不做特殊处理：

```go
func walkStmt(n ir.Node) ir.Node {
    ...
    switch n.Op() {
    case ir.OBREAK,
        ...
        ir.OGOTO:
        return n
    ...
    }
    ...
}
```

<br>

对于部分语句，例如 `ORANGE`，直接在 `walkStmt()` 函数中，调用具体的处理逻辑，即 [walkRange()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L40)，函数内部将 `ORANGE` 节点转化为 `OFOR` 节点，并根据具体元素，数组、切片、哈希表等，添加不同的处理逻辑。

```go
func walkStmt(n ir.Node) ir.Node {
    ...
    switch n.Op() {
    case ir.ORANGE:
        n := n.(*ir.RangeStmt)
        return walkRange(n)
    ...
    }
    ...
}

func walkRange(nrange *ir.RangeStmt) ir.Node {
    ...
    nfor := ir.NewForStmt(nrange.Pos(), nil, nil, nil, nil, nrange.DistinctVars)
    ...
    switch k := t.Kind(); {
    case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
        ...
    case k == types.TMAP:
        ...
    ...
    }
    ...
    var n ir.Node = nfor
    ...
    return n
}
```

<br>

部分关键字，例如 `make()` 函数相关操作，一般紧跟在赋值节点 `OAS` 之后，会通过 [walkAssign()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/assign.go#L20)函数再调用至[walkExpr()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L29)函数和 [walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数中进行处理，并调用至最终实现，即 [walkMakeChan()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/builtin.go#L285) 函数。

```go
func walkAssign(init *ir.Nodes, n ir.Node) ir.Node {
    ...
    switch as.Y.Op() {
    default:
        as.Y = walkExpr(as.Y, init)
    ...
    }
    ...
}

func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
    switch n.Op() {
    case ir.OMAKECHAN:
        n := n.(*ir.MakeExpr)
        return walkMakeChan(n, init)
    ...
    }
}
```

## 生成中间代码

经过 `walk` 系列函数处理之后，会得到最终的抽象语法树，`range`、`panic` 等类似语法糖的语句，也会被转化为真正的实现语句，此时会在 [compileFunctions()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/gc/compile.go#L115) 函数中会通过 [ssagen.Compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/pgen.go#L215) 函数，执行具体编译逻辑，并最终生成中间代码。

```go
// compileFunctions compiles all functions in compilequeue.
func compileFunctions(profile *pgoir.Profile) {
    ...
    var wg sync.WaitGroup
    var compile func([]*ir.Func)
    compile = func(fns []*ir.Func) {
        wg.Add(len(fns))
        for _, fn := range fns {
            fn := fn
            queue(func(worker int) {
                ssagen.Compile(fn, worker, profile)
                compile(fn.Closures)
                wg.Done()
            })
        }
    }
    ...
    compile(compilequeue)
    compilequeue = nil
    wg.Wait()
    ...
}
```

<br>

[ssagen.Compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/pgen.go#L215) 函数内部会调用 [buildssa()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L293) 函数，执行具体的编译操作，函数内部会首先调用 [stmtList()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L1424) 函数，将所有 AST 节点转化为 SSA 形式的中间代码，然后再调用 [compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssa/compile.go#L30) 函数，进行优化。

```go
// Compile builds an SSA backend function,
// uses it to generate a plist,
// and flushes that plist to machine code.
// worker indicates which of the backend workers is doing the processing.
func Compile(fn *ir.Func, worker int, profile *pgoir.Profile) {
    f := buildssa(fn, worker, inline.IsPgoHotFunc(fn, profile) || inline.HasPgoHotInline(fn))
    ...
}

func buildssa(fn *ir.Func, worker int, isPgoHot bool) *ssa.Func {
    ...
    // 将 AST 节点转化为中间代码
    s.stmtList(fn.Body)
    ...
    // 优化中间代码
    ssa.Compile(s.f)
    ...
}

// stmtList converts the statement list n to SSA and adds it to s.
func (s *state) stmtList(l ir.Nodes) {
    for _, n := range l {
        s.stmt(n)
    }
}
```

### [stmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L1431)

[stmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L1431) 函数负责根据节点操作符的不同，将 AST 节点转化为 SSA 形式的中间代码，例如常见的 `go`、`if`、`return`、`for` 等等。

```go
func (s *state) stmt(n ir.Node) {
    ...
    switch n.Op() {
    case ir.OGO:
        n := n.(*ir.GoDeferStmt)
        ...
    case ir.OIF:
        n := n.(*ir.IfStmt)
        ...
    case ir.ORETURN:
        n := n.(*ir.ReturnStmt)
        ...
    case ir.OFOR:
        // OFOR: for Ninit; Left; Right { Nbody }
        // cond (Left); body (Nbody); incr (Right)
        n := n.(*ir.ForStmt)
        ...
    ...
    default:
        s.Fatalf("unhandled stmt %v", n.Op())
    }
}
```

### [Compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssa/compile.go#L30)

[Compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssa/compile.go#L30) 函数内部，会调用多种处理函数进行优化处理，即 [passes](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssa/compile.go#L457)，处理函数内部也会根据架构不同，执行相对应的策略，函数会构建出最终的 SSA 形式的中间代码。

```go
func Compile(f *Func) {
    ...
    for _, p := range passes {
        ...
        p.fn(f)
        ...
    }
    ...
}

var passes = [...]pass{
    {name: "number lines", fn: numberLines, required: true},
    {name: "early phielim and copyelim", fn: copyelim},
    {name: "early deadcode", fn: deadcode}, // remove generated dead code to avoid doing pointless work during opt
    ...
    {name: "loop rotate", fn: loopRotate},
    {name: "trim", fn: trim}, // remove empty blocks
}
```

## 生成机器码

如之前所言，[ssagen.Compile()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/pgen.go#L215) 函数内部会调用 [buildssa()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L293) 函数生成最终的 SSA 形式的中间代码。

```go
// Compile builds an SSA backend function,
// uses it to generate a plist,
// and flushes that plist to machine code.
// worker indicates which of the backend workers is doing the processing.
func Compile(fn *ir.Func, worker int, profile *pgoir.Profile) {
    f := buildssa(fn, worker, inline.IsPgoHotFunc(fn, profile) || inline.HasPgoHotInline(fn))
    ...
}
```

<br>

在此之后，会构建一个 [Progs](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/objw/prog.go#L67) 结构体，然后通过 [genssa()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L7252) 函数，将 SSA 中间代码存入 [Progs](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/objw/prog.go#L67) 结构体中，并用来生成机器码。

```go
func Compile(fn *ir.Func, worker int, profile *pgoir.Profile) {
    ...
    pp := objw.NewProgs(fn, worker)
    defer pp.Free()
    genssa(f, pp)
    ...
}

// Progs accumulates Progs for a function and converts them into machine code.
type Progs struct {
    Text       *obj.Prog  // ATEXT Prog for this function
    Next       *obj.Prog  // next Prog
    PC         int64      // virtual PC; count of Progs
    Pos        src.XPos   // position to use for new Progs
    CurFunc    *ir.Func   // fn these Progs are for
    ...
}
```

<br>

将所有 SSA 形式的中间代码加载完成后，会调用 [Flush()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/objw/prog.go#L110) 方法，完成机器码的生成工作。

```go
func Compile(fn *ir.Func, worker int, profile *pgoir.Profile) {
    ...
    pp.Flush() // assemble, fill in boilerplate, etc.
    ...
}

// Flush converts from pp to machine code.
func (pp *Progs) Flush() {
    plist := &obj.Plist{Firstpc: pp.Text, Curfn: pp.CurFunc}
    obj.Flushplist(base.Ctxt, plist, pp.NewProg)
}
```

## Ref

- <https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/README.md>
- <https://draveness.me/golang/docs/part1-prerequisite/ch02-compile/golang-compile-intro/>
