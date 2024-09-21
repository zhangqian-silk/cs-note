# 字符编码

## 技术背景

在计算机中，所有的数据均以二进制方式（0和1）进行存储，想要正确表示自然语言中常用的字符，例如数字 0、1、2，字母 a、b、c，符号 {}、()、[]，以及中文字符等等，则需要指定具体用哪些二进制数字来表示具体的字符，这种映射关系叫做编码。

为了在不同的环境下实现通信，则必须遵循统一的字符编码规范。

## ASICC

> [wiki/ASCII](https://zh.wikipedia.org/wiki/ASCII)

ASCII（American Standard Code for Information Interchange，美国信息交换标准代码）是基于拉丁字母的一套电脑字符编码标准，现已被国际标准化组织（International Organization for Standardization，ISO）定为国际标准（ISO/IEC 646）。

ASICC 的编码范围为 0~127(0x00~0x7f)，包含了所有的英文字母(A-Za-z)、所有的数字(0-9)、部分的标点符号((),[],{}...)、部分运算符号(,+,-,*,/...)以及部分控制字符(换行符、回车符、空字符等)。

## Unicode

> [wiki/Unicode](https://zh.wikipedia.org/wiki/Unicode)

Unicode，全称为 Unicode 标准（The Unicode Standard），是为了解决其他字符编码方案的局限而产生，现已成为业界标准，随着通用字符集 ISO/IEC 10646 的发展而不断增修，截至2022年，Unicode 发布了 15.0.0 版本，已有超过14万个字符。

在 Unicode 的历代版本中，逐步扩充了阿拉伯字符、希腊字母、中日韩统一表意文字、藏文、缅文、交通标志、颜文字等等自然语言符号。

## UTF-8

> [wiki/UTF-8](https://zh.wikipedia.org/wiki/UTF-8)

UTF-8（8-bit Unicode Transformation Format）是一种针对 Unicode 的可变长度字符编码，也是一种前缀码。变长的特性，一方面可以节省内存使用，另一方面则可以同时兼容 ASCII 码，目前已是互联网最主要的编码方式。

### 编码方式

UTF-8 编码方式如下：

- 单字节字符，首位为 0，与 ASCII 码完全一致；
- 多字节字符，假设为 n 位(n>1)，则第一个字节前 n 位为 1，第 n+1 位为 0，后面字节的前两位为 10，其余空位填充 unicode 码，高位用 0 补齐。

因此，UTF-8 编码会形成如下特征：

- 0xxxxxxx
- 110xxxxx 10xxxxxx
- 1110xxxx 10xxxxxx 10xxxxxx
- 11110xxx 10xxxxxx 10xxxxxx 10xxxxxx

此时，不难判断出字节的识别方式：

- 0xxxxxxx：如果B的第一位为0，则B独立的表示一个字符(ASCII码)；
- 10xxxxxx：如果B的第一位为1，第二位为0，则B为一个多字节字符中的一个字节(非ASCII字符)；
- 110xxxxx：如果B的前两位为1，第三位为0，则B为两个字节表示的字符中的第一个字节；
- 1110xxxx：如果B的前三位为1，第四位为0，则B为三个字节表示的字符中的第一个字节；
- 11110xxx：如果B的前四位为1，第五位为0，则B为四个字节表示的字符中的第一个字节；

## Percent Encoding

> [wiki/百分比编码](https://zh.wikipedia.org/wiki/%E7%99%BE%E5%88%86%E5%8F%B7%E7%BC%96%E7%A0%81)
> [[rfc3986] Uniform Resource Identifier (URI): Generic Syntax](https://datatracker.ietf.org/doc/html/rfc3986)

百分比编码(Percent-encoding)，又称作 URL 编码(URL encoding)，是 URL 和 URL 的一种特定的编码机制。

### 编码方式

先按照指定的编码方式（一般是 UTF-8）将字符转为 16 进制字节流，然后在每个字节前添加 `%` 来构成一个百分比编码。例如：

- 字符：中文
- UTF-8：\xe4\xb8\xad\xe6\x96\x87
- Percent Encoding：%E4%B8%AD%E6%96%87

### 字符类型

RFC 中声明了 URI 与 URL 中所能使用的字符集合，可分为具备分割意义的保留字符，和没有特殊含义的非保留字符。在该数据集意外的字符，均必须使用百分比编码表示。

- RFC 3986 section 2.2 保留字符 （2005年1月）：
    ! * ' ( ) ; : @ & = + $ , / ? # [ ]
  
- RFC 3986 section 2.3 未保留字符 （2005年1月）：
    A B C D E F G H I J K L M N O P Q R S T U V W X Y Z
    a b c d e f g h i j k l m n o p q r s t u v w x y z
    0 1 2 3 4 5 6 7 8 9 - _ . ~
