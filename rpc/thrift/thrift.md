# Thrift

> [Thrift: Scalable Cross-Language Services Implementation](https://thrift.apache.org/static/files/thrift-20070401.pdf)
> [Github Apache Thrift](https://github.com/apache/thrift/tree/master)
> [Thrift: The Missing Guide](https://diwakergupta.github.io/thrift-missing-guide/#_language_reference)

## 简介

Thrift 是由 Fackbook 团队开发的跨语言的 RPC 框架，于 2007 年开源，后贡献给 Apache 基金会。

Thrift 采用了 C/S 架构，并通过 IDL(Interface Description Language) 定义接口，之后会协助生成目标语言的代码。生成的代码包括将数据结构和服务接口转换为目标语言的类和接口。

## IDL

> [Thrift Types](https://thrift.apache.org/docs/types)
> [Thrift interface description language](https://thrift.apache.org/docs/idl)

For Thrift version 0.20.0.

Thrift 可以按照 IDL 中定义的数据类型与服务，生成特定语言的代码，以达到跨语言通信的功能。使用者无需再考虑其他语言。

### Basic Definitions

```text
Literal         ::=  ('"' [^"]* '"') | ("'" [^']* "'")

Letter          ::=  ['A'-'Z'] | ['a'-'z']

Digit           ::=  ['0'-'9']

Identifier      ::=  ( Letter | '_' ) ( Letter | Digit | '.' | '_' )*

ListSeparator   ::=  ',' | ';'
```

- `Literal`：字面量，匹配所有单引号或双引号包裹起来的内容。
- `Letter` & `Digit`：字母和数字的集合。
- `Identifier`：标识符，用来定义变量名，结构名，服务名，等等。只能以字母或 '\_' 开头，只能包含字母、数字、'\.' 和 '\_'。
- `ListSeparator`：分隔符，用来标识语句的结束，通常是可选项。

### Types

```text
FieldType       ::=  Identifier | BaseType | ContainerType

BaseType        ::=  'bool' | 'byte' | 'i8' | 'i16' | 'i32' | 'i64' | 'double' | 'string' | 'binary' | 'uuid'

ContainerType   ::=  MapType | SetType | ListType

MapType         ::=  'map' CppType? '<' FieldType ',' FieldType '>'

SetType         ::=  'set' CppType? '<' FieldType '>'

ListType        ::=  'list' CppType? '<' FieldType '>' 

CppType         ::=  'cpp_type' Literal
```

Thrift 中的字段类型(`FieldType`)支持自定义类型(`Identifier`)、基础类型(`BaseType`)以及容器类型(`ContainerType`)。

基础类型(`BaseType`)的定义如下所示：

- bool: A boolean value (true or false)
- byte: An 8-bit signed integer
- i8: An 8-bit signed integer
- i16: A 16-bit signed integer
- i32: A 32-bit signed integer
- i64: A 64-bit signed integer
- double: A 64-bit floating point number
- string: A text string encoded using UTF-8 encoding
- binary: a sequence of unencoded bytes
- uuid(Universal unique identifier encoding): A 16-byte binary in big endian (or "network") order

可以看到，出于通用性考虑，基础类型并没有包括无符号整型（部分编程语言并不支持）。同时，也并非会一味兼容，例如在最近引入的 `uuid`，出于必要性考虑，会去推动其他语言引入相关类型的定义，如 [Introduce uuid as additional builtin type](https://issues.apache.org/jira/browse/THRIFT-5587)。

容器类型(`ContainerType`)的定义如下所示：

- list: An ordered list of elements. Translates to an STL vector, Java ArrayList, native arrays in scripting languages, etc.
- set: An unordered set of unique elements. Translates to an STL set, Java HashSet, set in Python, etc. Note: PHP does not support sets, so it is treated similar to a List
- map<type1,type2>: A map of strictly unique keys to values. Translates to an STL map, Java HashMap, PHP associative array, Python/Ruby dictionary, etc. While defaults are provided, the type mappings are not explicitly fixed. Custom code generator directives have been added to allow substitution of custom types in various destination languages.

Thrift 中的容器类型也是非常通用的，可以映射到大部分常用编程语言中，容器内部的元素，也可以是任何 Thrift 中的合法类型，但是容器中元素类型必须一致。此外，针对于 `map` 类型，为了最大的兼容性，我们在使用的时候最好是使用基础类型，而不是结构体或是容器类型，比如在 JSON 协议中，key 值就只能是基本类型。

### Constant Values

```text
ConstValue      ::=  IntConstant | DoubleConstant | Literal | Identifier | ConstList | ConstMap

IntConstant     ::=  ('+' | '-')? Digit+

DoubleConstant  ::=  ('+' | '-')? Digit* ('.' Digit+)? ( ('E' | 'e') IntConstant )?

ConstList       ::=  '[' (ConstValue ListSeparator?)* ']'

ConstMap        ::=  '{' (ConstValue ':' ConstValue ListSeparator?)* '}'
```

常量值(`ConstValue`)举例如下：

```thrift
// 数字均可支持正数或是复数
const i8 silk0 = 2000
const double silk1 = 3.26
const double silk2 = 3.26e3     // 科学计数法，即 3260
const double silk3 = 3.26e-3    // 科学计数法，即 0.00326

const string silk4 = "silk"
const string silk5 = silk4

// 此处 ',' 可替换为 ';'，或是直接省略
const list<string> silk6 = ['a', 'b', 'c']
const map<string, string> silk7 = {'key1': 'value1', 'key2': 'value2'}  
```

### Field

```text
Field           ::=  FieldID? FieldReq? FieldType Identifier ('=' ConstValue)? ListSeparator?

FieldID         ::=  IntConstant ':'

FieldReq        ::=  'required' | 'optional' 
```

字段(`Field`)主要包括字段类型(`FieldType`)与字段标识符(`Identifier`)，此外字段 ID(`FieldID`)、字段必要性(`FieldReq`)、字段默认值(`'=' ConstValue`)以及分隔符(`ListSeparator`)均是可选项。

字段 ID (`FieldID`)由整型常量(`IntConstant`)加上冒号分隔符(`':'`)构成，在序列化时，是字段的唯一标识，如果不填写的话，会有默认填充。但是同时也因为是唯一标识，随意删改会有较大影响，倾向于手动为字段赋值，且新的改动在原有基础上进行拓展，而非修改。

字段必要性(`FieldReq`)有两种显式声明 `required` 和 `optional`，也有一种默认取值 `default`。区别简介如下：

- `required`：一定会被序列化，如果未赋值，则会抛出异常。同时，就像是流传比较广的一句话，“Required Is Forever”，如果修改 `required` 的声明，则类似于改动字段 ID(`FieldID`)，导致版本的不兼容。
- `optional`：有赋值或有默认值时才会被序列化。
- `default`：类似于 `required` 和 `optional` 的混合，也被称作 `opt-in, req-out`。但是在实际场景中，还是存在字段不会被写入的情况，特别是某些字段无法通过 thrift 进行传输。

### Document

```text
Document        ::=  Header* Definition*
```

每个 IDL 文件包含 0 个或多个 `Header`，后面紧跟着 0 个或多个 `Definition`。

### Header

```text
Header          ::=  Include | CppInclude | Namespace
```

每个 `Header` 可以是一个 `Include`、`CppInclude` 或是 `Namespace`。

#### Thrift Include

```text
Include         ::=  'include' Literal
```

通过 `include` 关键字，加上一串用于表示文件路径的 `Literal` ，可以使另一个文件中的所有符号可见（带有前缀），并将相应的 `include` 语句添加到为此 Thrift 文件生成的代码中。

例如在 base.thrift 中，我们有如下定义：

```thrift
struct Base {
    ...
}
```

在 silk.thrift 中，就可以通过 `include` 引入该文件，并使用其内部的符号 `Base`（带有前缀 `base`）。

```thrift
include 'base.thrift'

struct Example {
    1: base.Base ExampleBase
}
```

#### C++ Include

```text
CppInclude      ::=  'cpp_include' Literal
```

`cpp_include` 可以将一个自定义的 C++ 引入声明添加到此 thrift 文档最终生成的 C++ 代码中。

#### Namespace

```text
Namespace       ::=  ( 'namespace' ( NamespaceScope Identifier ) )

NamespaceScope  ::=  '*' | 'c_glib' | 'cpp' | 'delphi' | 'haxe' | 'go' | 'java' | 'js' | 'lua' | 'netstd' | 'perl' | 'php' | 'py' | 'py.twisted' | 'rb' | 'st' | 'xsd'
```

`Namespace` 可以声明该 thrift 最终生成代码时，其内部定义的变量、结构、服务等将针对这些语言生成对应的代码。

```thrift
namespace go silk.example.go
namespace python silk.example.python
```

### Definition

```text
Definition      ::=  Const | Typedef | Enum | Struct | Union | Exception | Service
```

`Definition` 可以包含常量(`Const`)、类型声明(`Typedef`)、枚举(`Enum`)、结构体(`Struct`)、联合体(`Union`)、异常(`Exception`)、服务(`Service`)。这部分是 IDL 的核心内容。

#### Const

```text
Const           ::=  'const' FieldType Identifier '=' ConstValue ListSeparator?
```

常量(`Const`)声明的构成包括 `const` 关键字，常量的类型(`FieldType`)，常量的标识符(`Identifier`)，赋值符号(`=`)，常量值(`ConstValue`)以及可选的分隔符(`ListSeparator`)。

例如:

```thrift
const i8 constInt = 100
const string constString = 'hello, world';
```

#### Typedef

```text
Typedef         ::=  'typedef' DefinitionType Identifier
DefinitionType  ::=  BaseType | ContainerType
```

类型定义(`Typedef`)以 `typedef` 开头，用于为 Thrift 中声明的类型(`DefinitionType`)创建别名(`Identifier`)。

要注意，目前 `Typedef` 还不支持为自定义类型创建别名。

例如：

```thrift
typedef i8 int8
const int8 constInt = 100
```

#### Enum

```text
Enum            ::=  'enum' Identifier '{' (Identifier ('=' IntConstant)? ListSeparator?)* '}'
```

枚举(`Enum`)用来创建一种可以被枚举的类型，并对每一种值给定特定的命名。

以关键字 `enum` 开头，紧跟着类型标识符(`Identifier`)，用花括号(`{}`)包裹起来该枚举对应的所有的值，每个枚举值都有特定的标识符(`Identifier`)，并且可以为其赋值一个非负的整形(`IntConstant`)，最后可以以分隔符结尾(`ListSeparator`)。

若没有显示的给枚举值赋值，则首位枚举值默认为 0，其他枚举值默认是前一个枚举值的结果加 1。同时需要注意枚举名称本身即代表了命名空间，在使用时需要拼写全部的名称。例如：

```thrift
enum Silk {
    began   // 默认为 0
    pause   // 默认为 began + 1 = 1
    ended   // 默认为 pause + 1 = 2
}

Silk silkStatus = Silk.began
```

我们也可以通过手动赋值，来实现位图的效果，例如：

```thrift
enum Silk {
    status_0 = 1>>0
    status_1 = 1>>1
    status_2 = 1>>2
    status_3 = 1>>3
}
```

#### Struct

```text
Struct          ::=  'struct' Identifier '{' Field* '}'
```

结构体(`Struct`)是 Thrift 中的基本组合类型，其中每个字段(`Field`)的名称在结构体内部都要求是唯一的。

以关键字 `struct` 开头，紧跟着类型标识符(`Identifier`)，用花括号(`{}`)包裹起来该结构体所包含的所有字段(`Field`)。例如：

```thrift
enum Silk {
    status_0 = 1>>0
    status_1 = 1>>1
    status_2 = 1>>2
    status_3 = 1>>3
}
```

#### Union

```text
Union          ::=  'union' Identifier '{' Field* '}'
```

联合(`Union`)和结构体(`Struct`)较为类似，但是其内部的字段都默认会共用一段内存空间，有且仅有一个字段会被赋值。例如在统计信息时，用户可以使用电话号码或者邮箱：

```thrift
union SilkInfo {
    1: string phone,
    2: string email
}

// 只能有一个字段被设置
SilkInfo info_0.phone = "12312341234"
// SilkInfo info_0.email = "123@qq.com"
```

#### Exception

```text
Exception       ::=  'exception' Identifier '{' Field* '}'
```

异常(`Exception`)同样与结构体(`Struct`)类似，只不过在生成具体编程语言的代码时，会适当地继承对应的基类。

#### Service

```text
Service         ::=  'service' Identifier ( 'extends' Identifier )? '{' Function* '}'

Function        ::=  'oneway'? FunctionType Identifier '(' Field* ')' Throws? ListSeparator?

FunctionType    ::=  FieldType | 'void'

Throws          ::=  'throws' '(' Field* ')'
```

服务(`Service`)类似于接口，其内部包含了为某个特定服务提供能力支持的，一组任意数量的函数(`Function`)，同时服务也可以拓展其他已定义的服务(`'extends' Identifier`)。

函数(`Function`)一般由函数类型(`FunctionType`)、函数标识符(`Identifier`)以及任意数量的入参(`' Field* '`)构成。以外还包括可选关键字 `oneway`、异常抛出(`Throws`)以及分隔符(`ListSeparator`)。

`oneway` 表示这个函数是单向的，客户端只需要发起对应请求即可，服务端不会返回任何响应。与返回类型 `void` 的区别在于，`void` 类型的函数仍然可以抛出异常。

例如：

```thrift
service SilkService {
    oneway void SetUser(1: string UserId),
    i16 GetAge(1: string UserId) throws (1: Error err),
}
```
