# Authentication

认证（Authentication）主要用于验证用户身份的过程，确保用户身份真实有效。

## Session-Cookie

Session 和 Cookie 是 Web 开发中用于跟踪用户状态的核心技术，两者协同工作以弥补 HTTP 协议的无状态性，也可以基于此验证用户身份。

### Cookie

Cookie 是由服务器生成并发送到浏览器的小型文本数据（通常 ≤4KB），浏览器会将其存储在本地，后续请求自动携带，用于保存用户偏好、登录状态等信息。

**工作流程**

- 服务器创建：服务器通过 HTTP 响应头 `Set-Cookie: name=value; attributes` 发送 Cookie
- 浏览器存储：浏览器解析并保存 Cookie（按域名分类）
- 自动回传：后续对同一域名的请求会自动附加 Cookie 信息

**关键属性**

- `Expires/Max-Age`：设置过期时间（持久化 Cookie）或会话期 Cookie（浏览器关闭失效）
- `Domain`：指定生效的域名（默认为当前域名，可包含子域名）。
- `Path`：限制 Cookie 仅在特定路径下生效。
- `Secure`：仅通过 HTTPS 协议传输。
- `HttpOnly`：禁止 JavaScript 访问（防范 XSS 攻击）。
- `SameSite`：控制跨站请求时是否发送（防范 CSRF 攻击，可选 Strict/Lax/None）。

### Session

Session 是服务器为每个用户创建的临时数据存储（如用户 ID、权限），通常依赖 Cookie 传递 Session ID 来关联用户。

**工作流程**

- **创建 Session**：用户首次访问时，服务器生成唯一 Session ID 和对应的存储空间（内存、数据库、Redis 等）
- **传递 Session ID**：通过 Set-Cookie 将 Session ID 发送给浏览器
- **验证请求**：后续请求携带 Session ID，服务器据此查找对应的 Session 数据

需要注意的是，执行认证逻辑的，是 **Session ID**，而 **Cookie** 只是传递 **Session ID** 的一种方式。在禁用 **Cookie** 的情况下，可以在 `Header` 或 `Query` 中携带 **Session ID**。

## JWT(JSON Web Token)

JWT 是一种基于 Token 的认证机制，其内部直接存储了用户信息，无需服务端额外存储其他内容，所以是**无状态**的，能较大情况节省服务端资源，且解决多系统间 Session 共享问题。

### 格式

JWT 本质上就是一组字串，通过（.）切分成三个为 Base64 编码的部分：

- **Header**：声明类型（typ: "JWT"）和签名算法（如 alg: "HS246"）
- **Payload**：携带用户信息和其他声明
- **Signature**：对 Header 和 Payload 的签名，防止篡改

![](images/2025-03-23-20-39-35.png)

其最终的格式通常为 `xxx.yyy.zzz`，如

```text
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.
eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.
SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c
```

### 常见属性

- 标准注册声明：JWT 规范中预定义的一些声明
  - **iss(Issuer)**：JWT 的签发者（如服务名称或 URL）
  - **sub(Subject)**：JWT 的主题（如用户 ID）
  - **aud(Audience)**：JWT 的目标接收方（如服务端 API 的 URL）
  - **exp(Expiration Time)**：JWT 的过期时间（Unix 时间戳）
  - **nbf(Not Before)**：JWT 的生效时间（Unix 时间戳）
  - **iat(Issued At)**：JWT 的签发时间（Unix 时间戳）
  - **jti(JWT ID)**：JWT 的唯一标识符，用于防止重放攻击

- 公共声明：由开发者自定义，但需避免与标准声明冲突
  - **name**：用户姓名，如 `"name": "chen"`
  - **email**：用户邮箱，如 `"email": "chen@example.com"`
  - **roles**：用户角色列表（如权限控制），如 `"roles": ["admin", "editor"]`
  - **scope**：权限范围，如`"scope": "read write delete"`

- 私有声明：由双方协商定义，需确保不与标准或公共声明冲突
  - **userId**：用户唯一标识符（与 sub 类似），如 `"userId": "u_abc123"`
  - **company**：用户所属公司，如 `"company": "Tech Corp"`
  - **preferences**：用户个性化设置，如 `"preferences": {"theme": "dark"}`
  - **deviceId**：用户设备标识符（用于多设备管理），如 `"deviceId": "d_xyz789"`

## SSO(Single Sign On)

SSO 允许用户通过一次登录访问多个相互信任的应用或服务，而无需重复输入凭据，其优势如下所示：

- **用户**：登录一次，可以同时访问多个服务，简化操作成本，无需记录多套用户名和密码
- **管理**：管理员只需维护一个认证服务即可
- **开发**：新系统开发时，直接使用统一的认证服务，无需二次开发

### 工作原理

SSO 的核心是集中式身份认证，用户只需在认证中心验证一次身份，即可通过 Token 在不同系统间传递认证信息，访问所有已授权的服务。

- 通过统一的认证服务，解决客户端 Cookie 跨域问题
- 通过 JWT 的无状态特性，解决服务端多系统 Session 共享问题。

### 核心组件

- **身份提供者（Identity Provider, IdP）**
  - 负责用户身份验证（如输入用户名密码）
  - 生成加密的认证令牌（如 JWT 令牌）

- **服务提供者（Service Provider, SP）**
  - 依赖 IdP 验证用户身份的应用或服务
  - 接收并验证令牌的有效性

- **令牌（Token）**
  - 用户通过认证后，IdP 生成的加密凭证
  - 用于向 SP 证明身份

### 工作流程

**第 1 步**

- 用户访问 Gmail 时请求 IdP
- Idp 发现用户未登录，则重定向至 SSO 登录页面
- 用户在该页面输入登录信息（账号密码或 MFA）

**第 2-3 步**

- IdP 验证登录信息，并为用户创建全局会话 Cookie 和 Token
- 将 Cookie 返回至客户端存储，并限制 IdP 域名
- 将 Token 返回给 Gmail 服务使用

**第 4-7 步**

- Gmail 请求 IdP 并验证 Token，IdP 返回验证成功
- Gmail 返回请求资源

**第 8-10 步**

- 用户浏览到另一个 Google 旗下的网站，例如 YouTube
- YouTube 发现用户未登录，请求 IdP 进行身份验证，并携带 Cookie
- IdP 发现用户已经登录，会将令牌返回给 YouTube

**第 11 - 14 步**

- YouTube 请求 IdP 并验证 Token，IdP 返回验证成功
- YouTube 返回请求资源

![](images/2025-03-23-22-18-16.png)

### SAML

## OAuth





## MFA

1. **联合认证协议**  
   - **OAuth 2.0**：主要用于**授权**第三方应用访问资源，但常被用于联合登录（如“使用Google账号登录”）。实际认证由第三方完成。
   - **OpenID Connect (OIDC)**：基于OAuth 2.0的认证协议，提供标准化的用户身份信息（ID Token）。
   - **SAML**：XML-based协议，常见于企业单点登录（SSO）。

2. **多因素认证（MFA）**  
   - 结合密码（所知）、手机/硬件密钥（所有）、生物特征（所是）等多种方式增强安全性。


## Ref

- <https://javaguide.cn/system-design/security/basis-of-authority-certification.html>
- <https://javaguide.cn/system-design/security/design-of-authority-system.html>
- <https://javaguide.cn/system-design/security/jwt-intro.html>
- <https://mp.weixin.qq.com/s/ul0AHZ0zP5BxKTJFtxDeIQ?token=724645736&lang=zh_CN>
